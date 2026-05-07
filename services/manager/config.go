package sageagents

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AgentConfig holds the per-agent configuration loaded from the active agent registry.
type AgentConfig struct {
	// ID is optional in JSON because agents are usually keyed by ID in the
	// top-level agents map. It is filled during load so downstream code can
	// treat the registry as a list when needed.
	ID string `json:"id,omitempty"`

	// Enabled controls whether the agent is active. Omitted means enabled.
	Enabled *bool `json:"enabled,omitempty"`

	// Model is the model identifier to use for this agent's completions.
	// e.g. "gpt-4o", "claude-sonnet-4-5", "o3-mini"
	Model string `json:"model,omitempty"`

	// SystemPromptFile is a path (relative to the config file's directory)
	// to a markdown file whose contents become this agent's system prompt.
	SystemPromptFile string `json:"systemPromptFile,omitempty"`

	// Routable agents are exposed to the orchestrator as local route tools.
	Routable bool `json:"routable,omitempty"`

	// Targetable agents are shown in the chat-mode catalog. Nil means the
	// catalog derives a conservative default from routable/worker status.
	Targetable *bool `json:"targetable,omitempty"`

	// DisplayName is operator-facing UI text for catalogs and chat mode labels.
	DisplayName string `json:"displayName,omitempty"`

	// SupportedChatModes controls which chat entry modes are available for this agent.
	SupportedChatModes []string `json:"supportedChatModes,omitempty"`

	// ModeLabels and ModeDescriptions let the registry provide all user-facing
	// chat mode copy instead of hardcoding it in the UI.
	ModeLabels       map[string]string `json:"modeLabels,omitempty"`
	ModeDescriptions map[string]string `json:"modeDescriptions,omitempty"`

	// RouteToolName is the exact orchestrator local tool name, e.g.
	// call_backend_dev_agent.
	RouteToolName string `json:"routeToolName,omitempty"`

	// RouteDescription tells the orchestrator when to choose this route.
	RouteDescription string `json:"routeDescription,omitempty"`

	// ToolBundles reference top-level toolBundles entries.
	ToolBundles []string `json:"toolBundles,omitempty"`

	// Tools are explicit MCP tool names in addition to bundles.
	Tools []string `json:"tools,omitempty"`

	// PeerTargets lists agent IDs this agent may consult through the bounded mesh.
	PeerTargets []string `json:"peerTargets,omitempty"`

	// MaxPeerDepth overrides the registry/default max depth for this caller.
	MaxPeerDepth int `json:"maxPeerDepth,omitempty"`

	// SeniorGate controls quality-gate behavior: off, delivery, or always.
	SeniorGate string `json:"seniorGate,omitempty"`

	// Authority and MustNotOwn are operator-facing role metadata surfaced by
	// inventory and prompts; code enforces route/tools/peer/gate policy.
	Authority  string   `json:"authority,omitempty"`
	MustNotOwn []string `json:"mustNotOwn,omitempty"`

	sourceDir string
}

// AgentsConfig is the top-level structure of the active agent registry.
type AgentsConfig struct {
	Version     int                    `json:"version,omitempty"`
	ToolBundles map[string][]string    `json:"toolBundles,omitempty"`
	Agents      map[string]AgentConfig `json:"agents"`

	// configDir is the directory the file was loaded from, used to resolve
	// relative paths like systemPromptFile.
	configDir string

	// SourceFiles records the files merged into the active registry.
	SourceFiles []string `json:"-"`

	// Warnings records non-fatal registry load/validation issues.
	Warnings []string `json:"-"`
}

// LoadAgentsConfig reads and parses the agents.json config file at the given path.
func LoadAgentsConfig(path string) (*AgentsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read agents config %s: %w", path, err)
	}
	var cfg AgentsConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid agents config %s: %w", path, err)
	}
	cfg.configDir = filepath.Dir(path)
	if cfg.Agents == nil {
		cfg.Agents = make(map[string]AgentConfig)
	}
	for id, agent := range cfg.Agents {
		if agent.ID == "" {
			agent.ID = id
		}
		agent.sourceDir = cfg.configDir
		cfg.Agents[id] = agent
	}
	if cfg.ToolBundles == nil {
		cfg.ToolBundles = make(map[string][]string)
	}
	cfg.SourceFiles = []string{path}
	return &cfg, nil
}

// Get returns the config for the given agent ID.
// Returns a zero-value AgentConfig (no model, no prompt) if not found -
// callers should apply their own defaults.
func (c *AgentsConfig) Get(agentID string) AgentConfig {
	if c == nil || c.Agents == nil {
		return AgentConfig{}
	}
	return c.Agents[agentID]
}

// ModelFor returns the configured model for the given agent ID.
// Returns an empty string if not configured (callers apply their own defaults).
func (c *AgentsConfig) ModelFor(agentID string) string {
	return c.Get(agentID).Model
}

// SystemPrompt loads and returns the contents of the agent's system prompt file.
// Returns an empty string (no error) if no file is configured.
func (c *AgentsConfig) SystemPrompt(agentID string) (string, error) {
	cfg := c.Get(agentID)
	if cfg.SystemPromptFile == "" {
		return "", nil
	}

	promptPath := cfg.SystemPromptFile
	if !filepath.IsAbs(promptPath) {
		baseDir := cfg.sourceDir
		if baseDir == "" {
			baseDir = c.configDir
		}
		promptPath = filepath.Join(baseDir, promptPath)
	}

	data, err := os.ReadFile(promptPath)
	if err != nil {
		return "", fmt.Errorf("could not read system prompt for %s (%s): %w", agentID, promptPath, err)
	}
	return string(data), nil
}
