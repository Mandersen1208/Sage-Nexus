# Sage Nexus Dashboard

Vite/React frontend for Sage Nexus.

The dashboard is presentation-only. It renders the manager's catalog, session,
model, provider, chat, and stream APIs. Agent names, mode labels, targetability,
and mode availability should come from `/agents/catalog`; do not hardcode new
agent choices in the UI.

## Routes

- `/chat` - primary Sage chat interface with shared sessions and mode selection.
- `/dashboard` - task telemetry, timelines, event bus, handoffs, continue/stop.
- `/models` - per-agent model configuration.
- `/files` - manager-backed file browser/download UI.
- `/skills` - local + discovered skill governance workflows.
- `/settings` - provider auth and tool capability management.

Nginx serves this as an SPA, so direct refresh on these routes should resolve to
`index.html`.

## Runtime API

The frontend talks to the manager through `VITE_MANAGER_URL`; Docker defaults to
same-origin `/api` proxying through nginx.

Core calls live in `src/api.ts`:

- chat submit/continue/stop
- chat sessions
- stream events
- agent catalog and model catalog
- Copilot provider status
- tool catalog CRUD
- local skill compose/catalog CRUD
- discovered skill source + skill governance APIs
- manager `/dispatch` for manager-chat-assisted drafting

## Current UX Surfaces

- Add Tool modal includes manager-chat-assisted drafting with session continuity.
- Skills page is the primary governance page (local skills + discovered server skills).
- Settings no longer hosts full skill governance workflows.
- Chat renders live model deltas in one bottom "Live output" box above the
  composer. Those deltas are volatile display state; final assistant messages
  still come from completed task artifacts or persisted chat transcript rows.

## Validation

```bash
npm run build
```
