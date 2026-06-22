# Sage Nexus Architecture

Sage Nexus is the standalone Sage runtime. It keeps the existing Sage design while removing OpenClaw as a required host application.

For current runtime behavior, endpoint inventory, active UI feature state, and known UX debt, see `docs/STATUS.md`.

## Control Plane

The Go manager owns routing and agent handoff execution. Sage is the front-of-house persona layer, not the router. The manager receives chat tasks, performs ACP admission, picks the first task owner from the agent registry, dispatches agent-owned handoffs through Redis/work context, emits task lifecycle events, persists chat sessions, and exposes the dashboard HTTP API.

Chat mode semantics are intentionally split:

- `Sage Only` is direct persona chat with Sage. Worker routing and flow launch stay off.
- `Sage Auto` sends the turn through Sage framing, the manager's initial router, and the agent-owned handoff runtime. Sage is the face of the experience, not the routing brain.

## Services (stable roles)

- `manager`: Go orchestration service on port `8090`.
- `acp-server`: Go admission service on port `8080`.
- `sage-mcp`: TypeScript MCP service on port `3030` (tools + canonical skill discovery/indexing).
- `dashboard`: React/nginx app on port `5174`.
- `redis`: task bus, event bus, chat/session store, work-context store, governance policy state.
- `skills-db` (pgvector): canonical skill source/skill registry and embeddings.

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

The manager-owned registry and prompt files are the only bundled runtime source
of truth. Example configs live under `examples/` and are not loaded by default.

Adding an agent should remain registry-driven:

- prompt file
- registry entry
- tool bundles or explicit tools
- peer policy
- handoff policy
- targetable modes

Manager Go code should not grow per-agent route maps or static tool exposure switches.

## Capability Discovery and Governance

Sage MCP is the canonical capability aggregation layer. It polls local skills and configured external MCP servers, normalizes them into canonical skill records, stores embeddings in pgvector, and exposes search/list/update flows.

Manager owns policy and orchestration-time consumption:

- server governance (`enabled/disabled`, trust, sync status),
- skill governance (`quarantined/released/disabled`, optional allowlist),
- release workflows (bulk by server and per-skill),
- retrieval pre-injection into orchestration input.

Default scheduler behavior is daily sync at local `2:00 AM`, with manual sync endpoints for immediate refresh.

## Provider Auth (architecture intent)

Copilot is the first standalone provider. The manager refreshes short-lived Copilot API tokens from Sage-owned GitHub auth state or supported env tokens. It calls `https://api.githubcopilot.com/chat/completions` directly.

Codex is the first subscription-backed smoke provider. Any configured agent can
use provider refs such as `codex/gpt-5.5`, routed through a host-side
`codex-bridge` that shells out to the locally signed-in Codex CLI. Tool use stays
manager-mediated: Codex requests tools through a structured bridge response, and
the manager executes the same approved MCP/local tools used by Copilot-backed
agents.

Provider status and login lifecycle details are tracked in `docs/STATUS.md`.
