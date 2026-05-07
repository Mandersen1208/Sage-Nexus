import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";

type FetchLike = typeof fetch;

interface AgentContextReadInput {
  work_context_id: string;
  token: string;
  limit?: number;
  kind?: string;
  actor?: string;
}

interface AgentContextAppendInput {
  work_context_id: string;
  token: string;
  kind: string;
  summary: string;
  content?: string;
  metadata?: Record<string, unknown>;
}

interface AgentContextSearchInput {
  work_context_id: string;
  token: string;
  query: string;
  kind?: string;
  limit?: number;
}

interface WorkContextEvent {
  id?: string;
  kind?: string;
  actor?: string;
  summary?: string;
  content?: string;
  metadata?: Record<string, unknown>;
  createdAt?: number;
}

interface WorkContextDetail {
  meta?: Record<string, unknown>;
  events?: WorkContextEvent[];
}

function managerUrl(): string {
  return process.env["MANAGER_URL"] ?? "http://manager:8090";
}

function contextUrl(workContextId: string): URL {
  return new URL(`/work-context/${encodeURIComponent(workContextId)}`, managerUrl());
}

function authHeaders(token: string): Record<string, string> {
  return {
    "Authorization": `Bearer ${token}`,
    "Content-Type": "application/json",
  };
}

async function parseManagerResponse(res: Response): Promise<unknown> {
  const text = await res.text();
  if (!res.ok) {
    throw new Error(`manager returned ${res.status}: ${text}`);
  }
  if (text.trim() === "") return {};
  return JSON.parse(text) as unknown;
}

export async function readAgentContext(input: AgentContextReadInput, fetchImpl: FetchLike = fetch): Promise<WorkContextDetail> {
  const url = contextUrl(input.work_context_id);
  if (input.limit) url.searchParams.set("limit", String(input.limit));
  if (input.kind) url.searchParams.set("kind", input.kind);
  if (input.actor) url.searchParams.set("actor", input.actor);

  const res = await fetchImpl(url, {
    method: "GET",
    headers: authHeaders(input.token),
  });
  return await parseManagerResponse(res) as WorkContextDetail;
}

export async function appendAgentContext(input: AgentContextAppendInput, fetchImpl: FetchLike = fetch): Promise<WorkContextEvent> {
  const url = new URL(`${contextUrl(input.work_context_id).pathname}/events`, managerUrl());
  const res = await fetchImpl(url, {
    method: "POST",
    headers: authHeaders(input.token),
    body: JSON.stringify({
      kind: input.kind,
      summary: input.summary,
      content: input.content,
      metadata: input.metadata,
    }),
  });
  return await parseManagerResponse(res) as WorkContextEvent;
}

export async function searchAgentContext(input: AgentContextSearchInput, fetchImpl: FetchLike = fetch): Promise<{ query: string; count: number; results: WorkContextEvent[] }> {
  const detail = await readAgentContext({
    work_context_id: input.work_context_id,
    token: input.token,
    kind: input.kind,
    limit: Math.max(input.limit ?? 100, 100),
  }, fetchImpl);
  const query = input.query.toLowerCase();
  const limit = input.limit ?? 25;
  const results = (detail.events ?? [])
    .filter((event) => {
      const haystack = [
        event.kind,
        event.actor,
        event.summary,
        event.content,
        event.metadata ? JSON.stringify(event.metadata) : "",
      ].join("\n").toLowerCase();
      return haystack.includes(query);
    })
    .slice(-limit);
  return { query: input.query, count: results.length, results };
}

export function registerAgentContextTools(server: McpServer): void {
  server.registerTool(
    "agent_context_read",
    {
      title: "Read Agent Work Context",
      description: "Read the shared Agent Work Context for this task using the scoped work_context_id and token.",
      inputSchema: z.object({
        work_context_id: z.string().min(1),
        token: z.string().min(1),
        limit: z.number().int().min(1).max(200).optional(),
        kind: z.string().optional(),
        actor: z.string().optional(),
      }).strict(),
      annotations: { readOnlyHint: true, destructiveHint: false, idempotentHint: true, openWorldHint: false },
    },
    async (input) => {
      const output = await readAgentContext(input);
      return {
        content: [{ type: "text" as const, text: JSON.stringify(output, null, 2) }],
        structuredContent: output as Record<string, unknown>,
      };
    },
  );

  server.registerTool(
    "agent_context_append",
    {
      title: "Append Agent Work Context",
      description: "Append a concise note, finding, decision, blocker, or tool result to the shared Agent Work Context for this task.",
      inputSchema: z.object({
        work_context_id: z.string().min(1),
        token: z.string().min(1),
        kind: z.string().min(1),
        summary: z.string().min(1),
        content: z.string().optional(),
        metadata: z.record(z.unknown()).optional(),
      }).strict(),
      annotations: { readOnlyHint: false, destructiveHint: false, idempotentHint: false, openWorldHint: false },
    },
    async (input) => {
      const output = await appendAgentContext(input);
      return {
        content: [{ type: "text" as const, text: JSON.stringify(output, null, 2) }],
        structuredContent: output as Record<string, unknown>,
      };
    },
  );

  server.registerTool(
    "agent_context_search",
    {
      title: "Search Agent Work Context",
      description: "Exact substring search over the shared Agent Work Context for this task. v1 is not vector search.",
      inputSchema: z.object({
        work_context_id: z.string().min(1),
        token: z.string().min(1),
        query: z.string().min(1),
        kind: z.string().optional(),
        limit: z.number().int().min(1).max(100).optional(),
      }).strict(),
      annotations: { readOnlyHint: true, destructiveHint: false, idempotentHint: true, openWorldHint: false },
    },
    async (input) => {
      const output = await searchAgentContext(input);
      return {
        content: [{ type: "text" as const, text: JSON.stringify(output, null, 2) }],
        structuredContent: output,
      };
    },
  );
}
