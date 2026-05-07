# Sage Nexus Status

Last updated: 2026-05-07

This is the canonical runtime status and handoff document for Sage Nexus. Keep this file current and use it as the source of truth for operational behavior and active feature state.

## Runtime Matrix

| Service | Port | Purpose |
| --- | --- | --- |
| `dashboard` | `5174` | React UI (`/chat`, `/dashboard`, `/models`, `/files`, `/settings`) |
| `manager` | `8090` | Orchestrator/control plane and HTTP API |
| `sage-mcp` | `3030` | MCP tools service |
| `acp-server` | `8080` | Admission control |
| `redis` | `6379` | Sessions, events, work context |

### Key Environment and Mounted Roots

| Key | Current meaning |
| --- | --- |
| `SAGE_SOUL_PATH` | SOUL source path inside container. Default `/home/node/.openclaw/workspace/SOUL.md`. |
| `SAGE_STATE_DIR` | Sage-owned state root. Default `/sage-state`. |
| `SAGE_FILE_BROWSER_ROOT` | File browser root in manager. Current `/code`. |
| `SAGE_HOST_CODE_ROOT` | Host bind source for `/code` (default `C:/Users/matta/code`). |
| `SAGE_HOST_WORKSPACE` | Host bind source for SOUL workspace compatibility mount. |

## API and Route Surface

### Manager HTTP (current)

- `GET /health`
- `POST /chat`
- `POST /chat/continue`
- `POST /chat/stop`
- `GET /stream`
- `GET /agents/catalog`
- `GET /agents/models`
- `PATCH /agents/models`
- `GET /chat/sessions`
- `POST /chat/sessions`
- `PATCH /chat/sessions/{id}`
- `DELETE /chat/sessions/{id}`
- `GET /workspace/files/list`
- `GET /workspace/files/download`
- `GET /providers/copilot/status`
- `POST /providers/copilot/login/start`
- `POST /providers/copilot/login/complete`
- `POST /providers/copilot/logout`
- `POST /providers/copilot/refresh`
- `GET /work-context/{id}`
- `POST /work-context/{id}/events`
- `GET /work-context/by-task/{taskId}`

### Dashboard SPA routes

- `/chat`
- `/dashboard`
- `/models`
- `/files`
- `/settings`

## Feature Inventory by UI Page

### Chat (`/chat`)

- Central chat session view with create/switch/delete and persisted session metadata.
- Targeted agent mode support (`auto`, `solo`, `launch`) from `GET /agents/catalog`.
- Message send, stream updates, stop handling, and image attachment support.

### Dashboard (`/dashboard`)

- Task timeline and event chain view from stream/task state.
- Agent chain and intermediate orchestration visibility.
- Error feed and lifecycle diagnostics.

### Models (`/models`)

- View current model assignments by agent.
- Update model overrides through manager model API.

### Files (`/files`)

- Lists and navigates files under `SAGE_FILE_BROWSER_ROOT` (`/code`).
- Download file or folder archive from manager endpoints.
- Breadcrumb navigation, refresh action, copy path, and responsive table/card layout.

### Settings (`/settings`)

- Copilot auth status, device login start/complete, refresh, logout.
- MCP tools CRUD and enable/disable controls.
- Local MCP launch setup fields (workspace root, command, args, auto-start).
- Skills CRUD editor for MCP skills and `SKILL.md` content.

### Last-night additions that are currently present

- File browser route/page with manager-backed list/download.
- Expanded settings surfaces for local tool setup and skill editing.

## Known UX Debt

### SettingsPage (`apps/dashboard/src/SettingsPage.tsx`)

- **Severity: High**
  - Scope is too broad for one page (provider auth + local MCP setup + tools CRUD + skills CRUD).
  - Dense table/forms reduce scanability and make mobile ergonomics weak.
  - Multiple concepts are mixed without clear workflow boundaries.
- **Impact**
  - High cognitive load for basic admin actions.
  - Harder onboarding for new users and slower task completion.
- **Intended fix direction**
  - Split into tabs/subroutes (`Provider`, `Tools`, `Skills`, optional `Local MCP`).
  - Add clearer section ownership and progressive disclosure for advanced options.
  - Improve mobile interaction for tool/skill actions.

### FilesPage (`apps/dashboard/src/FilesPage.tsx`)

- **Severity: Medium**
  - Behavior is functional but currently very broad by default (repo/system folders shown immediately).
  - Missing first-class filter controls (hidden/system files, file type, search).
- **Impact**
  - Fast to browse, but noisy and less task-focused in large code roots.
- **Intended fix direction**
  - Add quick filters (`hide dotfiles`, `hide dependency dirs`, `show dirs first` toggle).
  - Add search and optional root shortcuts for common repos.

## Recent Backend Capability Additions

- File browser root expanded to `/code` via `SAGE_FILE_BROWSER_ROOT`, with host bind from `SAGE_HOST_CODE_ROOT`.
- Office document agent now supports calendar artifacts via `office_ics_create` in addition to `office_docx_create` and `office_xlsx_create`.
- Copilot device login defaults to the Copilot OAuth client behavior so custom `GITHUB_CLIENT_ID` is optional for normal flows.

## Verification Checklist

Run from `C:\Users\matta\code\sage-nexus`:

```powershell
docker compose up -d --build
Invoke-RestMethod http://localhost:8090/health
Invoke-RestMethod http://localhost:8090/agents/catalog
Invoke-RestMethod http://localhost:8090/workspace/files/list
Invoke-RestMethod http://localhost:8090/providers/copilot/status
```

Manual route refresh checks:

- `http://localhost:5174/chat`
- `http://localhost:5174/dashboard`
- `http://localhost:5174/models`
- `http://localhost:5174/files`
- `http://localhost:5174/settings`

## Next Cleanup Targets

1. Split settings UI into focused subviews and reduce per-page density.
2. Add file browser filters/search and optional favorite roots.
3. Tighten copy and interaction consistency across settings/files actions.
4. Add a lightweight docs CI check to ensure endpoint lists in this file remain current.
