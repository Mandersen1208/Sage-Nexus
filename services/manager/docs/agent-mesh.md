# Sage Agent Mesh

This document is the cleanup-phase map for Sage's bounded agent mesh.

## Control Pattern

Sage uses a bounded peer mesh, not an open swarm and not a strict hierarchy.

- Sage front-of-house owns the human conversation.
- The manager initial router chooses the first owner for a task.
- Sage Nexus can explicitly target a registry-listed agent in Only or Flow mode.
- Domain agents own their specialty and may push back.
- Agent handoffs are ownership transfers for the next task slice.
- Legacy peer calls are still available only when explicitly exposed, but the default mesh uses `handoff_to_agent`.
- Senior Dev owns review quality and risk judgment, not a mandatory approval gate.
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
| PM | skills, web search, work context, agent handoff |
| Architect | skills, runtime inventory, work context, agent handoff |
| Senior Dev | skills, web search, runtime inventory, work context, agent handoff |
| Frontend/Backend/DevOps/DBA/QA | skills, work context, agent handoff |
| Research | skills, web search, work context |
| Runtime Librarian | runtime inventory, work context |
| Office Document | Office artifact tools, work context |
| Financial | skills, web search, budget tools, work context |

Tool exposure is registry-driven. `toolBundles` and explicit `tools` define the requested MCP tools; manager startup discovers actual MCP schemas through `tools/list` and withholds unavailable tools. Handoff tools are controlled by `SAGE_AGENT_MESH_ENABLED` plus registry `peerTargets`. `SAGE_AGENT_MESH_MAX_DEPTH` can globally limit nested handoffs.

## Targeted Chat Modes

Sage Nexus reads `GET /agents/catalog` and does not hardcode agent names, labels, or supported mode choices.

| Mode | Behavior |
| --- | --- |
| `auto` | Sage frames the request, the manager picks the first owner, agents hand off through shared work context, and Sage revoices the final result. |
| `solo` | User-facing label: Only. The selected agent answers directly. Peer dispatch tools are suppressed. |
| `launch` | User-facing label: Flow. The selected agent owns the flow and can hand off to allowlisted peers. |

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
2. Manager initial router picks the first owning specialist or Sage.
3. Owning specialist reads Agent Work Context.
4. If another owner is needed, the current agent appends context and calls `handoff_to_agent`.
5. The manager publishes the handoff event and dispatches the next in-process agent with the same tokenized work context.
6. This continues until an agent calls `complete_task` or returns a final result with no accepted handoff.
7. Sage revoices the final result for the user.
8. Runtime Librarian indexes approved architecture records when asked.

## Cleanup Watch List

- Agent registration now comes from the active registry, but stale docs/scripts may still mention worker maps or route maps.
- Tool exposure is registry-defined and MCP-discovered; stale Go schemas or stale bundle names are migration risks.
- Peer policy is registry-defined through `peerTargets`, with safety defaults in `peer_mesh.go`.
- Prompt files define authority, while the registry enforces route/tools/peer/gate policy.
- Runtime inventory is scan-based truth; Redis events are context metadata.
- Architecture documents are indexed when present, but doc creation/update remains workflow-driven.
- Research briefs are work-context records, not yet promoted to durable architecture docs automatically.
