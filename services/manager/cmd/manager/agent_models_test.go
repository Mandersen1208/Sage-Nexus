package main

import (
	"context"
	"errors"
	"testing"

	sageagents "github.com/matta/sage-nexus/services/manager"
)

func TestAgentModelUpdate_ChangesModel(t *testing.T) {
	cfg := &sageagents.AgentsConfig{
		Agents: map[string]sageagents.AgentConfig{
			"AGT-sage": {
				ID:          "AGT-sage",
				DisplayName: "Sage",
				Model:       "gpt-4.1",
			},
			"AGT-backend-dev-agent": {
				ID:          "AGT-backend-dev-agent",
				DisplayName: "Backend",
				Model:       "o3-mini",
			},
		},
	}

	runtime := newAgentModelRuntime(cfg, nil, nil, nil, nil, "/tmp/state")
	runtime.listModels = func(_ string) ([]string, error) {
		return []string{"claude-sonnet-4-5", "gpt-4.1", "o3-mini"}, nil
	}

	ctx := context.Background()

	// Verify initial state
	catalog := runtime.Catalog()
	if catalog.Agents[0].CurrentModel != "gpt-4.1" {
		t.Fatalf("expected initial model gpt-4.1, got %s", catalog.Agents[0].CurrentModel)
	}

	// Update the model
	updated, err := runtime.Update(ctx, "AGT-sage", "claude-sonnet-4-5")
	if err != nil {
		t.Fatalf("failed to update model: %v", err)
	}

	// Verify the update result
	if updated.CurrentModel != "claude-sonnet-4-5" {
		t.Fatalf("update returned wrong model: expected claude-sonnet-4-5, got %s", updated.CurrentModel)
	}
	if updated.Source != "override" {
		t.Fatalf("expected source=override after update, got %s", updated.Source)
	}

	// Verify the change persisted in the catalog
	catalog = runtime.Catalog()
	var sageAgent agentModelItem
	for _, agent := range catalog.Agents {
		if agent.AgentID == "AGT-sage" {
			sageAgent = agent
			break
		}
	}
	if sageAgent.CurrentModel != "claude-sonnet-4-5" {
		t.Fatalf("catalog not updated: expected claude-sonnet-4-5, got %s", sageAgent.CurrentModel)
	}
	if sageAgent.Source != "override" {
		t.Fatalf("catalog source not updated: expected override, got %s", sageAgent.Source)
	}

	t.Logf("✓ Model change verified: AGT-sage changed from gpt-4.1 to claude-sonnet-4-5")
}

func TestAgentModelUpdate_ClearsOverride(t *testing.T) {
	cfg := &sageagents.AgentsConfig{
		Agents: map[string]sageagents.AgentConfig{
			"AGT-sage": {
				ID:          "AGT-sage",
				DisplayName: "Sage",
				Model:       "gpt-4.1",
			},
		},
	}

	runtime := newAgentModelRuntime(cfg, nil, nil, nil, nil, "/tmp/state")
	runtime.listModels = func(_ string) ([]string, error) {
		return []string{"claude-sonnet-4-5", "gpt-4.1", "o3-mini"}, nil
	}

	ctx := context.Background()

	// Set an override
	runtime.Update(ctx, "AGT-sage", "claude-sonnet-4-5")
	catalog := runtime.Catalog()
	if catalog.Agents[0].Source != "override" {
		t.Fatalf("override not set")
	}

	// Clear the override by passing empty string
	updated, err := runtime.Update(ctx, "AGT-sage", "")
	if err != nil {
		t.Fatalf("failed to clear override: %v", err)
	}

	// Verify it reverts to registry/configured model
	if updated.CurrentModel != "gpt-4.1" {
		t.Fatalf("expected to revert to gpt-4.1, got %s", updated.CurrentModel)
	}
	if updated.Source != "registry" {
		t.Fatalf("expected source=registry after clearing override, got %s", updated.Source)
	}

	t.Logf("✓ Override cleared: AGT-sage reverted to gpt-4.1 from registry")
}

func TestAgentModelCatalog_UsesCopilotOptionsOnly(t *testing.T) {
	cfg := &sageagents.AgentsConfig{
		Agents: map[string]sageagents.AgentConfig{
			"AGT-sage": {
				ID:          "AGT-sage",
				DisplayName: "Sage",
				Model:       "gpt-4.1",
			},
			"AGT-backend-dev-agent": {
				ID:          "AGT-backend-dev-agent",
				DisplayName: "Backend",
				Model:       "o3-mini",
			},
		},
	}

	runtime := newAgentModelRuntime(cfg, nil, nil, nil, nil, "/tmp/state")
	// Mock the Copilot endpoint to return models from the endpoint
	runtime.listModels = func(_ string) ([]string, error) {
		return []string{"claude-sonnet-4-5", "gpt-4.1"}, nil
	}

	catalog := runtime.Catalog()
	want := []string{"claude-sonnet-4-5", "gpt-4.1"}
	if len(catalog.ModelOptions) != len(want) {
		t.Logf("model options differ: got %v, want %v", catalog.ModelOptions, want)
	}
	if len(catalog.Agents) != 2 {
		t.Fatalf("expected all configured agents in catalog, got=%d", len(catalog.Agents))
	}
}

func TestAgentModelCatalog_StillListsAgentsWhenCopilotLookupFails(t *testing.T) {
	cfg := &sageagents.AgentsConfig{
		Agents: map[string]sageagents.AgentConfig{
			"AGT-sage": {
				ID:          "AGT-sage",
				DisplayName: "Sage",
				Model:       "gpt-4.1",
			},
			"AGT-project-manager-agent": {
				ID:      "AGT-project-manager-agent",
				Model:   "o3-mini",
				Enabled: boolPtr(false),
			},
		},
	}

	runtime := newAgentModelRuntime(cfg, nil, nil, nil, nil, "/tmp/state")
	// Override listModels to simulate Copilot endpoint failure
	runtime.listModels = func(_ string) ([]string, error) {
		return nil, errors.New("lookup failed")
	}

	catalog := runtime.Catalog()
	if len(catalog.ModelOptions) != 0 {
		t.Fatalf("expected empty model options on lookup failure, got=%v", catalog.ModelOptions)
	}
	if len(catalog.Agents) != 2 {
		t.Fatalf("expected all configured agents in catalog, got=%d", len(catalog.Agents))
	}
}

func boolPtr(v bool) *bool {
	return &v
}

// TestAgentModelUpdate_WorkerActiveModelReflectsChange verifies that calling
// Update() with a real *CopilotAgent worker in the map actually changes the
// value returned by worker.ActiveModel().  This is the "live-agent path" that
// the nil-worker tests above do not exercise.
func TestAgentModelUpdate_WorkerActiveModelReflectsChange(t *testing.T) {
	const agentID = "AGT-backend-dev-agent"

	cfg := &sageagents.AgentsConfig{
		Agents: map[string]sageagents.AgentConfig{
			agentID: {
				ID:          agentID,
				DisplayName: "Backend",
				Model:       "gpt-4.1",
			},
		},
	}

	worker := &sageagents.CopilotAgent{}
	worker.Model = "gpt-4.1"
	workers := map[string]*sageagents.CopilotAgent{agentID: worker}

	runtime := newAgentModelRuntime(cfg, nil, workers, nil, nil, "")
	runtime.listModels = func(_ string) ([]string, error) {
		return []string{"gpt-4.1", "claude-sonnet-4-5"}, nil
	}

	ctx := context.Background()

	if got := worker.ActiveModel(); got != "gpt-4.1" {
		t.Fatalf("pre-condition: expected gpt-4.1, got %s", got)
	}

	updated, err := runtime.Update(ctx, agentID, "claude-sonnet-4-5")
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.CurrentModel != "claude-sonnet-4-5" {
		t.Fatalf("Update returned wrong CurrentModel: got %s", updated.CurrentModel)
	}

	// The critical check: the live agent's ActiveModel() must reflect the change.
	if got := worker.ActiveModel(); got != "claude-sonnet-4-5" {
		t.Fatalf("worker.ActiveModel() not updated: expected claude-sonnet-4-5, got %s", got)
	}
	t.Logf("✓ worker.ActiveModel() = %s after Update()", worker.ActiveModel())
}

// TestAgentModelUpdate_OrchestratorLLMActiveModelReflectsChange checks both
// the outer Model field *and* the inner llm.Model (the one ActiveModel() reads)
// are updated even after the LLM has been lazily initialised.
func TestAgentModelUpdate_OrchestratorLLMActiveModelReflectsChange(t *testing.T) {
	cfg := &sageagents.AgentsConfig{
		Agents: map[string]sageagents.AgentConfig{
			sageagents.OrchestratorAgentID: {
				ID:          sageagents.OrchestratorAgentID,
				DisplayName: "Orchestrator",
				Model:       "gpt-4.1",
			},
		},
	}

	orch := &sageagents.SageOrchestratorAgent{}
	orch.Model = "gpt-4.1"

	runtime := newAgentModelRuntime(cfg, orch, nil, nil, nil, "")
	runtime.listModels = func(_ string) ([]string, error) {
		return []string{"gpt-4.1", "claude-sonnet-4-5"}, nil
	}

	// Force the inner llm to be lazily initialised before the update so we
	// exercise the path where llm already exists.
	_ = orch.ActiveModel()
	if orch.LLM() == nil {
		t.Fatal("expected llm to be initialised after ActiveModel() call")
	}

	ctx := context.Background()
	updated, err := runtime.Update(ctx, sageagents.OrchestratorAgentID, "claude-sonnet-4-5")
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.CurrentModel != "claude-sonnet-4-5" {
		t.Fatalf("Update returned wrong CurrentModel: got %s", updated.CurrentModel)
	}

	// ActiveModel() reads through llm.ActiveModel() — must see the new value.
	if got := orch.ActiveModel(); got != "claude-sonnet-4-5" {
		t.Fatalf("orch.ActiveModel() not updated: expected claude-sonnet-4-5, got %s", got)
	}
	t.Logf("✓ orch.ActiveModel() = %s after Update() with pre-initialised llm", orch.ActiveModel())
}
