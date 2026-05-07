# Sage Nexus

Standalone Sage runtime extracted from OpenClaw.

Sage Nexus keeps the current Sage shape:

- Go manager as the orchestration/control plane.
- Sage front-of-house persona backed by `SOUL.md`.
- Go ACP admission service.
- TypeScript MCP tool service.
- Vite/React dashboard with chat, task telemetry, model controls, sessions, stop/continue, and targeted agent modes.
- Redis for task events, chat sessions, work context, and runtime state.

## Current Defaults

- Dashboard: `http://localhost:5174`
- Manager: `http://localhost:8090`
- Sage MCP: `http://localhost:3030`
- ACP server: `http://localhost:8080`
- Default SOUL path in containers: `/home/node/.openclaw/workspace/SOUL.md`
- Sage state path in containers: `/sage-state`

## Current Runtime Status

Canonical live-state and handoff documentation is in:

- `docs/STATUS.md`

Keep operational/runtime behavior updates in that file so `README` and architecture docs do not drift.

The SOUL path intentionally keeps compatibility with the current real file while the rest of the runtime moves to Sage-owned state.

## Run

```powershell
cd C:\Users\matta\code\sage-nexus
docker compose up --build
```

If your SOUL.md lives somewhere else on the host, set `SAGE_HOST_WORKSPACE` to the directory that contains it before starting compose.

## Copilot Auth

Sage Nexus owns Copilot auth under `SAGE_STATE_DIR` and no longer reads OpenClaw auth profiles.

Supported auth sources:

- Sage auth store: `/sage-state/auth/github-copilot.json`
- Cached Copilot token: `/sage-state/credentials/github-copilot.token.json`
- Env fallback: `COPILOT_GITHUB_TOKEN`, `GH_TOKEN`, or `GITHUB_TOKEN`

Provider status is exposed at:

```text
GET /providers/copilot/status
POST /providers/copilot/login/start
POST /providers/copilot/login/complete
POST /providers/copilot/logout
POST /providers/copilot/refresh
```

Device login defaults to the Copilot OAuth client flow. Set `GITHUB_CLIENT_ID` only when intentionally testing a different OAuth app. For a quick local smoke, set `GH_TOKEN` or `GITHUB_TOKEN`.

## Layout

```text
apps/dashboard        React Sage Nexus UI
services/manager      Go manager/orchestrator and Sage agents
services/acp-server   Go ACP admission service
services/sage-mcp     TypeScript MCP tools
configs               Extracted registry and prompts
docs                  Standalone architecture and migration notes
```
