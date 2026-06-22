package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	sageagents "github.com/matta/sage-nexus/services/manager"
	"github.com/matta/sage-nexus/services/manager/a2a"
)

const agentHandoffChannel = "sage:agent_handoffs"

type agentHandoffRequest struct {
	CallerAgentID    string `json:"caller_agent_id"`
	TaskID           string `json:"task_id"`
	TargetAgentID    string `json:"target_agent_id"`
	Query            string `json:"query"`
	Reason           string `json:"reason"`
	Summary          string `json:"summary"`
	Depth            int    `json:"depth"`
	WorkContextID    string `json:"work_context_id"`
	WorkContextToken string `json:"token"`
}

type agentCompleteRequest struct {
	CallerAgentID    string `json:"caller_agent_id"`
	TaskID           string `json:"task_id"`
	Summary          string `json:"summary"`
	Result           string `json:"result"`
	Depth            int    `json:"depth"`
	WorkContextID    string `json:"work_context_id"`
	WorkContextToken string `json:"token"`
}

type agentHandoffEvent struct {
	TaskID        string `json:"taskId"`
	ContextID     string `json:"contextId"`
	WorkContextID string `json:"workContextId"`
	Token         string `json:"token"`
	FromAgentID   string `json:"fromAgentId"`
	ToAgentID     string `json:"toAgentId"`
	Query         string `json:"query"`
	Reason        string `json:"reason"`
	Summary       string `json:"summary"`
	Depth         int    `json:"depth"`
	CreatedAt     string `json:"createdAt"`
}

type agentOwnedResult struct {
	Content   string
	LastAgent string
	Err       error
}

type agentHandoffRuntime struct {
	rc           *redis.Client
	workers      map[string]*sageagents.CopilotAgent
	workContexts *sageagents.WorkContextStore

	mu    sync.Mutex
	tasks map[string]*agentOwnedTask
}

type agentOwnedTask struct {
	ctx       context.Context
	taskID    string
	contextID string
	access    sageagents.WorkContextAccess
	tracker   *sageagents.HandoffTracker
	rc        *redis.Client

	mu           sync.Mutex
	handoffCount int
	completed    bool
	result       agentOwnedResult
	done         chan agentOwnedResult
}

func newAgentHandoffRuntime(rc *redis.Client, workers map[string]*sageagents.CopilotAgent, workContexts *sageagents.WorkContextStore) *agentHandoffRuntime {
	return &agentHandoffRuntime{
		rc:           rc,
		workers:      workers,
		workContexts: workContexts,
		tasks:        map[string]*agentOwnedTask{},
	}
}

func (r *agentHandoffRuntime) start(ctx context.Context) {
	if r == nil || r.rc == nil {
		return
	}
	go func() {
		ps := r.rc.Subscribe(ctx, agentHandoffChannel)
		defer func() {
			if err := ps.Close(); err != nil {
				log.Printf("[agent-handoff] subscriber close failed: %v", err)
			}
		}()
		log.Printf("[agent-handoff] subscribed to %s", agentHandoffChannel)
		for {
			msg, err := ps.ReceiveMessage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("[agent-handoff] receive failed: %v", err)
				time.Sleep(time.Second)
				continue
			}
			var evt agentHandoffEvent
			if err := json.Unmarshal([]byte(msg.Payload), &evt); err != nil {
				log.Printf("[agent-handoff] invalid event: %v", err)
				continue
			}
			go r.dispatchHandoff(evt)
		}
	}()
}

func registerAgentHandoffRoutes(mux *http.ServeMux, runtime *agentHandoffRuntime) {
	mux.HandleFunc("/agent-handoff", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		if runtime == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "agent handoff runtime unavailable")
			return
		}
		var req agentHandoffRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if err := runtime.acceptHandoff(r.Context(), req); err != nil {
			writeJSONError(w, statusForHandoffError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]interface{}{"accepted": true, "agent": req.TargetAgentID})
	})

	mux.HandleFunc("/agent-complete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		if runtime == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "agent handoff runtime unavailable")
			return
		}
		var req agentCompleteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if err := runtime.acceptCompletion(r.Context(), req); err != nil {
			writeJSONError(w, statusForHandoffError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"completed": true, "agent": req.CallerAgentID})
	})
}

func statusForHandoffError(err error) int {
	switch {
	case errors.Is(err, errHandoffUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, errHandoffForbidden):
		return http.StatusForbidden
	case errors.Is(err, errHandoffNotFound):
		return http.StatusNotFound
	default:
		return http.StatusBadRequest
	}
}

var (
	errHandoffUnauthorized = errors.New("handoff unauthorized")
	errHandoffForbidden    = errors.New("handoff forbidden")
	errHandoffNotFound     = errors.New("handoff not found")
)

func (r *agentHandoffRuntime) runInitial(
	ctx context.Context,
	task inboundTask,
	access sageagents.WorkContextAccess,
	firstAgentID string,
	query string,
	tracker *sageagents.HandoffTracker,
) (agentOwnedResult, error) {
	if r == nil {
		return agentOwnedResult{}, fmt.Errorf("agent handoff runtime unavailable")
	}
	state := &agentOwnedTask{
		ctx:       ctx,
		taskID:    task.TaskID,
		contextID: task.ContextID,
		access:    access,
		tracker:   tracker,
		rc:        r.rc,
		done:      make(chan agentOwnedResult, 1),
	}
	r.mu.Lock()
	r.tasks[task.TaskID] = state
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		if current := r.tasks[task.TaskID]; current == state {
			delete(r.tasks, task.TaskID)
		}
		r.mu.Unlock()
	}()

	r.invokeAgent(state, "manager", firstAgentID, query, "initial route", 0)
	select {
	case result := <-state.done:
		if result.Err != nil {
			return result, result.Err
		}
		return result, nil
	case <-ctx.Done():
		return agentOwnedResult{Err: ctx.Err()}, ctx.Err()
	}
}

func (r *agentHandoffRuntime) acceptHandoff(ctx context.Context, req agentHandoffRequest) error {
	req.CallerAgentID = strings.TrimSpace(req.CallerAgentID)
	req.TargetAgentID = strings.TrimSpace(req.TargetAgentID)
	req.TaskID = strings.TrimSpace(req.TaskID)
	req.WorkContextID = strings.TrimSpace(req.WorkContextID)
	req.WorkContextToken = strings.TrimSpace(req.WorkContextToken)
	req.Query = strings.TrimSpace(req.Query)
	req.Reason = strings.TrimSpace(req.Reason)
	req.Summary = strings.TrimSpace(req.Summary)
	if req.CallerAgentID == "" || req.TargetAgentID == "" || req.TaskID == "" || req.Query == "" || req.Reason == "" || req.Summary == "" {
		return fmt.Errorf("caller_agent_id, task_id, target_agent_id, query, reason, and summary are required")
	}
	if !managerEnvBool("SAGE_AGENT_MESH_ENABLED", true) {
		return fmt.Errorf("%w: agent mesh disabled", errHandoffForbidden)
	}
	if req.Depth >= sageagents.PeerMeshMaxDepthFor(req.CallerAgentID) {
		return fmt.Errorf("%w: max agent handoff depth reached", errHandoffForbidden)
	}
	if !sageagents.IsPeerCallAllowed(req.CallerAgentID, req.TargetAgentID) {
		return fmt.Errorf("%w: peer handoff not allowed", errHandoffForbidden)
	}
	r.mu.Lock()
	state := r.tasks[req.TaskID]
	r.mu.Unlock()
	if state == nil {
		return fmt.Errorf("%w: task %s is not active", errHandoffNotFound, req.TaskID)
	}
	access, err := r.authorizeWorkContext(ctx, req.WorkContextID, req.WorkContextToken)
	if err != nil {
		return err
	}
	if access.ID != state.access.ID || access.TaskID != state.taskID {
		return fmt.Errorf("%w: work context does not match active task", errHandoffUnauthorized)
	}
	if _, ok := r.workers[req.TargetAgentID]; !ok {
		return fmt.Errorf("%w: target agent %s is not configured", errHandoffNotFound, req.TargetAgentID)
	}

	state.recordHandoff()
	hctx := sageagents.WithWorkContext(ctx, r.workContexts, access)
	sageagents.AppendWorkContextEvent(hctx, "handoff_requested", req.CallerAgentID, req.Summary, req.Query, map[string]interface{}{
		"to_agent_id": req.TargetAgentID,
		"reason":      req.Reason,
		"depth":       req.Depth,
	})
	publishA2AEventWithWorkContext(r.rc, a2a.NewWorkingStatus(state.taskID, state.contextID, "handoff: "+req.CallerAgentID+" -> "+req.TargetAgentID, map[string]interface{}{
		"activity":      "handoff_requested",
		"from_agent_id": req.CallerAgentID,
		"to_agent_id":   req.TargetAgentID,
		"reason":        req.Reason,
		"depth":         req.Depth,
	}), access.ID)
	evt := agentHandoffEvent{
		TaskID:        state.taskID,
		ContextID:     state.contextID,
		WorkContextID: access.ID,
		Token:         req.WorkContextToken,
		FromAgentID:   req.CallerAgentID,
		ToAgentID:     req.TargetAgentID,
		Query:         req.Query,
		Reason:        req.Reason,
		Summary:       req.Summary,
		Depth:         req.Depth + 1,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	payload, err := json.Marshal(evt)
	if err != nil {
		state.rollbackHandoff()
		return err
	}
	if err := r.rc.Publish(ctx, agentHandoffChannel, payload).Err(); err != nil {
		state.rollbackHandoff()
		return err
	}
	return nil
}

func (r *agentHandoffRuntime) acceptCompletion(ctx context.Context, req agentCompleteRequest) error {
	req.CallerAgentID = strings.TrimSpace(req.CallerAgentID)
	req.TaskID = strings.TrimSpace(req.TaskID)
	req.WorkContextID = strings.TrimSpace(req.WorkContextID)
	req.WorkContextToken = strings.TrimSpace(req.WorkContextToken)
	req.Summary = strings.TrimSpace(req.Summary)
	req.Result = strings.TrimSpace(req.Result)
	if req.CallerAgentID == "" || req.TaskID == "" || req.Summary == "" || req.Result == "" {
		return fmt.Errorf("caller_agent_id, task_id, summary, and result are required")
	}
	r.mu.Lock()
	state := r.tasks[req.TaskID]
	r.mu.Unlock()
	if state == nil {
		return fmt.Errorf("%w: task %s is not active", errHandoffNotFound, req.TaskID)
	}
	access, err := r.authorizeWorkContext(ctx, req.WorkContextID, req.WorkContextToken)
	if err != nil {
		return err
	}
	if access.ID != state.access.ID || access.TaskID != state.taskID {
		return fmt.Errorf("%w: work context does not match active task", errHandoffUnauthorized)
	}
	hctx := sageagents.WithWorkContext(ctx, r.workContexts, access)
	sageagents.AppendWorkContextEvent(hctx, "task_completed", req.CallerAgentID, req.Summary, req.Result, map[string]interface{}{
		"depth": req.Depth,
	})
	publishA2AEventWithWorkContext(r.rc, a2a.NewWorkingStatus(state.taskID, state.contextID, "agent marked task complete: "+req.CallerAgentID, map[string]interface{}{
		"activity": "task_completed",
		"agent":    req.CallerAgentID,
		"depth":    req.Depth,
	}), access.ID)
	state.complete(agentOwnedResult{Content: req.Result, LastAgent: req.CallerAgentID})
	return nil
}

func (r *agentHandoffRuntime) authorizeWorkContext(ctx context.Context, id, token string) (sageagents.WorkContextAccess, error) {
	if r.workContexts == nil {
		return sageagents.WorkContextAccess{}, fmt.Errorf("%w: work context store unavailable", errHandoffUnauthorized)
	}
	access, err := r.workContexts.AccessForToken(ctx, id, token)
	if err != nil {
		return sageagents.WorkContextAccess{}, fmt.Errorf("%w: %v", errHandoffUnauthorized, err)
	}
	return access, nil
}

func (r *agentHandoffRuntime) dispatchHandoff(evt agentHandoffEvent) {
	r.mu.Lock()
	state := r.tasks[evt.TaskID]
	r.mu.Unlock()
	if state == nil {
		log.Printf("[agent-handoff] task %s no longer active; dropping handoff to %s", evt.TaskID, evt.ToAgentID)
		return
	}
	if state.ctx.Err() != nil {
		state.complete(agentOwnedResult{Err: state.ctx.Err()})
		return
	}
	r.invokeAgent(state, evt.FromAgentID, evt.ToAgentID, evt.Query, evt.Reason, evt.Depth)
}

func (r *agentHandoffRuntime) invokeAgent(state *agentOwnedTask, fromAgentID, agentID, query, reason string, depth int) {
	worker, ok := r.workers[agentID]
	if !ok || worker == nil {
		state.complete(agentOwnedResult{LastAgent: agentID, Err: fmt.Errorf("worker %s not configured", agentID)})
		return
	}
	before := state.handoffCountSnapshot()
	ctx := state.ctx
	ctx = sageagents.WithPeerCallDepth(ctx, depth)
	ctx = sageagents.WithWorkContext(ctx, r.workContexts, state.access)
	sageagents.AppendWorkContextEvent(ctx, "agent_started", agentID, "Agent started task slice", reason, map[string]interface{}{
		"from_agent_id": fromAgentID,
		"depth":         depth,
	})
	publishA2AEventWithWorkContext(r.rc, a2a.NewWorkingStatus(state.taskID, state.contextID, "agent working: "+agentID, map[string]interface{}{
		"activity":      "agent_started",
		"agent":         agentID,
		"from_agent_id": fromAgentID,
		"depth":         depth,
	}), state.access.ID)

	if state.tracker != nil {
		state.tracker.Add(sageagents.AgentHandoff{AgentID: agentID, Role: "worker", Model: worker.ActiveModel()})
	}
	if err := worker.ConnectToMCP(); err != nil {
		state.complete(agentOwnedResult{LastAgent: agentID, Err: fmt.Errorf("worker %s MCP connect: %w", agentID, err)})
		return
	}
	start := time.Now()
	result, err := worker.Chat(ctx, agentOwnedDispatchMessages(worker, fromAgentID, reason, query, state.access))
	duration := time.Since(start).Round(time.Millisecond)
	if err != nil {
		sageagents.AppendWorkContextEvent(ctx, "agent_failed", agentID, "Agent failed task slice", err.Error(), map[string]interface{}{
			"duration_ms": duration.Milliseconds(),
			"depth":       depth,
		})
		publishA2AEventWithWorkContext(r.rc, a2a.NewWorkingStatus(state.taskID, state.contextID, "agent failed: "+agentID, map[string]interface{}{
			"activity":    "agent_failed",
			"agent":       agentID,
			"duration_ms": duration.Milliseconds(),
			"depth":       depth,
		}), state.access.ID)
		state.complete(agentOwnedResult{Content: result, LastAgent: agentID, Err: err})
		return
	}
	sageagents.AppendWorkContextEvent(ctx, "agent_completed", agentID, "Agent completed task slice", result, map[string]interface{}{
		"duration_ms": duration.Milliseconds(),
		"depth":       depth,
		"tool_calls":  worker.LastToolTrace(),
	})
	publishA2AEventWithWorkContext(r.rc, a2a.NewWorkingStatus(state.taskID, state.contextID, "agent completed: "+agentID, map[string]interface{}{
		"activity":    "agent_completed",
		"agent":       agentID,
		"duration_ms": duration.Milliseconds(),
		"depth":       depth,
		"tool_calls":  worker.LastToolTrace(),
	}), state.access.ID)
	if state.tracker != nil {
		state.tracker.Add(sageagents.AgentHandoff{
			AgentID:   agentID,
			Role:      "worker_result",
			Model:     worker.ActiveModel(),
			ToolCalls: worker.LastToolTrace(),
			Reply:     result,
		})
	}
	if state.handoffCountSnapshot() == before {
		state.complete(agentOwnedResult{Content: result, LastAgent: agentID})
	}
}

func agentOwnedDispatchMessages(worker *sageagents.CopilotAgent, fromAgentID, reason, query string, access sageagents.WorkContextAccess) []sageagents.ChatMessage {
	prompt := "You own the current slice of an agent-owned Sage Nexus task.\n" +
		"Read Agent Work Context when prior findings are relevant. Append concise context notes for decisions, findings, blockers, tool results, and final output.\n" +
		"If another domain owner is needed, append what you learned, call handoff_to_agent with the next agent, and stop. Do not continue pretending to own out-of-lane work.\n" +
		"If no further agent is needed, call complete_task with the final result or return the final result directly.\n" +
		"Do not reveal work-context tokens to the user."
	if strings.TrimSpace(fromAgentID) != "" {
		prompt += "\nPrevious owner: " + strings.TrimSpace(fromAgentID) + "."
	}
	if strings.TrimSpace(reason) != "" {
		prompt += "\nReason for this slice: " + strings.TrimSpace(reason)
	}
	if access.ID != "" && access.Token != "" {
		prompt += "\n\n" + sageagents.BuildWorkContextWorkerPrompt(access)
	}
	if worker != nil && strings.TrimSpace(worker.SystemPrompt) != "" {
		prompt = strings.TrimSpace(worker.SystemPrompt) + "\n\n" + prompt
	}
	return []sageagents.ChatMessage{
		{Role: "system", Content: prompt},
		{Role: "user", Content: query},
	}
}

func (t *agentOwnedTask) recordHandoff() {
	t.mu.Lock()
	t.handoffCount++
	t.mu.Unlock()
}

func (t *agentOwnedTask) rollbackHandoff() {
	t.mu.Lock()
	if t.handoffCount > 0 {
		t.handoffCount--
	}
	t.mu.Unlock()
}

func (t *agentOwnedTask) handoffCountSnapshot() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.handoffCount
}

func (t *agentOwnedTask) complete(result agentOwnedResult) {
	t.mu.Lock()
	if t.completed {
		t.mu.Unlock()
		return
	}
	t.completed = true
	t.result = result
	t.mu.Unlock()
	select {
	case t.done <- result:
	default:
	}
}
