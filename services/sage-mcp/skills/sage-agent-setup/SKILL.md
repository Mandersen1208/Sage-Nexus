# Sage Agent Setup

Use this skill when creating or changing a Sage specialist agent.

The goal is registry-first setup: adding a normal agent should require a prompt, a registry entry, any needed MCP tool implementation, tests, and a restart. It should not require editing manager worker maps, orchestrator route maps, peer-policy maps, senior-gate lists, or Go tool schemas.

---

## Agent Definition

Choose a stable agent ID:

```text
AGT-<domain>-agent
```

Define the boundary before wiring:

- What the agent owns.
- What it must not own.
- Whether it is routable by the orchestrator.
- Whether it is targetable from Sage Nexus chat.
- Which chat modes it supports.
- Which MCP tools or tool bundles it may use.
- Which peer agents it may consult.
- Whether Senior Dev gates delivery work for it.
- Which decisions require a Research brief.

Keep domain agents bounded. Runtime Librarian indexes and answers provenance, but it does not own planning or repair work.

---

## Prompt

Create one focused prompt:

```text
services/manager/prompts/<agent-name>.md
```

The prompt should state authority, source of truth, required tools, output contract, and boundaries. Do not hide routing policy in prose; routing, tools, peers, and gates belong in the registry.

---

## Registry Entry

Register the agent in the active agent registry.

Bundled fallback:

```text
services/manager/config/agents.json
```

Workspace override, loaded at runtime when present:

```text
/sage-state/workspace/sage/agents.registry.json
```

The manager reads `SAGE_AGENT_REGISTRY_FILE` first as an overlay and falls back to bundled core config when the workspace registry is missing or invalid.

Example:

```json
{
  "AGT-office-document-agent": {
    "id": "AGT-office-document-agent",
    "enabled": true,
    "displayName": "Office",
    "targetable": true,
    "supportedChatModes": ["solo", "launch"],
    "routable": true,
    "model": "gpt-4.1",
    "systemPromptFile": "../prompts/office-document-agent.md",
    "routeToolName": "call_office_document_agent",
    "routeDescription": "Route Microsoft Word and Excel artifact creation, DOCX/XLSX reports, formatted documents, tables, and spreadsheet workbooks to the Office document specialist.",
    "toolBundles": ["office", "context"],
    "tools": [],
    "peerTargets": ["AGT-project-manager-agent", "AGT-qa-agent", "AGT-research-agent"],
    "maxPeerDepth": 3,
    "seniorGate": "off",
    "authority": "DOCX and XLSX artifact generation, formatting, workbook structure, and document packaging.",
    "mustNotOwn": ["source research", "architecture decisions", "code implementation"]
  }
}
```

Registry fields:

- `enabled`: omitted means enabled.
- `displayName`: user-facing name surfaced by `/agents/catalog`.
- `targetable`: shows the agent in Sage Nexus chat mode selection.
- `supportedChatModes`: `solo` for direct agent conversation and `launch` for an agent-owned flow. `auto` is reserved for Sage front-of-house.
- `modeLabels` and `modeDescriptions`: optional per-mode UI copy. The frontend must render catalog data instead of hardcoding agent names or mode labels.
- `routable`: creates an orchestrator route tool when true.
- `routeToolName` and `routeDescription`: required for routable agents.
- `toolBundles`: top-level bundle names from `toolBundles`.
- `tools`: explicit MCP tool names beyond bundles.
- `peerTargets`: allowlisted agents for bounded peer calls.
- `maxPeerDepth`: caller-specific depth limit.
- `seniorGate`: `off`, `delivery`, or `always`.
- `authority` and `mustNotOwn`: role metadata surfaced to inventory and docs.

---

## MCP Tools

If the agent needs new tools:

1. Add the MCP implementation under:

```text
services/sage-mcp/src/tools/
```

2. Register the module in:

```text
services/sage-mcp/src/index.ts
```

3. Add tool names to a registry `toolBundles` entry or the agent's explicit `tools`.

Do not add Go tool schemas. The manager discovers MCP schemas with `tools/list` at startup and filters them through the registry. Unknown registry tools are logged and withheld from the worker.

---

## Routing

Normal route tools are generated from registry entries where:

```json
{
  "enabled": true,
  "routable": true,
  "routeToolName": "call_<domain>_agent"
}
```

The orchestrator prompt receives an injected Active Worker Registry section at startup. Do not edit Go route maps for normal agents.

Use deterministic pre-routing only for hard safety or admission reasons.

---

## Bounded Peer Mesh

Peer policy comes from each agent's `peerTargets` and `maxPeerDepth`.

Every peer call must include caller, target, reason, depth, and work-context metadata. The manager enforces the allowlist and depth. Peer handoffs should use Agent Work Context instead of large prompt handoffs.

Peer calls are consultations. The original owner keeps responsibility for its domain output. Senior Dev approves delivery quality but should not become the default planner or builder for every domain.

---

## Research Evidence

Agents must consult `AGT-research-agent` for decision-grade external/current facts:

- provider or model capabilities
- framework or library selection
- security posture
- vendor behavior
- licensing
- current documentation
- unfamiliar implementation patterns

Research should append a `research_brief` to Agent Work Context with sources, date, confidence, and impact.

---

## Tests

Add or update tests for:

- Registry builds workers dynamically.
- Registry generates the route tool and target.
- Registry controls tool exposure.
- Unknown registry tools are withheld clearly.
- Peer allowlists and max depth are enforced.
- Senior-gate mode behaves correctly.
- MCP tools work and appear in `tools/list`.
- Runtime inventory reports registry, route tools, peer policy, prompts, and tool exposure.

Useful commands:

```powershell
cd services\manager
go test ./...

cd ..\sage-mcp
npm test
npm run build
```

---

## Docker Rebuild

This runtime is containerized. Source changes are not live until rebuilt and recreated.

```powershell
docker compose build manager sage-mcp
docker compose up -d --force-recreate manager sage-mcp
```

Verify:

```powershell
Invoke-WebRequest -UseBasicParsing http://localhost:8090/health
Invoke-WebRequest -UseBasicParsing http://localhost:8090/agents/list
Invoke-WebRequest -UseBasicParsing http://localhost:8090/agents/health
```

Then ask Sage a request that should route to the new specialist and confirm the route, tool trace, and handoff. Do not accept polished text as proof.
