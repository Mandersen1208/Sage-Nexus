// Package sageagents implements the Sage Nexus manager and worker runtime.
//
// Sage Nexus keeps three boundaries deliberately separate:
//
//   - Sage front-of-house owns voice, user-facing session continuity, and the
//     choice to answer directly or delegate.
//   - The manager owns orchestration, admission control, session persistence,
//     Agent Work Context, streaming events, and provider auth.
//   - MCP tools own deterministic capabilities such as runtime inventory,
//     artifact creation, context reads/writes, and specialist helper tools.
//
// The package is standalone. It must not require OpenClaw state or provider
// plugins at runtime. Compatibility with the existing SOUL file is handled only
// through SAGE_SOUL_PATH, whose default currently points at the existing mounted
// workspace path.
//
// # Request Flow
//
// A chat request enters through cmd/manager over POST /chat. The manager creates
// or resumes the human chat session, opens a per-task Agent Work Context, emits
// stream events to Redis, and runs Sage front-of-house.
//
// Sage front-of-house can:
//
//   - answer directly for narrow conversational turns,
//   - call the orchestrator for normal delegated work,
//   - run a selected target agent in solo mode, or
//   - launch a selected agent-owned flow.
//
// Delegated work is routed from registry data, not hardcoded worker maps. The
// registry controls targetability, route tools, peer policy, senior-gate mode,
// tool bundles, explicit tools, and agent authority metadata.
//
// # State Ownership
//
// SAGE_STATE_DIR is the root for Sage-owned mutable state. The manager stores
// provider auth material, session metadata, work-context metadata, and generated
// runtime files under this root or Redis-backed keys. Provider code must not
// reach into OpenClaw auth profiles.
//
// Human chat sessions and Agent Work Context are separate by design. Chat
// sessions preserve conversation continuity with Sage. Work Context is per-task
// execution memory shared by agents and MCP tools through scoped tokens.
//
// # ACP and Provider Auth
//
// ACP admission remains the manager-side guard around delegated execution. The
// manager registers its Ed25519 identity with the ACP server and can auto-issue
// a local capability token from the standalone ACP service when a token is not
// supplied explicitly.
//
// Copilot auth is Sage-owned. The manager reads short-lived Copilot API tokens
// from SAGE_STATE_DIR/credentials, refreshes them from a stored GitHub OAuth
// token under SAGE_STATE_DIR/auth, and falls back to COPILOT_GITHUB_TOKEN,
// GH_TOKEN, or GITHUB_TOKEN when no stored auth exists.
package sageagents
