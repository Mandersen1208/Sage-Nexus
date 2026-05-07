# Migration Notes

This file captures extraction intent and migration context. For current runtime truth, route inventory, and active feature/UX state, use `docs/STATUS.md`.

## What Was Extracted

- `Sage-Agents` -> `services/manager`
- `acp-server` -> `services/acp-server`
- `sage-knowledge-mcp` -> `services/sage-mcp`
- `sage-dashboard` -> `apps/dashboard`

## What Changed

- Go module paths now use `github.com/matta/sage-nexus/...`.
- `OPENCLAW_STATE_DIR` was replaced by `SAGE_STATE_DIR`.
- Copilot auth no longer reads OpenClaw auth profiles.
- Runtime inventory now describes Sage runtime instead of Sage/OpenClaw runtime.
- Compose no longer starts OpenClaw gateway or mounts the OpenClaw repo.
- The only compatibility mount is the current SOUL workspace path.

## Compatibility

The default SOUL path remains:

```text
/home/node/.openclaw/workspace/SOUL.md
```

This lets Sage keep the existing persona file while the runtime moves to Sage-owned state. Later, move SOUL into Sage state and set:

```text
SAGE_SOUL_PATH=/sage-state/workspace/SOUL.md
```

## Parity Checklist

- `GET /health` returns ok.
- `/chat`, `/dashboard`, and `/models` refresh through the dashboard SPA.
- Chat sends and emits task lifecycle events.
- Central sessions persist through Redis and survive browser refresh.
- ACP admission registers manager and auto-issues a capability token.
- Copilot provider status reports connected when Sage auth store or env token is present.
- Runtime inventory scan finds Sage services, routes, agents, prompts, tools, SOUL source, Redis channels, and docs.

For an up-to-date operational checklist and current command set, see `docs/STATUS.md`.
