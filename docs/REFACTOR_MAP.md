# Sage Nexus Refactor Map

This map records the current file ownership boundaries before deeper cleanup.
It is intentionally concrete: each entry names what the file owns today and the
next safe direction for refactor work.

## Source Of Truth

- Runtime compose wiring: `docker-compose.yml`
- Host startup/teardown: `startup.ps1`
- Manager registry: `services/manager/config/agents.json`
- Manager prompts: `services/manager/prompts/`
- Sage MCP skills: `services/sage-mcp/skills/`
- Example external config: `examples/sage.config.example.json`

The old root-level `configs/manager` and `configs/prompts` copies were removed
because they were not referenced by runtime code and had drifted from the
manager-owned copies.

## Root

- `docker-compose.yml`: service topology, ports, volumes, environment.
- `startup.ps1`: local lifecycle wrapper for Codex bridge plus Docker compose.
- `README.md`: operator overview and run instructions.
- `docs/`: architecture, migration, provider setup, and refactor notes.
- `examples/`: non-runtime examples that should not be treated as live config.

## Manager Package

- `acp_client.go`: ACP admission and execution token client.
- `agent_registry.go`: registry loading, overlay merge, route tool generation,
  peer/handoff policy.
- `base_agent.go`: agent identity, ACP registration, MCP connection bootstrap.
- `chat_modes.go`: chat catalog and solo/auto/launch target validation.
- `codex_bridge_client.go`: manager-side HTTP client for the host Codex bridge.
- `common.go`: shared DTOs and environment helpers.
- `config.go`: config structs and constants.
- `copilot_agent.go`: Copilot/Codex-backed chat loop, tool call loop, and tool
  argument injection.
- `copilot_models.go`: Copilot model discovery.
- `copilot_token.go`: GitHub OAuth/device auth and Copilot runtime token cache.
- `error_bus.go`: structured error events and context publisher.
- `manager.go`: agent registry, health/log endpoints, and capability selection.
- `manager_execution_result.go`: Sage finalization result contract.
- `mcp_client.go`: HTTP MCP tool list/call adapter.
- `peer_mesh.go`: peer-call allowlist and depth policy.
- `progress.go`: progress events, latency spans, A2A status summaries.
- `provider_model.go`: provider/model reference parsing and validation.
- `sage_front_of_house.go`: Sage direct/delegated chat behavior and final voice.
- `sage_orchestrator_agent.go`: legacy worker routing, deterministic fallback,
  and worker dispatch.
- `sessions.go`: Redis-backed chat transcript and session metadata stores.
- `work_context.go`: Redis-backed work context, redaction, token authorization.

## Manager Commands

- `cmd/manager/main.go`: application assembly plus remaining HTTP/A2A runtime.
  This is still the largest file and should be the next extraction target.
- `cmd/manager/provider_auth_routes.go`: provider auth/status HTTP routes.
- `cmd/manager/task_runtime_registry.go`: in-memory cancellation and
  continuation registries for active runs.
- `cmd/manager/active_task_store.go`: Redis active task/run pointer lifecycle.
- `cmd/manager/agent_models.go`: model catalog and override routes.
- `cmd/manager/skill_catalog.go`: editable local skill catalog routes.
- `cmd/manager/skill_discovery_catalog.go`: discovered skill/source governance.
- `cmd/manager/tool_catalog.go`: custom tool catalog routes.
- `cmd/codex-bridge/main.go`: host-side bridge from manager HTTP calls to
  `codex exec`.
- `cmd/agent/main.go`: standalone worker process entrypoint.

## Sage MCP

- `src/index.ts`: MCP HTTP/stdio server and skill discovery API routing.
- `src/tools/agent-call.ts`: manager-mediated agent-to-agent tool.
- `src/tools/agent-context.ts`: work context read/append/search tools.
- `src/tools/delegate.ts`: delegate task publishing and task stream handling.
- `src/tools/delegate-continue.ts`: paused task continuation control.
- `src/tools/office.ts`: DOCX/XLSX/ICS artifact tools.
- `src/tools/runtime-inventory.ts`: runtime scan and inventory snapshot.
- `src/tools/skills.ts`: local skill registry MCP tools.
- `src/tools/websearch.ts`: SearXNG-backed web search tool.
- `src/services/skill-discovery.ts`: canonical skill source/skill persistence.
- `src/services/skills.ts`: bundled/overlay skill reader.

## Dashboard

- `src/App.tsx`: dashboard timeline, stream view, task detail, route shell.
- `src/Chat.tsx`: chat sessions, transcript persistence, mode selection.
- `src/SettingsPage.tsx`: provider auth, tools, skills, local setup UI.
- `src/SkillsPage.tsx`: discovered skill/source governance UI.
- `src/AgentModelsPage.tsx`: per-agent model override UI.
- `src/FilesPage.tsx`: workspace file browser.
- `src/api.ts`: manager API client.
- `src/hooks/useStream.ts`: event stream state reducer.
- `src/types.ts`: shared frontend DTOs.

## Next Refactor Sequence

1. Split `cmd/manager/main.go` into route groups:
   chat sessions, chat actions, workspace browser, work context, A2A dispatch.
2. Split `copilot_agent.go` into provider calls, tool loop, and tool argument
   injection.
3. Split `sage_front_of_house.go` into route classification, delegated
   finalization, and session persistence.
4. Split legacy `sage_orchestrator_agent.go` into reusable routing, worker
   dispatch, and fallback policy pieces if the rollback path remains.
5. Split dashboard `Chat.tsx` and `SettingsPage.tsx` into focused components
   after backend contracts settle.
