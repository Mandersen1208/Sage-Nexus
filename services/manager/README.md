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

## Provider Auth

Copilot auth is Sage-owned in this extracted project. The manager reads and writes provider state under `SAGE_STATE_DIR` and accepts env fallback from `COPILOT_GITHUB_TOKEN`, `GH_TOKEN`, or `GITHUB_TOKEN`.

The device login endpoints default to GitHub Copilot's OAuth client ID, so local setup does not require `GITHUB_CLIENT_ID`. Set `GITHUB_CLIENT_ID` only when intentionally testing a different GitHub OAuth app.

Dashboard-facing endpoints:

- `GET /providers/copilot/status`
- `POST /providers/copilot/login/start`
- `POST /providers/copilot/login/complete`
- `POST /providers/copilot/logout`
- `POST /providers/copilot/refresh`

## Validation

```powershell
go test ./...
```
