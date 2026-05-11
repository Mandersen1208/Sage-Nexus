package sageagents

import "testing"

func TestParseProviderModelDefaultsBareModelsToCopilot(t *testing.T) {
	got := ParseProviderModel("gpt-4.1")
	if got.Provider != ProviderCopilot || got.Model != "gpt-4.1" || got.Ref != "copilot/gpt-4.1" {
		t.Fatalf("unexpected parse: %+v", got)
	}
}

func TestParseProviderModelRecognizesCodex(t *testing.T) {
	got := ParseProviderModel(DefaultCodexModelRef)
	if got.Provider != ProviderCodex || got.Model != DefaultCodexModel || got.Ref != DefaultCodexModelRef {
		t.Fatalf("unexpected parse: %+v", got)
	}
}

func TestValidateAgentProviderModelAllowsCodexForAllAgents(t *testing.T) {
	if err := ValidateAgentProviderModel(SageAgentID, DefaultCodexModelRef); err != nil {
		t.Fatalf("Sage should allow Codex model: %v", err)
	}
	if err := ValidateAgentProviderModel("AGT-backend-dev-agent", DefaultCodexModelRef); err != nil {
		t.Fatalf("worker should allow Codex model: %v", err)
	}
}
