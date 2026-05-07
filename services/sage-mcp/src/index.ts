/**
 * sage-mcp
 *
 * Standalone MCP server for Sage Nexus.
 *
 * The manager is the orchestration owner. This service exposes deterministic
 * tools that workers can call: runtime inventory, Agent Work Context,
 * document/artifact creation, web/search helpers, and skill lookup.
 *
 * Transport: stdio (default) or streamable HTTP (set TRANSPORT=http).
 */

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import { createServer } from "http";
import { registerSkillTools } from "./tools/skills.js";
import { registerDelegateTools } from "./tools/delegate.js";
import { registerDelegateContinueTool } from "./tools/delegate-continue.js";
import { registerWebSearchTools } from "./tools/websearch.js";
import { registerBudgetTools } from "./tools/budget.js";
import { registerAgentCallTools } from "./tools/agent-call.js";
import { registerRuntimeInventoryTools } from "./tools/runtime-inventory.js";
import { registerAgentContextTools } from "./tools/agent-context.js";
import { registerOfficeTools } from "./tools/office.js";
import { getSkillDiscoveryRuntime, type SkillState, type SourceTrust } from "./services/skill-discovery.js";

// MCP_MODE gates which tools are exposed:
//   "sage"  - Sage's stdio subprocess. She gets only front-door/context tools.
//   "agent" - HTTP server used by worker agents. Full registry-filtered surface.
const mode = process.env.MCP_MODE ?? "agent";
console.error(`[sage-mcp] MCP_MODE=${mode}`);

function createMcpServer(): McpServer {
  const server = new McpServer(
    { name: "sage-mcp", version: "1.0.0" },
    {},
  );

  if (mode === "sage") {
    registerDelegateTools(server);
    registerDelegateContinueTool(server);
    registerRuntimeInventoryTools(server);
    registerAgentContextTools(server);
  } else {
    registerSkillTools(server);
    registerWebSearchTools(server);
    registerBudgetTools(server);
    registerAgentCallTools(server);
    registerRuntimeInventoryTools(server);
    registerAgentContextTools(server);
    registerOfficeTools(server);
  }

  return server;
}

function sendJson(res: import("http").ServerResponse, status: number, payload: unknown): void {
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(JSON.stringify(payload));
}

async function readJsonBody(req: import("http").IncomingMessage): Promise<Record<string, unknown>> {
  const chunks: Buffer[] = [];
  for await (const chunk of req) {
    chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(String(chunk)));
  }
  if (chunks.length === 0) return {};
  const raw = Buffer.concat(chunks).toString("utf-8");
  if (!raw.trim()) return {};
  return JSON.parse(raw) as Record<string, unknown>;
}

function asString(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function asBool(value: unknown): boolean | undefined {
  if (typeof value === "boolean") return value;
  return undefined;
}

function asStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.map((item) => asString(item)).filter((item) => item.length > 0);
}

function asTrust(value: unknown): SourceTrust | undefined {
  return value === "trusted" || value === "untrusted" ? value : undefined;
}

function asSkillState(value: unknown): SkillState | undefined {
  return value === "quarantined" || value === "released" || value === "disabled"
    ? value
    : undefined;
}

async function handleSkillDiscoveryRequest(
  req: import("http").IncomingMessage,
  res: import("http").ServerResponse,
): Promise<boolean> {
  if (!req.url) return false;
  const url = new URL(req.url, "http://localhost");
  const path = url.pathname;
  if (!path.startsWith("/skills/discovery")) return false;

  const runtime = await getSkillDiscoveryRuntime();

  if (path === "/skills/discovery/servers" && req.method === "GET") {
    const includeLocal = url.searchParams.get("includeLocal") === "true";
    const sources = await runtime.listSources(includeLocal);
    sendJson(res, 200, { sources, count: sources.length });
    return true;
  }

  if (path === "/skills/discovery/servers" && req.method === "POST") {
    const body = await readJsonBody(req);
    const source = await runtime.upsertSource({
      id: asString(body["id"]),
      displayName: asString(body["displayName"]) || asString(body["name"]),
      endpoint: asString(body["endpoint"]),
      trust: asTrust(body["trust"]),
      enabled: asBool(body["enabled"]),
      sourceType: "remote",
    });
    sendJson(res, 201, source);
    return true;
  }

  if (path === "/skills/discovery/sync" && req.method === "POST") {
    const body = await readJsonBody(req);
    const sourceID = asString(body["sourceId"]);
    if (sourceID) {
      const result = await runtime.syncSource(sourceID);
      sendJson(res, 200, { mode: "single", ...result });
      return true;
    }
    const result = await runtime.syncAllSources();
    sendJson(res, 200, { mode: "all", ...result });
    return true;
  }

  if (path === "/skills/discovery/skills" && req.method === "GET") {
    const sourceId = asString(url.searchParams.get("sourceId") || "");
    const state = asSkillState(url.searchParams.get("state") || "");
    const includeLocal = url.searchParams.get("includeLocal") === "true";
    const skills = await runtime.listSkills({
      sourceId: sourceId || undefined,
      state,
      includeLocal,
    });
    sendJson(res, 200, { skills, count: skills.length });
    return true;
  }

  if (path === "/skills/discovery/search" && req.method === "GET") {
    const query = asString(url.searchParams.get("query") || "");
    const agentId = asString(url.searchParams.get("agentId") || "");
    const limitRaw = asString(url.searchParams.get("limit") || "");
    const limit = Number.parseInt(limitRaw, 10);
    if (!query) {
      sendJson(res, 200, { skills: [], count: 0, query: "" });
      return true;
    }
    const skills = await runtime.searchSkills({
      query,
      agentId: agentId || undefined,
      limit: Number.isFinite(limit) ? limit : undefined,
    });
    sendJson(res, 200, { skills, count: skills.length, query });
    return true;
  }

  const serverPatchMatch = /^\/skills\/discovery\/servers\/([^/]+)$/.exec(path);
  if (serverPatchMatch && req.method === "PATCH") {
    const sourceId = decodeURIComponent(serverPatchMatch[1] || "");
    const body = await readJsonBody(req);
    const source = await runtime.patchSource(sourceId, {
      displayName: asString(body["displayName"]),
      endpoint: asString(body["endpoint"]),
      trust: asTrust(body["trust"]),
      enabled: asBool(body["enabled"]),
    });
    sendJson(res, 200, source);
    return true;
  }

  const serverReleaseMatch = /^\/skills\/discovery\/servers\/([^/]+)\/release$/.exec(path);
  if (serverReleaseMatch && req.method === "POST") {
    const sourceId = decodeURIComponent(serverReleaseMatch[1] || "");
    const released = await runtime.releaseSourceSkills(sourceId);
    sendJson(res, 200, { sourceId, released });
    return true;
  }

  const serverSyncMatch = /^\/skills\/discovery\/servers\/([^/]+)\/sync$/.exec(path);
  if (serverSyncMatch && req.method === "POST") {
    const sourceId = decodeURIComponent(serverSyncMatch[1] || "");
    const result = await runtime.syncSource(sourceId);
    sendJson(res, 200, result);
    return true;
  }

  const skillPatchMatch = /^\/skills\/discovery\/skills\/([^/]+)$/.exec(path);
  if (skillPatchMatch && req.method === "PATCH") {
    const skillId = decodeURIComponent(skillPatchMatch[1] || "");
    const body = await readJsonBody(req);
    const skill = await runtime.patchSkill(skillId, {
      state: asSkillState(body["state"]),
      allowedAgents: asStringArray(body["allowedAgents"]),
    });
    sendJson(res, 200, skill);
    return true;
  }

  sendJson(res, 404, { error: "not found" });
  return true;
}

async function runStdio(): Promise<void> {
  const server = createMcpServer();
  const transport = new StdioServerTransport();
  await server.connect(transport);
  console.error("[sage-mcp] Running via stdio");
}

async function runHTTP(): Promise<void> {
  const port = parseInt(process.env.PORT ?? "3030", 10);
  const discovery = await getSkillDiscoveryRuntime();
  discovery.startDailySyncScheduler();

  const httpServer = createServer(async (req, res) => {
    if (req.url === "/health") {
      res.writeHead(200, { "Content-Type": "application/json" })
        .end(JSON.stringify({ status: "ok", server: "sage-mcp" }));
      return;
    }

    if ((req.url || "").startsWith("/skills/discovery")) {
      try {
        const handled = await handleSkillDiscoveryRequest(req, res);
        if (handled) return;
      } catch (err) {
        sendJson(res, 500, { error: err instanceof Error ? err.message : String(err) });
        return;
      }
    }

    if (req.method !== "POST" || req.url !== "/mcp") {
      res.writeHead(404).end("Not found");
      return;
    }

    const chunks: Buffer[] = [];
    req.on("data", (chunk: Buffer) => chunks.push(chunk));
    req.on("end", async () => {
      try {
        const body = JSON.parse(Buffer.concat(chunks).toString("utf-8")) as unknown;
        const server = createMcpServer();
        const transport = new StreamableHTTPServerTransport({
          sessionIdGenerator: undefined,
          enableJsonResponse: true,
        });
        res.on("close", () => transport.close());
        await server.connect(transport);
        await transport.handleRequest(req, res, body);
      } catch (err) {
        res.writeHead(400).end("Bad request");
        console.error("[sage-mcp] Request error:", err);
      }
    });
  });

  httpServer.listen(port, () => {
    console.error(`[sage-mcp] Running via HTTP on http://localhost:${port}/mcp`);
  });
}

const transport = process.env.TRANSPORT ?? "stdio";

if (transport === "http") {
  runHTTP().catch((err: unknown) => {
    console.error("[sage-mcp] Fatal error:", err);
    process.exit(1);
  });
} else {
  runStdio().catch((err: unknown) => {
    console.error("[sage-mcp] Fatal error:", err);
    process.exit(1);
  });
}
