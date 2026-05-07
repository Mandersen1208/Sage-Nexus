package sageagents

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

// SageOrchestratorAgent routes incoming requests to specialist worker agents
// using registry-generated internal routing tools. Workers run in-process.
type SageOrchestratorAgent struct {
	BaseAgent
	Manager      *Manager
	Workers      map[string]*CopilotAgent
	Registry     *AgentsConfig
	RouteTools   []toolDef
	RouteTargets map[string]string
	Model        string
	SystemPrompt string

	llm        *CopilotAgent
	lastWorker string
	lastReply  string // text of the most recent successful worker reply — preserved across orchestration failures

	// Error-memory state — populated by the sage:errors subscriber started via
	// StartErrorSubscriber. The orchestrator owns this because it already acts
	// as the system brain (worker routing + handoff tracking); error awareness
	// is the natural next layer.
	errorsMu        sync.RWMutex
	errorBuffer     []ErrorEvent
	errorMaxSize    int
	errorsStartedAt time.Time
}

type WorkerDispatchOptions struct {
	Mode              string
	RouteTool         string
	SuppressPeerTools bool
	EnforceSeniorGate bool
}

// OrchestratorErrorStats summarizes errors the orchestrator has seen within a
// rolling window. Exposed via /orchestrator/errors.
type OrchestratorErrorStats struct {
	WindowMinutes int            `json:"window_minutes"`
	Total         int            `json:"total"`
	ByKind        map[string]int `json:"by_kind,omitempty"`
	ByAgent       map[string]int `json:"by_agent,omitempty"`
	ByTool        map[string]int `json:"by_tool,omitempty"`
}

// ErrorsStartedAt reports when the error subscriber was started (unix seconds,
// zero if never started). Used by the /orchestrator/errors endpoint.
func (a *SageOrchestratorAgent) ErrorsStartedAt() int64 {
	a.errorsMu.RLock()
	defer a.errorsMu.RUnlock()
	if a.errorsStartedAt.IsZero() {
		return 0
	}
	return a.errorsStartedAt.Unix()
}

// StartErrorSubscriber spawns a goroutine that subscribes to ChannelErrors and
// appends decoded events into the orchestrator's ring buffer. Idempotent —
// calling twice is a no-op (second call logs and returns). Runs until ctx is
// cancelled or the Redis client is closed.
func (a *SageOrchestratorAgent) StartErrorSubscriber(ctx context.Context, rc *redis.Client) {
	a.errorsMu.Lock()
	if !a.errorsStartedAt.IsZero() {
		a.errorsMu.Unlock()
		log.Printf("[orchestrator] StartErrorSubscriber called twice — ignored")
		return
	}
	if a.errorMaxSize <= 0 {
		a.errorMaxSize = 500
	}
	a.errorsStartedAt = time.Now()
	a.errorsMu.Unlock()

	go func() {
		ps := rc.Subscribe(ctx, ChannelErrors)
		defer ps.Close()
		log.Printf("[orchestrator] subscribed to %s (buffer=%d)", ChannelErrors, a.errorMaxSize)
		for {
			msg, err := ps.ReceiveMessage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("[orchestrator] error subscriber recv failed: %v (retrying in 1s)", err)
				time.Sleep(time.Second)
				continue
			}
			var evt ErrorEvent
			if err := json.Unmarshal([]byte(msg.Payload), &evt); err != nil {
				log.Printf("[orchestrator] error subscriber decode failed: %v  payload=%.160s", err, msg.Payload)
				continue
			}
			a.recordError(evt)
			log.Printf("[orchestrator] recv %s kind=%s agent=%s tool=%s err=%.100s", ChannelErrors, evt.Kind, evt.Agent, evt.Tool, evt.Error)
		}
	}()
}

// recordError appends evt to the ring buffer, dropping the oldest entry once
// the cap is reached.
func (a *SageOrchestratorAgent) recordError(evt ErrorEvent) {
	a.errorsMu.Lock()
	defer a.errorsMu.Unlock()
	if a.errorMaxSize <= 0 {
		a.errorMaxSize = 500
	}
	a.errorBuffer = append(a.errorBuffer, evt)
	if len(a.errorBuffer) > a.errorMaxSize {
		overflow := len(a.errorBuffer) - a.errorMaxSize
		a.errorBuffer = a.errorBuffer[overflow:]
	}
}

// RecentErrors returns up to `limit` most-recent events, newest last. A copy
// so callers cannot mutate internal state.
func (a *SageOrchestratorAgent) RecentErrors(limit int) []ErrorEvent {
	a.errorsMu.RLock()
	defer a.errorsMu.RUnlock()
	if limit <= 0 || limit > len(a.errorBuffer) {
		limit = len(a.errorBuffer)
	}
	out := make([]ErrorEvent, limit)
	copy(out, a.errorBuffer[len(a.errorBuffer)-limit:])
	return out
}

// ErrorStats computes counts over the trailing window. All events newer than
// (now - window) are included.
func (a *SageOrchestratorAgent) ErrorStats(window time.Duration) OrchestratorErrorStats {
	a.errorsMu.RLock()
	defer a.errorsMu.RUnlock()
	cutoff := time.Now().Add(-window).Unix()
	stats := OrchestratorErrorStats{
		WindowMinutes: int(window.Minutes()),
		ByKind:        map[string]int{},
		ByAgent:       map[string]int{},
		ByTool:        map[string]int{},
	}
	for _, e := range a.errorBuffer {
		if e.Timestamp < cutoff {
			continue
		}
		stats.Total++
		if e.Kind != "" {
			stats.ByKind[e.Kind]++
		}
		if e.Agent != "" {
			stats.ByAgent[e.Agent]++
		}
		if e.Tool != "" {
			stats.ByTool[e.Tool]++
		}
	}
	return stats
}

// ActiveModel reports the orchestrator LLM's model.
func (a *SageOrchestratorAgent) ActiveModel() string {
	a.ensureLLM()
	return a.llm.ActiveModel()
}

// LLM returns the inner CopilotAgent used for orchestrator completions, or nil
// before the first call. This lets external code (e.g. model-override logic)
// update the model on both the outer struct and the lazily-initialised LLM.
func (a *SageOrchestratorAgent) LLM() *CopilotAgent {
	return a.llm
}

// LastWorker returns the worker AgentID that handled the most recent call.
func (a *SageOrchestratorAgent) LastWorker() string { return a.lastWorker }

// LastReply returns the text of the most recent successful worker reply, or
// "" if no worker has produced output yet. The manager uses this to surface
// partial work when a later round fails (round-cap, network, LLM error).
func (a *SageOrchestratorAgent) LastReply() string { return a.lastReply }

func (a *SageOrchestratorAgent) ResetLastWorker() {
	if a == nil {
		return
	}
	a.lastWorker = ""
	a.lastReply = ""
}

// LastWorkerToolTrace returns the MCP tool trace of the worker that handled
// the most recent call, or nil if no worker was invoked.
func (a *SageOrchestratorAgent) LastWorkerToolTrace() []string {
	if a.lastWorker == "" {
		return nil
	}
	if w, ok := a.Workers[a.lastWorker]; ok && w != nil {
		return w.LastToolTrace()
	}
	return nil
}

// LastWorkerToolErrors returns per-tool failures from the worker that handled
// the most recent call. Empty when every tool succeeded.
func (a *SageOrchestratorAgent) LastWorkerToolErrors() []ToolCallLog {
	if a.lastWorker == "" {
		return nil
	}
	if w, ok := a.Workers[a.lastWorker]; ok && w != nil {
		return w.LastToolErrors()
	}
	return nil
}

// LastRouterTrace returns the orchestrator's own tool-call trace (which router
// tool it picked — e.g. "call_research_agent").
func (a *SageOrchestratorAgent) LastRouterTrace() []string {
	if a.llm == nil {
		return nil
	}
	return a.llm.LastToolTrace()
}

// SetStateDir wires the orchestrator LLM's Copilot token source.
func (a *SageOrchestratorAgent) SetStateDir(dir string) {
	a.ensureLLM()
	a.llm.SetStateDir(dir)
}

func (a *SageOrchestratorAgent) ensureLLM() {
	if a.llm == nil {
		a.llm = &CopilotAgent{
			BaseAgent: a.BaseAgent,
			Model:     a.Model,
		}
	}
}

// Orchestrate asks the orchestrator LLM to pick a worker via registry routing
// tools and returns the worker's answer. Every agent involved registers on
// the tracker so callers can see the full handoff chain.
func (a *SageOrchestratorAgent) Orchestrate(ctx context.Context, input string, tracker *HandoffTracker) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if tracker != nil {
		tracker.Add(AgentHandoff{AgentID: a.AgentID, Role: "orchestrator"})
	}

	if err := a.RegisterWithACP("L2"); err != nil {
		return "", fmt.Errorf("ACP registration: %w", err)
	}
	if len(a.Workers) == 0 {
		return "", fmt.Errorf("no worker agents configured")
	}

	a.lastWorker = ""
	a.lastReply = ""
	a.ensureLLM()

	messages := []ChatMessage{}
	if a.SystemPrompt != "" {
		messages = append(messages, ChatMessage{Role: "system", Content: a.SystemPrompt})
	}
	messages = append(messages, ChatMessage{Role: "user", Content: input})

	start := time.Now()
	routeTools := a.routeTools()
	if len(routeTools) == 0 {
		return "", fmt.Errorf("no orchestrator route tools configured")
	}
	reply, err := a.llm.ChatWithLocalTools(ctx, messages, routeTools, a.makeRouterExec(ctx, input, tracker))
	EmitLatencySpan(ctx, "orchestrator_route_model", start)
	return reply, err
}

// Resume continues an orchestration that was paused via RoundCapReachedError.
// prevMessages is the conversation captured in the cap error. note is an
// optional human-supplied hint ("yes go", "focus on the senior dev's last
// note", etc.) — empty string means a vanilla "user said continue."
//
// Tracker / lastReply / lastWorker are preserved across the pause so the
// resumed run extends the original task's history.
func (a *SageOrchestratorAgent) Resume(
	ctx context.Context,
	prevMessages []ChatMessage,
	tracker *HandoffTracker,
	originalInput, note string,
) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	a.ensureLLM()

	resumeNote := strings.TrimSpace(note)
	if resumeNote == "" {
		resumeNote = "The human confirmed: continue. Push the orchestration forward with what you've already gathered."
	} else {
		resumeNote = "The human confirmed: continue. " + resumeNote
	}

	msgs := append([]ChatMessage{}, prevMessages...)
	msgs = append(msgs, ChatMessage{Role: "user", Content: resumeNote})

	routeTools := a.routeTools()
	if len(routeTools) == 0 {
		return "", fmt.Errorf("no orchestrator route tools configured")
	}
	return a.llm.ChatWithLocalTools(ctx, msgs, routeTools, a.makeRouterExec(ctx, originalInput, tracker))
}

// makeRouterExec constructs the local-tools exec callback that maps router
// tool names (call_*_agent) to in-process worker dispatches. Pulled out of
// Orchestrate so Resume can reuse the exact same wiring on a continuation.
func (a *SageOrchestratorAgent) makeRouterExec(
	ctx context.Context,
	originalInput string,
	tracker *HandoffTracker,
) func(name string, args map[string]interface{}) (string, error) {
	return func(name string, args map[string]interface{}) (string, error) {
		query, _ := args["query"].(string)
		if query == "" {
			query = originalInput
		}
		workerID, ok := a.routeTargets()[name]
		if !ok {
			return "", fmt.Errorf("unknown router tool: %s", name)
		}
		worker, ok := a.Workers[workerID]
		if !ok || worker == nil {
			return "", fmt.Errorf("worker %s not configured", workerID)
		}
		AppendWorkContextEvent(ctx, "orchestrator_route", a.AgentID, "Orchestrator selected worker", "", map[string]interface{}{
			"tool":   name,
			"worker": workerID,
		})

		if a.requiresSeniorGateForRequest(workerID, query) {
			AppendWorkContextEvent(ctx, "senior_gate", a.AgentID, "Senior gate required", "", map[string]interface{}{"worker": workerID})
			if err := a.runSeniorGate(ctx, query, workerID, tracker); err != nil {
				AppendWorkContextEvent(ctx, "senior_gate", a.AgentID, "Senior gate failed", err.Error(), map[string]interface{}{"worker": workerID})
				return "", err
			}
			AppendWorkContextEvent(ctx, "senior_gate", a.AgentID, "Senior gate approved", "", map[string]interface{}{"worker": workerID})
		} else if a.requiresSeniorGate(workerID) {
			log.Printf("  [%s] senior gate skipped for %s (read-only intent)", a.AgentID, workerID)
			AppendWorkContextEvent(ctx, "senior_gate", a.AgentID, "Senior gate skipped for read-only intent", "", map[string]interface{}{"worker": workerID})
		}

		mcpStart := time.Now()
		if err := worker.ConnectToMCP(); err != nil {
			return "", fmt.Errorf("worker %s MCP connect: %w", workerID, err)
		}
		EmitLatencySpan(ctx, "worker_mcp_health", mcpStart)
		a.lastWorker = workerID
		log.Printf("  [%s] dispatching to %s  query=%q", a.AgentID, workerID, truncate(query, 120))
		AppendWorkContextEvent(ctx, "worker_start", a.AgentID, "Dispatching worker", "", map[string]interface{}{"worker": workerID})
		EmitProgress(ctx, ProgressEvent{Type: "worker", Agent: workerID, Phase: "start", Timestamp: time.Now().Unix()})
		wstart := time.Now()
		result, err := worker.Chat(ctx, workerDispatchMessages(worker, ctx, query))
		EmitLatencySpan(ctx, "worker_model_call", wstart)
		wdur := time.Since(wstart).Round(time.Millisecond)
		if err != nil {
			log.Printf("  [%s] %s ✗ (%s) err=%v", a.AgentID, workerID, wdur, err)
			EmitProgress(ctx, ProgressEvent{Type: "worker", Agent: workerID, Phase: "end", DurationMS: wdur.Milliseconds(), Error: err.Error(), Timestamp: time.Now().Unix()})
			PublishError(ctx, ErrorEvent{Kind: "worker", Agent: workerID, Error: err.Error(), DurationMS: wdur.Milliseconds()})
			AppendWorkContextEvent(ctx, "worker_end", workerID, "Worker failed", err.Error(), map[string]interface{}{"duration_ms": wdur.Milliseconds()})
			return "", err
		}
		log.Printf("  [%s] %s ✓ (%s) tools=%v replyLen=%d", a.AgentID, workerID, wdur, worker.LastToolTrace(), len(result))
		EmitProgress(ctx, ProgressEvent{Type: "worker", Agent: workerID, Phase: "end", DurationMS: wdur.Milliseconds(), Timestamp: time.Now().Unix()})
		AppendWorkContextEvent(ctx, "worker_end", workerID, "Worker completed", result, map[string]interface{}{
			"duration_ms": wdur.Milliseconds(),
			"tool_calls":  worker.LastToolTrace(),
		})
		if result != "" {
			a.lastReply = result
		}
		if tracker != nil {
			tracker.Add(AgentHandoff{
				AgentID:   workerID,
				Role:      "worker",
				Model:     worker.ActiveModel(),
				ToolCalls: worker.LastToolTrace(),
				Reply:     result,
			})
		}
		return result, nil
	}
}

func (a *SageOrchestratorAgent) DispatchWorker(
	ctx context.Context,
	workerID string,
	query string,
	tracker *HandoffTracker,
	opts WorkerDispatchOptions,
) (string, error) {
	if a == nil {
		return "", fmt.Errorf("orchestrator not configured")
	}
	worker, ok := a.Workers[workerID]
	if !ok || worker == nil {
		return "", fmt.Errorf("worker %s not configured", workerID)
	}
	mode := strings.TrimSpace(opts.Mode)
	if mode == "" {
		mode = "direct"
	}
	if opts.SuppressPeerTools {
		ctx = WithSuppressedTools(ctx, "call_agent", "list_agents")
	}

	if opts.EnforceSeniorGate && a.requiresSeniorGateForRequest(workerID, query) {
		AppendWorkContextEvent(ctx, "senior_gate", a.AgentID, "Senior gate required", "", map[string]interface{}{"worker": workerID, "mode": mode})
		if err := a.runSeniorGate(ctx, query, workerID, tracker); err != nil {
			AppendWorkContextEvent(ctx, "senior_gate", a.AgentID, "Senior gate failed", err.Error(), map[string]interface{}{"worker": workerID, "mode": mode})
			return "", err
		}
		AppendWorkContextEvent(ctx, "senior_gate", a.AgentID, "Senior gate approved", "", map[string]interface{}{"worker": workerID, "mode": mode})
	} else if opts.EnforceSeniorGate && a.requiresSeniorGate(workerID) {
		log.Printf("  [%s] senior gate skipped for %s (read-only intent)", a.AgentID, workerID)
		AppendWorkContextEvent(ctx, "senior_gate", a.AgentID, "Senior gate skipped for read-only intent", "", map[string]interface{}{"worker": workerID, "mode": mode})
	}

	mcpStart := time.Now()
	if err := worker.ConnectToMCP(); err != nil {
		return "", fmt.Errorf("worker %s MCP connect: %w", workerID, err)
	}
	EmitLatencySpan(ctx, "worker_mcp_health", mcpStart)
	a.lastWorker = workerID
	log.Printf("  [%s] dispatching to %s mode=%s query=%q", a.AgentID, workerID, mode, truncate(query, 120))
	AppendWorkContextEvent(ctx, "worker_start", a.AgentID, "Dispatching worker", "", map[string]interface{}{
		"worker":              workerID,
		"mode":                mode,
		"route_tool":          opts.RouteTool,
		"peer_tools_disabled": opts.SuppressPeerTools,
	})
	EmitProgress(ctx, ProgressEvent{Type: "worker", Agent: workerID, Phase: "start", Mode: mode, Timestamp: time.Now().Unix()})
	wstart := time.Now()
	result, err := worker.Chat(ctx, workerDispatchMessages(worker, ctx, query))
	EmitLatencySpan(ctx, "worker_model_call", wstart)
	wdur := time.Since(wstart).Round(time.Millisecond)
	if err != nil {
		log.Printf("  [%s] %s failed (%s): %v", a.AgentID, workerID, wdur, err)
		EmitProgress(ctx, ProgressEvent{Type: "worker", Agent: workerID, Phase: "end", Mode: mode, DurationMS: wdur.Milliseconds(), Error: err.Error(), Timestamp: time.Now().Unix()})
		PublishError(ctx, ErrorEvent{Kind: "worker", Agent: workerID, Error: err.Error(), DurationMS: wdur.Milliseconds()})
		AppendWorkContextEvent(ctx, "worker_end", workerID, "Worker failed", err.Error(), map[string]interface{}{"duration_ms": wdur.Milliseconds(), "mode": mode})
		return "", err
	}
	log.Printf("  [%s] %s ok (%s) tools=%v replyLen=%d", a.AgentID, workerID, wdur, worker.LastToolTrace(), len(result))
	EmitProgress(ctx, ProgressEvent{Type: "worker", Agent: workerID, Phase: "end", Mode: mode, DurationMS: wdur.Milliseconds(), Timestamp: time.Now().Unix()})
	AppendWorkContextEvent(ctx, "worker_end", workerID, "Worker completed", result, map[string]interface{}{
		"duration_ms": wdur.Milliseconds(),
		"mode":        mode,
		"tool_calls":  worker.LastToolTrace(),
	})
	if result != "" {
		a.lastReply = result
	}
	if tracker != nil {
		tracker.Add(AgentHandoff{
			AgentID:   workerID,
			Role:      "worker",
			Model:     worker.ActiveModel(),
			ToolCalls: worker.LastToolTrace(),
			Reply:     result,
		})
	}
	return result, nil
}

func workerDispatchMessages(worker *CopilotAgent, ctx context.Context, query string) []ChatMessage {
	workPrompt := ""
	if access, ok := WorkContextFromContext(ctx); ok {
		workPrompt = BuildWorkContextWorkerPrompt(access)
	}
	if strings.TrimSpace(workPrompt) == "" {
		return []ChatMessage{{Role: "user", Content: query}}
	}
	systemPrompt := strings.TrimSpace(workPrompt)
	if worker != nil && strings.TrimSpace(worker.SystemPrompt) != "" {
		systemPrompt = strings.TrimSpace(worker.SystemPrompt) + "\n\n" + systemPrompt
	}
	return []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: query},
	}
}

func (a *SageOrchestratorAgent) routeTools() []toolDef {
	if len(a.RouteTools) > 0 {
		return a.RouteTools
	}
	if a.Registry == nil {
		return nil
	}
	routes, _ := a.Registry.BuildOrchestratorRoutes()
	a.RouteTools = routes.Tools
	a.RouteTargets = routes.Targets
	return a.RouteTools
}

func (a *SageOrchestratorAgent) routeTargets() map[string]string {
	if len(a.RouteTargets) > 0 {
		return a.RouteTargets
	}
	if a.Registry == nil {
		return map[string]string{}
	}
	routes, _ := a.Registry.BuildOrchestratorRoutes()
	a.RouteTools = routes.Tools
	a.RouteTargets = routes.Targets
	return a.RouteTargets
}

func (a *SageOrchestratorAgent) requiresSeniorGate(workerID string) bool {
	mode := "off"
	if a != nil && a.Registry != nil {
		mode = a.Registry.SeniorGateForAgent(workerID)
	}
	return mode == "delivery" || mode == "always"
}

func (a *SageOrchestratorAgent) requiresSeniorGateForRequest(workerID, query string) bool {
	mode := "off"
	if a != nil && a.Registry != nil {
		mode = a.Registry.SeniorGateForAgent(workerID)
	}
	switch mode {
	case "always":
		return true
	case "delivery":
		return isDeliveryWorkRequest(query)
	default:
		return false
	}
}

func isDeliveryWorkRequest(query string) bool {
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return false
	}
	deliveryTerms := []string{
		"add",
		"apply",
		"build",
		"change",
		"commit",
		"configure",
		"create",
		"delete",
		"deploy",
		"edit",
		"fix",
		"generate",
		"implement",
		"install",
		"migrate",
		"push",
		"rebuild",
		"refactor",
		"remove",
		"repair",
		"restart",
		"ship",
		"update",
		"write",
	}
	if containsAnyWord(normalized, deliveryTerms) {
		return true
	}
	if strings.Contains(normalized, "create a doc") ||
		strings.Contains(normalized, "architecture document") ||
		strings.Contains(normalized, "workorder") ||
		strings.Contains(normalized, "implementation plan") ||
		strings.Contains(normalized, "plan next phase") ||
		strings.Contains(normalized, "plan out") {
		return true
	}
	return false
}

func containsAnyWord(s string, terms []string) bool {
	padded := " " + s + " "
	for _, term := range terms {
		if strings.Contains(padded, " "+term+" ") ||
			strings.Contains(padded, " "+term+"ing ") ||
			strings.Contains(padded, " "+term+"ed ") ||
			strings.Contains(padded, " "+term+"s ") {
			return true
		}
	}
	return false
}

func containsAny(s string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(s, term) {
			return true
		}
	}
	return false
}

func (a *SageOrchestratorAgent) runSeniorGate(
	ctx context.Context,
	query string,
	targetWorkerID string,
	tracker *HandoffTracker,
) error {
	senior, ok := a.Workers[SeniorDevAgentID]
	if !ok || senior == nil {
		return fmt.Errorf("senior gate required but %s is not configured", SeniorDevAgentID)
	}
	mcpStart := time.Now()
	if err := senior.ConnectToMCP(); err != nil {
		return fmt.Errorf("senior gate MCP connect failed: %w", err)
	}
	EmitLatencySpan(ctx, "senior_gate_mcp_health", mcpStart)

	gatePrompt := fmt.Sprintf(
		"Review this request before %s executes it. "+
			"Return a response beginning with SENIOR_APPROVED or SENIOR_REJECTED.\n\nRequest:\n%s",
		targetWorkerID,
		query,
	)

	start := time.Now()
	log.Printf("  [%s] senior gate start for %s", a.AgentID, targetWorkerID)
	EmitProgress(ctx, ProgressEvent{Type: "worker", Agent: SeniorDevAgentID, Phase: "start", Timestamp: time.Now().Unix()})
	result, err := senior.Chat(ctx, workerDispatchMessages(senior, ctx, gatePrompt))
	EmitLatencySpan(ctx, "senior_gate_model_call", start)
	dur := time.Since(start).Round(time.Millisecond)
	if err != nil {
		log.Printf("  [%s] senior gate error for %s (%s): %v", a.AgentID, targetWorkerID, dur, err)
		EmitProgress(ctx, ProgressEvent{Type: "worker", Agent: SeniorDevAgentID, Phase: "end", DurationMS: dur.Milliseconds(), Error: err.Error(), Timestamp: time.Now().Unix()})
		return fmt.Errorf("senior gate failed for %s: %w", targetWorkerID, err)
	}

	EmitProgress(ctx, ProgressEvent{Type: "worker", Agent: SeniorDevAgentID, Phase: "end", DurationMS: dur.Milliseconds(), Timestamp: time.Now().Unix()})

	if tracker != nil {
		tracker.Add(AgentHandoff{
			AgentID:   SeniorDevAgentID,
			Role:      "review_gate",
			Model:     senior.ActiveModel(),
			ToolCalls: senior.LastToolTrace(),
			Reply:     result,
		})
	}

	normalized := strings.ToUpper(strings.TrimSpace(result))
	if strings.HasPrefix(normalized, "SENIOR_APPROVED") {
		log.Printf("  [%s] senior gate approved %s (%s)", a.AgentID, targetWorkerID, dur)
		return nil
	}

	log.Printf("  [%s] senior gate rejected %s (%s)", a.AgentID, targetWorkerID, dur)
	return fmt.Errorf("senior gate rejected %s: %s", targetWorkerID, truncate(result, 240))
}
