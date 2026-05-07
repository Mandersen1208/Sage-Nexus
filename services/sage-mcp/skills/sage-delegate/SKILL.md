# Sage Delegate — Task Delegation to the Manager via ACP

Use this skill when you need to delegate a task to the Sage multi-agent manager for execution by a specialized sub-agent. The manager is an external Go process connected to the Sage manager and governed by ACP admission control.

---

## When to Delegate

Delegate to the manager when:
- The task requires capabilities beyond your current context (e.g., GitHub Copilot completions, code execution, long-running analysis)
- The task involves ACP-gated capabilities that require admission control approval before execution
- The user asks you to "run an agent", "delegate to Sage", or "use the manager"
- Multi-step orchestration is needed across specialized agents

Do NOT delegate for simple Q&A, summarization, or tasks you can handle directly.

---

## How to Delegate

Call the `sendToManager` tool with the task description. This tool is available in the Sage dashboard and publishes your request to the `sage:in` Redis channel, then waits for the manager's response on `sage:out`.

### Tool: `sendToManager`

```
sendToManager(content, opts?)
```

**Parameters:**
- `content` (string, required) — The task description or instruction for the manager. Be specific and include all relevant context. The manager will forward this to the appropriate sub-agent.
- `opts.sessionId` (string, optional) — Current session ID for context correlation.
- `opts.capability` (string, optional) — ACP capability being requested. Defaults to `acp:cap:skill.agent-delegate`.
- `opts.resource` (string, optional) — ACP resource scope. Defaults to `sage://workspace/*`.
- `opts.timeoutMs` (number, optional) — Response timeout in milliseconds. Default: 30000 (30s).

### Example

```
sendToManager("Analyze the authentication flow in src/auth/ and suggest improvements for security and maintainability", {
  sessionId: currentSessionId,
  capability: "acp:cap:skill.agent-delegate",
  resource: "sage://workspace/src/auth"
})
```

---

## Message Format (sage:in channel)

When published, the request appears on Redis as:

```json
{
  "request_id": "<uuid>",
  "session_id": "<optional session id>",
  "content": "<task description>",
  "capability": "acp:cap:skill.agent-delegate",
  "resource": "sage://workspace/*",
  "timestamp": 1712345678
}
```

---

## Response Format (sage:out channel)

The manager publishes a response after ACP admission and agent execution:

```json
{
  "requestId": "<same uuid>",
  "content": "<agent's response>",
  "agent": "AGT-sage-orchestrator",
  "status": "ok" | "error" | "escalate",
  "error": "<optional error message>"
}
```

**Status values:**
- `ok` — Task completed successfully. `content` contains the agent's response.
- `error` — Task failed. `error` contains the reason (may include ACP denial message).
- `escalate` — ACP requires human review before this action can proceed. Inform the user that approval is needed.

---

## ACP Admission

The manager enforces ACP admission before executing any task. The current default policy is:

| Risk Score | Decision  |
|------------|-----------|
| < 40       | APPROVED  |
| 40 – 69    | ESCALATED (human review required) |
| ≥ 70       | DENIED    |

If the response status is `escalate`, tell the user: "This action requires human approval through ACP before the agent can proceed."

If the response status is `error` and the error contains "ACP denied", tell the user the action was blocked by the institution's access control policy.

---

## Checking Bridge Status

The manager bridge must be connected before delegation is possible. The bridge is active when `SAGE_MANAGER_ENABLED=true` is set in the gateway environment. If `sendToManager` throws "Manager bridge not connected", the manager container may not be running or `REDIS_ADDR` may not be configured.

---

## Summary

1. Identify that the task should be delegated to a specialized agent
2. Call `sendToManager(taskDescription, { sessionId })`
3. Await the response (up to 30s by default)
4. If `status === "ok"` → return `content` to the user
5. If `status === "escalate"` → tell the user approval is required
6. If `status === "error"` → report the error and offer alternatives
