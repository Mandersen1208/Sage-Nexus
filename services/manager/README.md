# Sage Nexus Manager

Standalone Go control plane for Sage Nexus.

The manager owns chat/session HTTP routes, Sage front-of-house dispatch, agent registry loading, ACP admission, Redis event streaming, Agent Work Context, and provider auth state. Sage remains the persona/front-of-house layer; the manager remains the orchestrator.

## Runtime

Default local endpoints:

- Manager: `:8090`
- Redis: `redis:6379`
- ACP: `http://acp-server:8080`
- MCP: `http://sage-mcp:3030`

Key state/config:

- `SAGE_STATE_DIR`, default `/sage-state`
- `SAGE_SOUL_PATH`, default `/home/node/.openclaw/workspace/SOUL.md`
- `SAGE_AGENT_REGISTRY_FILE`, default `/sage-state/workspace/sage/agents.registry.json`
- Bundled fallback registry: `config/agents.json`
- Bundled fallback prompts: `prompts/`
- Dispatch rolling context window:
  - `MANAGER_DISPATCH_ROLLING_MAX_MESSAGES` (default `12`)
  - `MANAGER_DISPATCH_ROLLING_MAX_CHARS` (default `700`)
- Delegated output behavior:
  - `SAGE_DELEGATED_PREFER_WORKER_REPLY` (default `true`)

## Provider Auth

Copilot auth is Sage-owned in this extracted project. The manager reads and writes provider state under `SAGE_STATE_DIR` and accepts env fallback from `COPILOT_GITHUB_TOKEN`, `GH_TOKEN`, or `GITHUB_TOKEN`.

The device login endpoints default to GitHub Copilot's OAuth client ID, so local setup does not require `GITHUB_CLIENT_ID`. Set `GITHUB_CLIENT_ID` only when intentionally testing a different GitHub OAuth app.

Dashboard-facing endpoints:

- `GET /providers/copilot/status`
- `POST /providers/copilot/login/start`
- `POST /providers/copilot/login/complete`
- `POST /providers/copilot/logout`
- `POST /providers/copilot/refresh`

Codex is available through a local bridge:

- `CODEX_BRIDGE_URL`, default in compose: `http://host.docker.internal:8765`
- `GET /providers/codex/status`
- `AGT-sage` default model: `codex/gpt-5.5`

Any configured agent can be switched to `codex/gpt-5.5` through `/agents/models`.
When Codex-backed agents need tools, the manager executes the existing approved
MCP/local tools and returns the results to the Codex bridge loop.

## API Groups

Core:

- `GET /health`
- `GET /stream`
- `POST /dispatch`
- `POST /agent-dispatch`
- `GET /orchestrator/errors`

Chat + sessions:

- `POST /chat`
- `POST /chat/continue`
- `POST /chat/stop`
- `GET /chat/sessions`
- `POST /chat/sessions`
- `GET|PATCH|DELETE /chat/sessions/{id}`

Catalogs + governance:

- `GET /agents/catalog`
- `GET|PATCH /agents/models`
- `GET|POST /tools/catalog`
- `PATCH|DELETE /tools/catalog/{id}`
- `POST /skills/compose`
- `GET|POST /skills/catalog`
- `GET|PATCH|DELETE /skills/catalog/{id}`
- `GET|POST /skills/discovered/servers`
- `PATCH /skills/discovered/servers/{id}`
- `POST /skills/discovered/servers/{id}/release`
- `POST /skills/discovered/servers/{id}/sync`
- `POST /skills/discovered/sync`
- `GET /skills/discovered/skills`
- `PATCH /skills/discovered/skills/{id}`

Workspace + work context:

- `GET /workspace/files/list`
- `GET /workspace/files/download`
- `GET /work-context/{id}`
- `POST /work-context/{id}/events`
- `GET /work-context/by-task/{taskId}`

## Validation

```powershell
go test ./...
```
