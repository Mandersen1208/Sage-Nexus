package sageagents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSageSystemPromptPrefersSoulPath(t *testing.T) {
	t.Setenv("SAGE_SOUL_PATH", filepath.Join(t.TempDir(), "SOUL.md"))
	if err := os.WriteFile(os.Getenv("SAGE_SOUL_PATH"), []byte("real soul"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &AgentsConfig{}
	prompt, source := loadSageSystemPrompt(cfg)
	if prompt != "real soul" {
		t.Fatalf("expected SOUL.md prompt, got %q", prompt)
	}
	if source != os.Getenv("SAGE_SOUL_PATH") {
		t.Fatalf("expected source %q, got %q", os.Getenv("SAGE_SOUL_PATH"), source)
	}
}

func TestLoadSageSystemPromptFallsBackToBundledPrompt(t *testing.T) {
	dir := t.TempDir()
	fallbackPath := filepath.Join(dir, "sage.md")
	if err := os.WriteFile(fallbackPath, []byte("bundled sage"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SAGE_SOUL_PATH", filepath.Join(dir, "missing-SOUL.md"))

	cfg := &AgentsConfig{
		Agents: map[string]AgentConfig{
			SageAgentID: {SystemPromptFile: "sage.md"},
		},
		configDir: dir,
	}
	prompt, source := loadSageSystemPrompt(cfg)
	if prompt != "bundled sage" {
		t.Fatalf("expected bundled prompt, got %q", prompt)
	}
	if source != "bundled" {
		t.Fatalf("expected bundled source, got %q", source)
	}
}

func TestAutoFallbackWorkerIDPrefersProjectManager(t *testing.T) {
	runner := &SageRunner{Orchestrator: &SageOrchestratorAgent{Workers: map[string]*CopilotAgent{
		defaultSageAutoFallbackWorkerID: {BaseAgent: BaseAgent{AgentID: defaultSageAutoFallbackWorkerID}},
		"AGT-research-agent":            {BaseAgent: BaseAgent{AgentID: "AGT-research-agent"}},
	}}}

	if got := runner.autoFallbackWorkerID(); got != defaultSageAutoFallbackWorkerID {
		t.Fatalf("autoFallbackWorkerID() = %q, want %q", got, defaultSageAutoFallbackWorkerID)
	}
}

func TestAutoFallbackWorkerIDFallsBackToAnyAvailableWorker(t *testing.T) {
	runner := &SageRunner{Orchestrator: &SageOrchestratorAgent{Workers: map[string]*CopilotAgent{
		"AGT-research-agent": {BaseAgent: BaseAgent{AgentID: "AGT-research-agent"}},
	}}}

	if got := runner.autoFallbackWorkerID(); got != "AGT-research-agent" {
		t.Fatalf("autoFallbackWorkerID() = %q, want AGT-research-agent", got)
	}
}

func TestBuildAutoOrchestrationInputPreparesContextWithoutFinalizing(t *testing.T) {
	store := newPackageTestSessionStore()
	store.Append(context.Background(), "ctx-1", ChatMessage{Role: "user", Content: "previous request"})
	store.Append(context.Background(), "ctx-1", ChatMessage{Role: "assistant", Content: "previous reply"})
	tracker := NewHandoffTracker()
	runner := &SageRunner{
		Sage:         &CopilotAgent{BaseAgent: BaseAgent{AgentID: SageAgentID}},
		Orchestrator: &SageOrchestratorAgent{},
		Sessions:     store,
	}

	got, err := runner.BuildAutoOrchestrationInput(context.Background(), "ctx-1", "current request", tracker)
	if err != nil {
		t.Fatalf("BuildAutoOrchestrationInput() error = %v", err)
	}
	if !containsAllText(got, []string{"Recent chat context for continuity:", "previous request", "previous reply", "Current user request:", "current request"}) {
		t.Fatalf("auto orchestration input missing expected context:\n%s", got)
	}
	handoffs := tracker.Handoffs()
	if len(handoffs) != 1 || handoffs[0].AgentID != SageAgentID || handoffs[0].Role != "front-of-house" {
		t.Fatalf("unexpected handoffs: %+v", handoffs)
	}
}

func TestManagerExecutionResultRequiresSageFinalForAuto(t *testing.T) {
	tracker := NewHandoffTracker()
	tracker.Add(AgentHandoff{AgentID: SageAgentID, Role: "front-of-house"})
	tracker.Add(AgentHandoff{AgentID: OrchestratorAgentID, Role: "orchestrator"})
	tracker.Add(AgentHandoff{AgentID: "AGT-backend-dev-agent", Role: "worker"})

	worker := &CopilotAgent{BaseAgent: BaseAgent{AgentID: "AGT-backend-dev-agent"}}
	worker.toolTrace = []string{"agent_context_read"}
	orch := &SageOrchestratorAgent{
		Workers:    map[string]*CopilotAgent{"AGT-backend-dev-agent": worker},
		lastWorker: "AGT-backend-dev-agent",
		llm:        &CopilotAgent{BaseAgent: BaseAgent{AgentID: OrchestratorAgentID}, toolTrace: []string{"call_backend_dev_agent"}},
	}
	runner := &SageRunner{Orchestrator: orch}

	got := runner.managerExecutionResult("ctx-1", "raw manager result", nil, tracker)
	if got.Kind != ExecutionAgentic {
		t.Fatalf("Kind = %q, want %q", got.Kind, ExecutionAgentic)
	}
	if !got.RequiresSageFinal {
		t.Fatal("auto execution result must require Sage finalization")
	}
	if got.RawResult != "raw manager result" || got.RecommendedReply != "raw manager result" {
		t.Fatalf("unexpected execution text: %+v", got)
	}
	if got.WorkerAgentID != "AGT-backend-dev-agent" {
		t.Fatalf("WorkerAgentID = %q", got.WorkerAgentID)
	}
	if strings.Join(got.WorkerChain, ",") != "AGT-backend-dev-agent" {
		t.Fatalf("WorkerChain = %v", got.WorkerChain)
	}
	if strings.Join(got.ToolCalls, ",") != "call_backend_dev_agent,agent_context_read" {
		t.Fatalf("ToolCalls = %v", got.ToolCalls)
	}
}

func TestBuildManagerExecutionResultIncludesTaskID(t *testing.T) {
	runner := &SageRunner{Orchestrator: &SageOrchestratorAgent{}}

	got := runner.BuildManagerExecutionResult("ctx-1", "task-1", "manager result", nil, nil)
	if got.TaskID != "task-1" || got.ContextID != "ctx-1" {
		t.Fatalf("unexpected identifiers: %+v", got)
	}
	if !got.RequiresSageFinal {
		t.Fatal("manager execution result must require Sage finalization")
	}
}

func TestComposeFinalFromManagerResultDoesNotFallbackToRawManagerOutput(t *testing.T) {
	raw := "RAW MANAGER TEXT SHOULD NOT BE RETURNED DIRECTLY"
	runner := (*SageRunner)(nil)

	got, err := runner.ComposeFinalFromManagerResult(context.Background(), "fix dispatch", ManagerExecutionResult{
		Kind:              ExecutionAgentic,
		RawResult:         raw,
		RecommendedReply:  raw,
		RequiresSageFinal: true,
	})
	if err != nil {
		t.Fatalf("ComposeFinalFromManagerResult() error = %v", err)
	}
	if strings.Contains(got, raw) {
		t.Fatalf("fallback leaked raw manager output: %q", got)
	}
	if !strings.Contains(got, "Sage final response") {
		t.Fatalf("fallback should explain finalization failure, got %q", got)
	}
}

func TestBuildSageFinalizationMessagesStatesAutoBoundary(t *testing.T) {
	runner := &SageRunner{Sage: &CopilotAgent{SystemPrompt: "sage soul"}}

	messages, err := runner.buildSageFinalizationMessages("review the manager", ManagerExecutionResult{
		Kind:              ExecutionAgentic,
		RawResult:         "manager found worker output",
		RequiresSageFinal: true,
	})
	if err != nil {
		t.Fatalf("buildSageFinalizationMessages() error = %v", err)
	}
	joined := ""
	for _, message := range messages {
		joined += message.Content + "\n"
	}
	for _, want := range []string{
		"Sage is the entry and exit interface",
		"The manager handles routing and execution",
		"Do not claim to be the manager or router",
		"manager found worker output",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("finalization prompt missing %q:\n%s", want, joined)
		}
	}
}

type packageTestSessionStore struct {
	messages map[string][]ChatMessage
}

func newPackageTestSessionStore() *packageTestSessionStore {
	return &packageTestSessionStore{messages: map[string][]ChatMessage{}}
}

func (s *packageTestSessionStore) Append(_ context.Context, contextID string, msg ChatMessage) {
	s.messages[contextID] = append(s.messages[contextID], msg)
}

func (s *packageTestSessionStore) Load(_ context.Context, contextID string) []ChatMessage {
	existing := s.messages[contextID]
	out := make([]ChatMessage, len(existing))
	copy(out, existing)
	return out
}

func (s *packageTestSessionStore) Trim(_ context.Context, contextID string, maxTurns int) {
	if maxTurns <= 0 {
		return
	}
	items := s.messages[contextID]
	if len(items) <= maxTurns {
		return
	}
	s.messages[contextID] = append([]ChatMessage{}, items[len(items)-maxTurns:]...)
}

func containsAllText(text string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}

func TestWrapDelegatedReplyAddsSageVoiceAroundArtifact(t *testing.T) {
	wrapped := wrapDelegatedReply("```yaml\nmode: auto\n```")
	if wrapped == "```yaml\nmode: auto\n```" {
		t.Fatal("wrapDelegatedReply() returned raw artifact without Sage wrapper")
	}
	if got := wrapped; got == "" {
		t.Fatal("wrapDelegatedReply() returned empty string")
	}
}
