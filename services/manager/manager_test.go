package sageagents

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestRegisterAgentAddsHealthyAgent(t *testing.T) {
	t.Parallel()

	mgr := NewManager()
	mgr.RegisterAgent(AgentMetadata{
		AgentID:      "AGT-runtime-librarian-agent",
		Endpoint:     "in-process",
		Capabilities: []string{"runtime_inventory_scan"},
	})

	req := httptest.NewRequest("GET", "/agents/health", nil)
	rec := httptest.NewRecorder()
	mgr.healthHandler(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Agents []AgentStatus `json:"agents"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if len(body.Agents) != 1 {
		t.Fatalf("expected one agent, got %d", len(body.Agents))
	}
	agent := body.Agents[0]
	if agent.AgentID != "AGT-runtime-librarian-agent" {
		t.Fatalf("expected runtime librarian, got %q", agent.AgentID)
	}
	if !agent.Healthy {
		t.Fatalf("expected agent to be healthy")
	}
	if len(agent.Capabilities) != 1 || agent.Capabilities[0] != "runtime_inventory_scan" {
		t.Fatalf("unexpected capabilities: %v", agent.Capabilities)
	}
}
