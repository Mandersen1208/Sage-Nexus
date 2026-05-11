package sageagents

import "strings"

const (
	ProviderCopilot       = "copilot"
	ProviderCodex         = "codex"
	DefaultCodexModel     = "gpt-5.5"
	DefaultCodexModelRef  = ProviderCodex + "/" + DefaultCodexModel
	defaultProviderPrefix = ProviderCopilot
)

type ProviderModel struct {
	Provider string
	Model    string
	Ref      string
}

func ParseProviderModel(raw string) ProviderModel {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = copilotDefaultModel
	}
	provider := defaultProviderPrefix
	model := value
	if before, after, ok := strings.Cut(value, "/"); ok {
		candidate := strings.ToLower(strings.TrimSpace(before))
		switch candidate {
		case ProviderCodex, ProviderCopilot:
			provider = candidate
			model = strings.TrimSpace(after)
		}
	}
	if model == "" {
		model = copilotDefaultModel
	}
	return ProviderModel{
		Provider: provider,
		Model:    model,
		Ref:      provider + "/" + model,
	}
}

func IsCodexModelRef(raw string) bool {
	return ParseProviderModel(raw).Provider == ProviderCodex
}

func ValidateAgentProviderModel(agentID, model string) error {
	return nil
}
