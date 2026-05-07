import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { buildRuntimeInventorySnapshot, redactObject } from "../src/tools/runtime-inventory.js";

function write(filePath: string, content: string): void {
  fs.mkdirSync(path.dirname(filePath), { recursive: true });
  fs.writeFileSync(filePath, content, "utf-8");
}

const root = fs.mkdtempSync(path.join(os.tmpdir(), "sage-runtime-inventory-"));
const repoRoot = path.join(root, "repo");
const workspace = path.join(root, "workspace");
const configFile = path.join(root, "sage.json");

write(path.join(repoRoot, "docker-compose.yml"), `
services:
  manager:
    environment:
      SAGE_SOUL_PATH: "/home/node/.openclaw/workspace/SOUL.md"
    ports:
      - "8090:8090"
  sage-mcp:
    environment:
      TOKEN: "should-not-leak"
    volumes:
      - /repo:/workspace/sage-nexus:ro
`);

write(path.join(repoRoot, "services", "manager", "cmd", "manager", "main.go"), `
package main
func main() {
  mux.HandleFunc("/chat", nil)
  mux.HandleFunc("/stream", nil)
}
`);

write(path.join(repoRoot, "services", "manager", "config", "agents.json"), JSON.stringify({
  version: 1,
  toolBundles: {
    context: ["agent_context_read", "agent_context_append"],
    office: ["office_docx_create", "office_xlsx_create"],
    runtime: ["runtime_inventory_scan", "runtime_inventory_search"],
  },
  agents: {
    "AGT-sage": { model: "gpt-4.1", systemPromptFile: "../prompts/sage.md" },
    "AGT-sage-orchestrator": { model: "gpt-4.1", systemPromptFile: "../prompts/sage-orchestrator.md" },
    "AGT-runtime-librarian-agent": {
      model: "gpt-4.1",
      systemPromptFile: "../prompts/runtime-librarian-agent.md",
      toolBundles: ["runtime", "context"],
    },
    "AGT-office-document-agent": {
      model: "gpt-4.1",
      systemPromptFile: "../prompts/office-document-agent.md",
      routable: true,
      routeToolName: "call_office_document_agent",
      routeDescription: "Create DOCX and XLSX artifacts.",
      toolBundles: ["office", "context"],
      peerTargets: ["AGT-qa-agent"],
      maxPeerDepth: 2,
      seniorGate: "off",
      authority: "Office artifacts",
      mustNotOwn: ["architecture decisions"],
    },
  },
}, null, 2));

write(path.join(repoRoot, "services", "manager", "prompts", "sage.md"), "# Sage");
write(path.join(repoRoot, "services", "manager", "prompts", "sage-orchestrator.md"), "# Orchestrator");
write(path.join(repoRoot, "services", "manager", "prompts", "runtime-librarian-agent.md"), "# Librarian");
write(path.join(repoRoot, "services", "manager", "prompts", "office-document-agent.md"), "# Office");
write(path.join(repoRoot, "services", "sage-mcp", "src", "tools", "delegate.ts"), `
server.registerTool("delegate_to_manager", {}, async () => ({}));
`);
write(path.join(repoRoot, "services", "sage-mcp", "src", "tools", "office.ts"), `
server.registerTool("office_docx_create", {}, async () => ({}));
server.registerTool("office_xlsx_create", {}, async () => ({}));
`);
write(path.join(workspace, "SOUL.md"), "# Real Soul");
write(path.join(workspace, "projects", "sage-nexus", "architecture", "README.md"), `# Sage Nexus Architecture
Status: Approved

Runtime-backed chat and dashboard architecture.
`);

write(configFile, JSON.stringify({
  gateway: { auth: { token: "super-secret-token" } },
  mcp: {
    servers: {
      "sage-knowledge": {
        env: {
          SAGE_SOUL_PATH: "/sage-state/workspace/SOUL.md",
          API_KEY: "super-secret-api-key",
        },
      },
    },
  },
}, null, 2));

process.env["RUNTIME_REPO_ROOT"] = repoRoot;
process.env["RUNTIME_CONFIG_FILE"] = configFile;
process.env["SAGE_SOUL_PATH"] = path.join(workspace, "SOUL.md");
process.env["SAGE_AGENT_REGISTRY_FILE"] = path.join(workspace, "sage", "agents.registry.json");

const snapshot = buildRuntimeInventorySnapshot();
const entityIds = new Set(snapshot.entities.map((entity) => entity.id));

assert.equal(entityIds.has("route:manager:/chat"), true);
assert.equal(entityIds.has("route:manager:/stream"), true);
assert.equal(entityIds.has("agent:AGT-runtime-librarian-agent"), true);
assert.equal(entityIds.has("agent:AGT-office-document-agent"), true);
assert.equal(entityIds.has("route:orchestrator:call_office_document_agent"), true);
assert.equal(entityIds.has("mcp_tool:delegate_to_manager"), true);
assert.equal(entityIds.has("mcp_tool:office_docx_create"), true);
assert.equal(entityIds.has("finding:soul-path-conflict"), true);
assert.equal(entityIds.has("architecture_doc:projects/sage-nexus/architecture/README.md"), true);

const office = snapshot.entities.find((entity) => entity.id === "agent:AGT-office-document-agent");
assert.deepEqual(office?.metadata?.["toolBundles"], ["office", "context"]);
assert.deepEqual(office?.metadata?.["tools"], ["agent_context_append", "agent_context_read", "office_docx_create", "office_xlsx_create"]);
assert.equal(office?.metadata?.["routeToolName"], "call_office_document_agent");
assert.equal(office?.metadata?.["seniorGate"], "off");

const serialized = JSON.stringify(snapshot);
assert.equal(serialized.includes("super-secret-token"), false);
assert.equal(serialized.includes("super-secret-api-key"), false);
assert.equal(serialized.includes("should-not-leak"), false);

assert.deepEqual(redactObject({ nested: { apiKey: "x" }, normal: "ok" }), {
  nested: { apiKey: "[REDACTED]" },
  normal: "ok",
});

console.log("runtime inventory tests passed");
