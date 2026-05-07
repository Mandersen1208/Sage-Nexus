# Sage Agent Mesh

This document is the cleanup-phase map for Sage's bounded agent mesh.

## Control Pattern

Sage uses a bounded peer mesh, not an open swarm and not a strict hierarchy.

- Sage front-of-house owns the human conversation.
- Orchestrator chooses the first owner for a task.
- Sage Nexus can explicitly target a registry-listed agent in Only or Flow mode.
- Domain agents own their specialty and may push back.
- Peer calls are consultations, not ownership transfers.
- Senior Dev owns quality approval for delivery work.
- Architect owns architecture shape and durable architecture documents.
- Research owns external/current evidence.
- Runtime Librarian owns inventory and architecture record indexing.

## Role Authority Matrix

| Agent | Owns | Must Not Own |
| --- | --- | --- |
| `AGT-project-manager-agent` | requirements, acceptance criteria, task breakdown | final architecture, implementation, quality approval |
| `AGT-architect-agent` | system boundaries, architecture docs, tradeoffs | code quality gate, implementation details |
| `AGT-senior-dev-agent` | code quality, maintainability, tests, delivery approval | frontend/backend/infra planning by default |
| `AGT-frontend-dev-agent` | UI behavior, accessibility, responsive implementation | backend contracts or data model ownership |
| `AGT-backend-dev-agent` | APIs, services, integration behavior, reliability | UI ownership or database admin policy |
| `AGT-devops-agent` | Docker, deployment, CI/CD, operations, rollback | app feature design |
| `AGT-database-admin-agent` | schema, migrations, query performance, data integrity | API/UI ownership |
| `AGT-qa-agent` | test strategy, reproduction, regression risk | implementation ownership |
| `AGT-research-agent` | current/external evidence and source synthesis | implementation, architecture decisions |
| `AGT-runtime-librarian-agent` | runtime inventory, prompt/config provenance, architecture indexing | planning, implementation, repair ownership |
| `AGT-office-document-agent` | DOCX/XLSX artifacts, workbook structure, formatted document packaging | research, architecture decisions, code implementation |
| `AGT-financial-agent` | personal finance and budget analysis | engineering mesh work |

## Tool Exposure Matrix

| Agent group | Tools |
| --- | --- |
| PM | skills, web search, work context, bounded peer calls |
| Architect | skills, runtime inventory, work context, bounded peer calls |
| Senior Dev | skills, web search, runtime inventory, work context, bounded peer calls |
| Frontend/Backend/DevOps/DBA/QA | skills, work context, bounded peer calls |
| Research | skills, web search, work context |
| Runtime Librarian | runtime inventory, work context |
| Office Document | Office artifact tools, work context |
| Financial | skills, web search, budget tools, work context |

Tool exposure is registry-driven. `toolBundles` and explicit `tools` define the requested MCP tools; manager startup discovers actual MCP schemas through `tools/list` and withholds unavailable tools. Peer mesh tools are controlled by `SAGE_AGENT_MESH_ENABLED` plus registry `peerTargets`. `SAGE_AGENT_MESH_MAX_DEPTH` can globally limit nested peer calls.

## Targeted Chat Modes

Sage Nexus reads `GET /agents/catalog` and does not hardcode agent names, labels, or supported mode choices.

| Mode | Behavior |
| --- | --- |
| `auto` | Sage front-of-house decides whether to answer directly or route through the mesh. |
| `solo` | User-facing label: Only. The selected agent answers directly. Peer dispatch tools are suppressed. |
| `launch` | User-facing label: Flow. The selected agent owns the flow. Registry peer policy and Senior Dev gate still apply. |

Registry fields `targetable`, `displayName`, `supportedChatModes`, `modeLabels`, and `modeDescriptions` drive the catalog. Sage supports `auto` and `solo`; workers can support `solo` and `launch`; non-owning agents like Librarian can be exposed as `solo` only.

## Peer-Call Decision Table

| Caller | Valid peers |
| --- | --- |
| PM | Architect, Senior Dev, Frontend, Backend, DevOps, QA, DBA, Office Document, Research |
| Architect | Senior Dev, Frontend, Backend, DevOps, QA, DBA, Office Document, Research |
| Senior Dev | PM, Architect, Frontend, Backend, DevOps, QA, DBA, Office Document, Research |
| Frontend | Architect, Backend, QA, Research |
| Backend | Architect, Frontend, DevOps, QA, DBA, Research |
| DevOps | Architect, Backend, QA, DBA, Research |
| DBA | Architect, Backend, DevOps, QA, Research |
| QA | Architect, Frontend, Backend, DevOps, DBA, Research |
| Office Document | PM, Architect, QA, Research |

Financial and Runtime Librarian are not general engineering peers.

## Research Evidence Rules

Research is required for decision-grade external/current claims:

- provider or model capabilities
- framework or library selection
- security posture
- vendor behavior
- licensing
- current documentation
- unfamiliar implementation patterns

The Research agent should append `research_brief` to Agent Work Context with sources, date, confidence, and impact. Other agents should read or request a brief before making those decisions.

## Runtime Evidence Flow

1. User asks Sage.
2. Orchestrator picks the owning specialist.
3. Owning specialist reads Agent Work Context.
4. If runtime truth matters, Architect/Senior Dev use runtime inventory.
5. If external/current facts matter, owning specialist calls Research.
6. If domain boundaries cross, owning specialist calls an allowlisted peer.
7. Peer findings are recorded as `peer_request`, `peer_response`, `domain_pushback`, or `research_brief`.
8. Senior Dev gates delivery work.
9. Runtime Librarian indexes approved architecture records when asked.

## Cleanup Watch List

- Agent registration now comes from the active registry, but stale docs/scripts may still mention worker maps or route maps.
- Tool exposure is registry-defined and MCP-discovered; stale Go schemas or stale bundle names are migration risks.
- Peer policy is registry-defined through `peerTargets`, with safety defaults in `peer_mesh.go`.
- Prompt files define authority, while the registry enforces route/tools/peer/gate policy.
- Runtime inventory is scan-based truth; Redis events are context metadata.
- Architecture documents are indexed when present, but doc creation/update remains workflow-driven.
- Research briefs are work-context records, not yet promoted to durable architecture docs automatically.
