# Sage Architecture Docs

Use this skill when Sage needs to create, review, update, or reason from persistent architecture documents for a project.

Architecture docs are runtime memory for the project. They must live in the mounted workspace, not inside Docker image layers, so rebuilds and new containers keep the same architectural truth.

---

## Storage Rule

Persist project architecture under the Sage workspace:

```text
/sage-state/workspace/projects/<project-id>/architecture/
```

Recommended files:

```text
README.md
system-context.md
container-view.md
component-view.md
runtime-map.md
quality-attributes.md
operations.md
risks.md
adr/
```

Use repo-local paths when working outside the container, but the runtime source of truth is the mounted workspace path above.

---

## Roles

Architect owns structure and consistency:

- Creates or updates architecture docs.
- Uses current runtime inventory before proposing changes.
- Uses Research briefs before external/current architecture decisions.
- Records service boundaries, data flow, deployment shape, risks, and decisions.
- Updates the docs after any architecture-relevant change.

Senior dev owns feasibility approval:

- Reviews the architecture for implementation risk.
- Confirms the plan matches code reality.
- Pushes back on vague, overbuilt, or untestable designs.
- Approves before implementation work starts.

Runtime librarian owns source-of-truth indexing:

- Scans runtime, routes, services, prompts, MCP tools, mounts, env, Redis channels, and architecture docs.
- Scans the active agent registry, including routable agents, route tools, peer policy, senior-gate mode, and tool exposure.
- Answers "where does this come from?" and "what depends on this?"
- Flags drift between architecture docs and runtime truth.
- Records approved architecture plans as indexed project truth after senior review.

---

## Workflow

For new architecture or a meaningful change:

1. Architect or senior dev uses runtime inventory tools to gather current truth and likely touched paths.
2. If the architecture depends on provider/model, framework/library, security, vendor, licensing, current-docs, or unfamiliar implementation patterns, ask Research for a `research_brief`.
3. Architect drafts or updates the architecture document.
4. Senior dev reviews and approves, requests changes, or rejects.
5. If approved, hand the architecture update to the runtime librarian for indexing.
6. Librarian rescans and records the new architecture document as current project truth.
7. Implementation agents work from the approved document.
8. After implementation, architect updates the document with what actually changed.
9. Senior dev reviews the update.
10. Librarian indexes the updated document and flags drift.

Do not let implementation drift ahead of persistent architecture docs for cross-service or project-level changes.

---

## Document Standard

Use a lightweight blend of established patterns:

- arc42 style for durable sections: goals, constraints, context, solution strategy, building blocks, runtime view, deployment, risks.
- C4 style for maps: system context, containers/services, components where useful.
- ADRs for decisions: one decision per file, with context, decision, consequences, and status.
- Docs-as-code: plain Markdown stored in the workspace so it survives Docker rebuilds and can be indexed.

Keep docs useful and short. Do not produce ceremony. A document is good when senior devs can implement from it and the librarian can verify it against runtime truth.

---

## README.md Template

```markdown
# <Project Name> Architecture

Status: Draft | Approved | Superseded
Owner: AGT-architect-agent
Reviewer: AGT-senior-dev-agent
Last librarian scan: <timestamp or pending>

## Purpose

What this project/system does and why it exists.

## Current Runtime Truth

Summary from runtime librarian scan:

- Services:
- Routes:
- MCP tools:
- Data stores:
- External dependencies:
- Known config/mounts:

## Research Evidence

Decision-grade external/current evidence:

- Research brief:
- Sources:
- Confidence:
- Impact on selected approach:

## Architecture Summary

The short version of the design.

## Key Decisions

- ADR links:

## Implementation Boundaries

What belongs here and what explicitly does not.

## Risks

Known risks, open questions, and validation gaps.

## Validation

Commands, probes, tests, and runtime checks that prove the architecture matches reality.
```

---

## ADR Template

Store ADRs under:

```text
architecture/adr/0001-short-title.md
```

```markdown
# ADR 0001: <Decision>

Status: Proposed | Accepted | Rejected | Superseded
Date: YYYY-MM-DD
Owners: AGT-architect-agent, AGT-senior-dev-agent

## Context

What forced this decision.

## Decision

What we decided.

## Consequences

What becomes easier, harder, riskier, or more constrained.

## Runtime Links

- Services:
- Routes:
- MCP tools:
- Config/env:
- Files:

## Librarian Verification

Last scan:
Findings:
```

---

## Handoff Contract

Architect to research:

```text
Produce a decision-grade research brief for this architecture choice. Include sources, date, confidence, and impact on the decision. Append the brief to Agent Work Context as `research_brief`.
```

Architect to senior dev:

```text
Review this architecture update for feasibility, implementation risk, missing boundaries, and testability. Approve only if a dev agent could implement from it without guessing.
```

Senior dev to librarian:

```text
Index this approved architecture document and compare it against runtime inventory. Flag drift, missing services/routes/tools, stale config paths, or unverified assumptions.
```

Librarian back to architect:

```text
Here are the mismatches between architecture docs and runtime truth. Update the architecture document or mark the discrepancy as an intentional decision with an ADR.
```

---

## When To Require This Workflow

Use this workflow for:

- New projects.
- New services.
- New routes or public APIs.
- Docker/runtime topology changes.
- Agent mesh or MCP tool changes.
- Agent registry, routing, peer policy, or tool exposure changes.
- Storage, auth, permission, or data-flow changes.
- Any repair that changes system boundaries.

Skip it for small local bug fixes that do not change architecture. For source-of-truth questions, senior dev or architect should use runtime inventory directly; the librarian is the indexer/record keeper, not the default routing owner.
