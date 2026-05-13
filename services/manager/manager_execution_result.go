package sageagents

// ManagerExecutionKind describes the manager-side outcome before Sage turns it
// into the final user-facing response.
type ManagerExecutionKind string

const (
	ExecutionDirect  ManagerExecutionKind = "direct"
	ExecutionAgentic ManagerExecutionKind = "agentic"
	ExecutionClarify ManagerExecutionKind = "clarify"
	ExecutionPaused  ManagerExecutionKind = "paused"
	ExecutionError   ManagerExecutionKind = "error"
)

// ManagerExecutionResult is the boundary between manager execution and Sage
// front-of-house response composition. RawResult is execution source material,
// not the final Sage Auto chat response.
type ManagerExecutionResult struct {
	Kind              ManagerExecutionKind `json:"kind"`
	TaskID            string               `json:"task_id,omitempty"`
	ContextID         string               `json:"context_id,omitempty"`
	RawResult         string               `json:"raw_result"`
	RecommendedReply  string               `json:"recommended_reply,omitempty"`
	WorkerAgentID     string               `json:"worker_agent_id,omitempty"`
	WorkerChain       []string             `json:"worker_chain,omitempty"`
	ToolCalls         []string             `json:"tool_calls,omitempty"`
	RequiresSageFinal bool                 `json:"requires_sage_final"`
	PausedReason      string               `json:"paused_reason,omitempty"`
	Error             string               `json:"error,omitempty"`
}
