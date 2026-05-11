# Sage Nexus Status

Last updated: 2026-05-08

This is the canonical runtime and feature inventory for Sage Nexus.

## Runtime Matrix

| Service | Port | Purpose |
| --- | --- | --- |
| `dashboard` | `5174` | React SPA (`/chat`, `/dashboard`, `/models`, `/files`, `/skills`, `/settings`) |
| `manager` | `8090` | Orchestrator/control plane, chat/session APIs, governance APIs |
| `sage-mcp` | `3030` | MCP tools + canonical skill discovery/indexing |
| `acp-server` | `8080` | Admission control/token issuance |
| `redis` | `6379` | A2A events, chat sessions, rolling context, work context, policy state |
| `skills-db` | `5432` | Postgres + pgvector for canonical skill registry/embeddings |

## Key Environment and Mounts

| Key | Current meaning |
| --- | --- |
| `SAGE_SOUL_PATH` | SOUL source path inside container. Default `/home/node/.openclaw/workspace/SOUL.md`. |
| `SAGE_STATE_DIR` | Sage-owned state root. Default `/sage-state`. |
| `SAGE_FILE_BROWSER_ROOT` | File browser root in manager. Default `/code`. |
| `SAGE_HOST_CODE_ROOT` | Host bind source for `/code` (default `C:/Users/matta/code`). |
| `SAGE_HOST_WORKSPACE` | Host bind source for SOUL compatibility mount. |
| `MANAGER_DISPATCH_ROLLING_MAX_MESSAGES` | Rolling context window size for `/dispatch` session memory (default `12`). |
| `MANAGER_DISPATCH_ROLLING_MAX_CHARS` | Max chars per prior message when composing rolling context (default `700`). |
| `SAGE_DELEGATED_PREFER_WORKER_REPLY` | Prefer raw worker output over orchestrator rewrite for delegated replies (default `true`). |
| `CODEX_BRIDGE_URL` | Host-side Codex bridge URL for subscription-backed Codex models. Docker default `http://host.docker.internal:8765`. |

## Manager HTTP API Surface

### Core orchestration

- `GET /health`
- `GET /stream`
- `POST /dispatch`
- `POST /agent-dispatch`
- `GET /orchestrator/errors`
- `GET /agents/list`
- `GET /agents/catalog`

### Chat + continuation

- `POST /chat`
- `POST /chat/continue`
- `POST /chat/stop`
- `GET /chat/sessions`
- `POST /chat/sessions`
- `GET /chat/sessions/{id}`
- `PATCH /chat/sessions/{id}`
- `DELETE /chat/sessions/{id}`

### Model and capability catalogs

- `GET /agents/models`
- `PATCH /agents/models`
- `GET /tools/catalog`
- `POST /tools/catalog`
- `PATCH /tools/catalog/{id}`
- `DELETE /tools/catalog/{id}`
- `POST /skills/compose`
- `GET /skills/catalog`
- `POST /skills/catalog`
- `GET /skills/catalog/{id}`
- `PATCH /skills/catalog/{id}`
- `DELETE /skills/catalog/{id}`

### Discovered skill governance

- `GET /skills/discovered/servers`
- `POST /skills/discovered/servers`
- `PATCH /skills/discovered/servers/{id}`
- `POST /skills/discovered/servers/{id}/release`
- `POST /skills/discovered/servers/{id}/sync`
- `POST /skills/discovered/sync`
- `GET /skills/discovered/skills`
- `PATCH /skills/discovered/skills/{id}`

### Files and work context

- `GET /workspace/files/list`
- `GET /workspace/files/download`
- `GET /work-context/{id}`
- `POST /work-context/{id}/events`
- `GET /work-context/by-task/{taskId}`

### Provider auth

- `GET /providers/copilot/status`
- `POST /providers/copilot/login/start`
- `POST /providers/copilot/login/complete`
- `POST /providers/copilot/logout`
- `POST /providers/copilot/refresh`
- `GET /providers/codex/status`

## Sage MCP Discovery API Surface

- `GET /health`
- `POST /mcp`
- `GET /skills/discovery/servers`
- `POST /skills/discovery/servers`
- `PATCH /skills/discovery/servers/{id}`
- `POST /skills/discovery/servers/{id}/release`
- `POST /skills/discovery/servers/{id}/sync`
- `POST /skills/discovery/sync`
- `GET /skills/discovery/skills`
- `PATCH /skills/discovery/skills/{id}`
- `GET /skills/discovery/search`

## Feature Inventory by UI Route

### Chat (`/chat`)

- Shared chat sessions (create/select/delete) backed by manager session store.
- Agent mode selection (`auto`, `solo`, `launch`) from live catalog.
- Streaming task lifecycle + stop/continue flows.
- Attachment-aware dispatch.

### Dashboard (`/dashboard`)

- Live task timeline with status-update and artifact-update events.
- Agent chain and per-step captured reply visibility.
- Pause/failure reason cards with continue/stop controls.
- Error bus feed and worker/tool diagnostics.

### Models (`/models`)

- View active model assignment per agent.
- Patch per-agent model overrides from UI.

### Files (`/files`)

- Browse under `SAGE_FILE_BROWSER_ROOT`.
- Download files or folder archives.
- Breadcrumb navigation, refresh, copy path, responsive list/cards.

### Skills (`/skills`)

- Local skills section:
  - create local skill (no manual `SKILL.md` editor),
  - enable/disable local skill,
  - delete local skill.
- Indexed local skills section:
  - view canonicalized local entries from discovery index,
  - source-level sync.
- Discovered server skills section:
  - add MCP source (id, display name, endpoint, trust, enabled),
  - sync all sources or per source,
  - release quarantined skills by server (`Release Server`) or per skill,
  - disable/enable source,
  - disable/enable individual skill,
  - grouped-by-source expandable cards.

### Settings (`/settings`)

- Copilot auth lifecycle (status, connect, refresh, logout).
- Codex bridge status is currently API-only at `/providers/codex/status`; setup and manager-mediated tool notes live in `docs/CODEX_BRIDGE.md`.
- Capabilities/tool catalog management:
  - create/edit/delete tools,
  - enable/disable tools,
  - agent assignment, command/args/area metadata.
- Manager chat-assisted tool drafting in Add Tool modal:
  - manager-only create/design prompt flow,
  - prompt clears after send,
  - stable `session_id` per modal,
  - rolling short-term context from backend dispatch session memory.
- Skill governance ownership moved to `/skills`; Settings no longer hosts primary skill governance workflows.

## Orchestration + Memory Behavior

- `POST /dispatch` supports `session_id` and now persists rolling turn memory in Redis.
- Rolling context is prepended to subsequent dispatch calls for the same session id (bounded by env-configured window/size).
- For Sage delegated paths, worker output is preferred by default (`SAGE_DELEGATED_PREFER_WORKER_REPLY=true`).
- Delegated revoice defaults to pass-through in selective mode, reducing reply mutation.

## Skill Discovery + Governance Semantics

- Sage MCP runs automatic discovery sync once daily at local `2:00 AM`.
- Manual sync remains available via manager and MCP endpoints.
- Local source is auto-registered as trusted (`local://skills`) and indexed.
- Remote source default behavior:
  - `trusted` source -> new skills default `released`.
  - `untrusted` source -> new skills default `quarantined`.
- `Release Server` releases currently quarantined skills for that source only.
- Retrieval search only returns skills that are:
  - source `enabled = true`,
  - skill state `released`,
  - optional `allowed_agents` compatible with requesting agent.

## Verification Checklist

Run from `C:\Users\matta\code\sage-nexus`:

```powershell
docker compose up -d --build
Invoke-RestMethod http://localhost:8090/health
Invoke-RestMethod http://localhost:8090/agents/catalog
Invoke-RestMethod http://localhost:8090/skills/discovered/servers
Invoke-RestMethod http://localhost:8090/providers/copilot/status
Invoke-RestMethod http://localhost:8090/providers/codex/status
Invoke-RestMethod http://localhost:3030/skills/discovery/servers
```

Manual route checks:

- `http://localhost:5174/chat`
- `http://localhost:5174/dashboard`
- `http://localhost:5174/models`
- `http://localhost:5174/files`
- `http://localhost:5174/skills`
- `http://localhost:5174/settings`
