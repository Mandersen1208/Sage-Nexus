# Sage Nexus Dashboard

Vite/React frontend for Sage Nexus.

The dashboard is presentation-only. It renders the manager's catalog, session,
model, provider, chat, and stream APIs. Agent names, mode labels, targetability,
and mode availability should come from `/agents/catalog`; do not hardcode new
agent choices in the UI.

## Routes

- `/chat` - primary Sage chat interface.
- `/dashboard` - task telemetry, timelines, event bus, handoffs, continue/stop.
- `/models` - per-agent model configuration.

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

## Validation

```bash
npm run build
```
