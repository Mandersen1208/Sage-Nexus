# Sage Nexus Architecture

Sage Nexus is the standalone Sage runtime. It keeps the existing Sage design while removing OpenClaw as a required host application.

For current runtime behavior, endpoint inventory, active UI feature state, and known UX debt, see `docs/STATUS.md`.

## Control Plane

The Go manager owns orchestration. Sage is the front-of-house persona layer, not the orchestrator. The manager receives chat tasks, performs ACP admission, routes work through the agent registry, emits task lifecycle events, persists chat sessions, and exposes the dashboard HTTP API.

## Services (stable roles)

- `manager`: Go orchestration service on port `8090`.
- `acp-server`: Go admission service on port `8080`.
- `sage-mcp`: TypeScript MCP service on port `3030`.
- `dashboard`: React/nginx app on port `5174`.
- `redis`: task bus, event bus, chat/session store, work-context store.

## State (stable model)

Sage-owned durable state lives under `SAGE_STATE_DIR`, defaulting to `/sage-state` in containers.

Important paths:

- `/sage-state/auth/github-copilot.json`
- `/sage-state/credentials/github-copilot.token.json`
- `/sage-state/workspace/artifacts`
- `/sage-state/runtime-inventory.json`

The default SOUL path remains `/home/node/.openclaw/workspace/SOUL.md` for compatibility with the current real file. This is the only intentional OpenClaw path compatibility in v1.

## Agent Registry

The bundled registry remains in `services/manager/config/agents.json`. A workspace registry can overlay it through `SAGE_AGENT_REGISTRY_FILE`, defaulting to `/sage-state/workspace/sage/agents.registry.json`.

Adding an agent should remain registry-driven:

- prompt file
- registry entry
- tool bundles or explicit tools
- peer policy
- senior gate behavior
- targetable modes

Manager Go code should not grow per-agent route maps or static tool exposure switches.

## Provider Auth (architecture intent)

Copilot is the first standalone provider. The manager refreshes short-lived Copilot API tokens from Sage-owned GitHub auth state or supported env tokens. It calls `https://api.githubcopilot.com/chat/completions` directly.

Provider status and login lifecycle details are tracked in `docs/STATUS.md`.
