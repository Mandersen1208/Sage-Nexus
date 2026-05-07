package sageagents

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// AgentStatus extends AgentMetadata with live health information.
type AgentStatus struct {
	AgentMetadata
	Healthy bool `json:"healthy"`
}

// AgentHandoff records a single node in the agent delegation chain.
// Each time work is handed off from one agent to another, a handoff is appended.
type AgentHandoff struct {
	AgentID   string   `json:"agent_id"`
	Role      string   `json:"role"`                 // "orchestrator" | "worker" | "sub-agent"
	Model     string   `json:"model,omitempty"`      // model used by this agent, if any
	ToolCalls []string `json:"tool_calls,omitempty"` // MCP tools invoked by this agent
	Reply     string   `json:"reply,omitempty"`      // full text output from this agent
	Timestamp int64    `json:"timestamp"`
}

// HandoffTracker collects the agent delegation chain for a single request.
// Each agent in the call path calls Add() when it picks up work. Thread-safe.
type HandoffTracker struct {
	mu       sync.RWMutex
	handoffs []AgentHandoff
}

// NewHandoffTracker creates a fresh tracker for one request.
func NewHandoffTracker() *HandoffTracker { return &HandoffTracker{} }

// Add appends a handoff entry, automatically stamping the current time if Timestamp is zero.
func (t *HandoffTracker) Add(h AgentHandoff) {
	if h.Timestamp == 0 {
		h.Timestamp = time.Now().Unix()
	}
	t.mu.Lock()
	t.handoffs = append(t.handoffs, h)
	t.mu.Unlock()
}

// Handoffs returns a snapshot of the chain in insertion order.
func (t *HandoffTracker) Handoffs() []AgentHandoff {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]AgentHandoff, len(t.handoffs))
	copy(out, t.handoffs)
	return out
}

// AgentIDs returns the unique agent IDs across the chain, in order first seen.
func (t *HandoffTracker) AgentIDs() []string {
	seen := map[string]bool{}
	ids := []string{}
	for _, h := range t.Handoffs() {
		if !seen[h.AgentID] {
			seen[h.AgentID] = true
			ids = append(ids, h.AgentID)
		}
	}
	return ids
}

// AgentResponseLog is a structured record of an orchestration step — designed
// to be persisted to a DB or forwarded to an observability backend.
type AgentResponseLog struct {
	RequestID         string         `json:"request_id"`
	Timestamp         int64          `json:"timestamp"`
	Agents            []string       `json:"agents"`          // all agent IDs involved
	Model             string         `json:"model,omitempty"` // primary model used
	Input             interface{}    `json:"input"`
	Output            interface{}    `json:"output"`
	Error             string         `json:"error,omitempty"`
	ToolCalls         []string       `json:"tool_calls,omitempty"`  // all tool calls across all agents
	ToolErrors        []ToolCallLog  `json:"tool_errors,omitempty"` // only tool calls that failed (empty if all succeeded)
	Handoffs          []AgentHandoff `json:"handoffs,omitempty"`    // ordered delegation chain
	OrchestrationPath []string       `json:"orchestration_path"`
}

// ToolCallLog records a single tool invocation — kept in AgentResponseLog for
// failed calls so /agent-response-logs exposes what actually went wrong.
type ToolCallLog struct {
	Agent      string `json:"agent"`
	Tool       string `json:"tool"`
	DurationMS int64  `json:"duration_ms"`
	Error      string `json:"error"`
}

// Manager is the orchestrator: it tracks registered agents, monitors their
// health via heartbeats, selects the best agent for a task, and logs results.
type Manager struct {
	agents          map[string]*AgentStatus
	agentsMu        sync.RWMutex
	logs            []AgentResponseLog
	logsMu          sync.RWMutex
	heartbeatExpiry time.Duration
}

func NewManager() *Manager {
	return &Manager{
		agents:          make(map[string]*AgentStatus),
		heartbeatExpiry: 45 * time.Minute,
	}
}

// RegisterAgent records an agent in the manager's local registry. It is used
// for in-process agents that do not post HTTP heartbeats to /heartbeat.
func (m *Manager) RegisterAgent(meta AgentMetadata) {
	if meta.LastSeen == 0 {
		meta.LastSeen = time.Now().Unix()
	}
	m.agentsMu.Lock()
	m.agents[meta.AgentID] = &AgentStatus{AgentMetadata: meta, Healthy: true}
	m.agentsMu.Unlock()
}

// RegisterRoutes mounts manager HTTP handlers onto the given mux.
// Note: /health is NOT registered here — the caller owns that route so it
// can include extra fields (e.g. acp_ready status).
func (m *Manager) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/heartbeat", m.heartbeatHandler)
	mux.HandleFunc("/agents/health", m.healthHandler)
	mux.HandleFunc("/agent-response-logs", m.logsHandler)
}

func (m *Manager) heartbeatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var meta AgentMetadata
	if err := json.NewDecoder(r.Body).Decode(&meta); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid payload"}`))
		return
	}
	meta.LastSeen = time.Now().Unix()
	m.agentsMu.Lock()
	m.agents[meta.AgentID] = &AgentStatus{AgentMetadata: meta, Healthy: true}
	m.agentsMu.Unlock()
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func (m *Manager) healthHandler(w http.ResponseWriter, r *http.Request) {
	m.agentsMu.RLock()
	defer m.agentsMu.RUnlock()
	now := time.Now().Unix()
	agents := make([]*AgentStatus, 0, len(m.agents))
	for _, a := range m.agents {
		status := *a
		status.Healthy = now-status.LastSeen < int64(m.heartbeatExpiry.Seconds())
		agents = append(agents, &status)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"manager": "up",
		"agents":  agents,
	})
}

func (m *Manager) logsHandler(w http.ResponseWriter, r *http.Request) {
	m.logsMu.RLock()
	defer m.logsMu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(m.logs)
}

// AddLog records an orchestration result.
func (m *Manager) AddLog(entry AgentResponseLog) {
	m.logsMu.Lock()
	defer m.logsMu.Unlock()
	m.logs = append(m.logs, entry)
}

// SelectAgent picks the healthy agent with the best capability match for the
// given required capabilities. Returns an error if no suitable agent exists.
func (m *Manager) SelectAgent(requiredCaps []string) (string, error) {
	m.agentsMu.RLock()
	defer m.agentsMu.RUnlock()

	best, bestScore := "", -1
	for id, agent := range m.agents {
		if !agent.Healthy {
			continue
		}
		score := 0
		for _, req := range requiredCaps {
			for _, cap := range agent.Capabilities {
				if req == cap {
					score++
				}
			}
		}
		if score > bestScore {
			bestScore = score
			best = id
		}
	}
	if best == "" {
		return "", fmt.Errorf("no healthy agent found for capabilities %v", requiredCaps)
	}
	return best, nil
}
