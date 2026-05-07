// Package main implements the standalone Sage ACP service.
//
// ACP is the local admission-control wall between the manager and delegated
// agent execution. It stores registered agent public keys, issues challenges,
// verifies Ed25519 proof-of-possession signatures, creates capability tokens,
// and consumes execution tokens after admitted work completes.
//
// The extraction keeps ACP as its own service so Sage Nexus can preserve the
// existing challenge/verify/capability-token/execution-token flow without
// depending on OpenClaw runtime services.
//
// # Runtime State
//
// The service writes its local database under /data in Docker. Treat this as
// ACP-owned state. Manager and MCP services should only communicate with ACP
// through the HTTP API, never by reading ACP files directly.
package main
