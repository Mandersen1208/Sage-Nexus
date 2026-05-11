package sageagents

import (
	"fmt"
	"sort"
	"strings"
)

const (
	ChatModeAuto   = "auto"
	ChatModeSolo   = "solo"
	ChatModeLaunch = "launch"
)

var knownChatModes = []string{ChatModeAuto, ChatModeSolo, ChatModeLaunch}

type ChatCatalog struct {
	DefaultMode ChatModeSelection  `json:"defaultMode"`
	Agents      []ChatCatalogAgent `json:"agents"`
}

type ChatModeSelection struct {
	AgentMode     string `json:"agentMode"`
	TargetAgentID string `json:"targetAgentId,omitempty"`
	Label         string `json:"label"`
	Description   string `json:"description,omitempty"`
}

type ChatCatalogAgent struct {
	ID          string            `json:"id"`
	DisplayName string            `json:"displayName"`
	Description string            `json:"description,omitempty"`
	Authority   string            `json:"authority,omitempty"`
	Targetable  bool              `json:"targetable"`
	Modes       []ChatCatalogMode `json:"modes"`
}

type ChatCatalogMode struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	Description    string `json:"description,omitempty"`
	Enabled        bool   `json:"enabled"`
	DisabledReason string `json:"disabledReason,omitempty"`
}

func normalizeChatMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "", ChatModeAuto:
		return ChatModeAuto
	case ChatModeSolo:
		return ChatModeSolo
	case ChatModeLaunch:
		return ChatModeLaunch
	default:
		return mode
	}
}

func isKnownChatMode(mode string) bool {
	switch normalizeChatMode(mode) {
	case ChatModeAuto, ChatModeSolo, ChatModeLaunch:
		return true
	default:
		return false
	}
}

func normalizeChatModes(modes []string) []string {
	if len(modes) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(modes))
	for _, mode := range modes {
		mode = normalizeChatMode(mode)
		if mode == "" {
			continue
		}
		if _, ok := seen[mode]; ok {
			continue
		}
		seen[mode] = struct{}{}
		out = append(out, mode)
	}
	return out
}

func (c *AgentsConfig) BuildChatCatalog() ChatCatalog {
	defaultMode := ChatModeSelection{
		AgentMode:     ChatModeAuto,
		TargetAgentID: "",
		Label:         "Sage Auto",
		Description:   "Sage Auto sends the turn through the manager/orchestrator.",
	}
	if c != nil {
		if selection, err := c.ResolveChatSelection(ChatModeAuto, ""); err == nil {
			defaultMode = selection
		}
	}

	catalog := ChatCatalog{DefaultMode: defaultMode}
	if c == nil {
		return catalog
	}

	for _, id := range c.ActiveAgentIDs() {
		agent := c.Get(id)
		if agent.ID == "" {
			agent.ID = id
		}
		if !agent.TargetableValue() {
			continue
		}
		entry := ChatCatalogAgent{
			ID:          agent.ID,
			DisplayName: agent.DisplayNameValue(),
			Description: strings.TrimSpace(agent.RouteDescription),
			Authority:   strings.TrimSpace(agent.Authority),
			Targetable:  true,
			Modes:       c.chatCatalogModes(agent),
		}
		catalog.Agents = append(catalog.Agents, entry)
	}

	sort.Slice(catalog.Agents, func(i, j int) bool {
		if catalog.Agents[i].ID == SageAgentID {
			return true
		}
		if catalog.Agents[j].ID == SageAgentID {
			return false
		}
		return strings.ToLower(catalog.Agents[i].DisplayName) < strings.ToLower(catalog.Agents[j].DisplayName)
	})
	return catalog
}

func (c *AgentsConfig) ResolveChatSelection(mode, targetAgentID string) (ChatModeSelection, error) {
	mode = normalizeChatMode(mode)
	if !isKnownChatMode(mode) {
		return ChatModeSelection{}, fmt.Errorf("unsupported chat mode %q", mode)
	}
	if mode == ChatModeAuto {
		agent := c.Get(SageAgentID)
		if agent.ID == "" {
			agent.ID = SageAgentID
		}
		return ChatModeSelection{
			AgentMode:     ChatModeAuto,
			TargetAgentID: "",
			Label:         agent.modeLabel(ChatModeAuto),
			Description:   agent.modeDescription(ChatModeAuto),
		}, nil
	}

	targetAgentID = strings.TrimSpace(targetAgentID)
	if targetAgentID == "" {
		return ChatModeSelection{}, fmt.Errorf("targetAgentId required for %s mode", mode)
	}
	agent := c.Get(targetAgentID)
	if agent.ID == "" {
		return ChatModeSelection{}, fmt.Errorf("unknown target agent %q", targetAgentID)
	}
	if !agent.EnabledValue() {
		return ChatModeSelection{}, fmt.Errorf("target agent %s is disabled", targetAgentID)
	}
	if !agent.TargetableValue() {
		return ChatModeSelection{}, fmt.Errorf("target agent %s is not chat-targetable", targetAgentID)
	}
	if !stringSliceContains(agent.SupportedChatModesValue(), mode) {
		return ChatModeSelection{}, fmt.Errorf("target agent %s does not support %s mode", targetAgentID, mode)
	}
	return ChatModeSelection{
		AgentMode:     mode,
		TargetAgentID: targetAgentID,
		Label:         agent.modeLabel(mode),
		Description:   agent.modeDescription(mode),
	}, nil
}

func (c *AgentsConfig) chatCatalogModes(agent AgentConfig) []ChatCatalogMode {
	enabled := map[string]struct{}{}
	for _, mode := range agent.SupportedChatModesValue() {
		enabled[mode] = struct{}{}
	}
	modes := make([]ChatCatalogMode, 0, len(knownChatModes))
	for _, mode := range knownChatModes {
		_, ok := enabled[mode]
		item := ChatCatalogMode{
			ID:          mode,
			Label:       agent.modeLabel(mode),
			Description: agent.modeDescription(mode),
			Enabled:     ok,
		}
		if !ok {
			item.DisabledReason = agent.disabledChatModeReason(mode)
		}
		modes = append(modes, item)
	}
	return modes
}

func (a AgentConfig) SupportedChatModesValue() []string {
	if len(a.SupportedChatModes) > 0 {
		return normalizeChatModes(a.SupportedChatModes)
	}
	if !a.TargetableValue() {
		return nil
	}
	if a.ID == SageAgentID {
		return []string{ChatModeAuto, ChatModeSolo}
	}
	if a.IsWorker() {
		if a.Routable {
			return []string{ChatModeSolo, ChatModeLaunch}
		}
		return []string{ChatModeSolo}
	}
	return nil
}

func (a AgentConfig) DisplayNameValue() string {
	if name := strings.TrimSpace(a.DisplayName); name != "" {
		return name
	}
	id := strings.TrimPrefix(strings.TrimSpace(a.ID), "AGT-")
	id = strings.TrimSuffix(id, "-agent")
	id = strings.ReplaceAll(id, "-", " ")
	if id == "" {
		return strings.TrimSpace(a.ID)
	}
	parts := strings.Fields(id)
	for i, part := range parts {
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func (a AgentConfig) modeLabel(mode string) string {
	mode = normalizeChatMode(mode)
	if a.ModeLabels != nil {
		if label := strings.TrimSpace(a.ModeLabels[mode]); label != "" {
			return label
		}
	}
	display := a.DisplayNameValue()
	switch mode {
	case ChatModeAuto:
		return display + " Auto"
	case ChatModeSolo:
		return display + " Only"
	case ChatModeLaunch:
		return display + " Flow"
	default:
		return display + " " + mode
	}
}

func (a AgentConfig) modeDescription(mode string) string {
	mode = normalizeChatMode(mode)
	if a.ModeDescriptions != nil {
		if desc := strings.TrimSpace(a.ModeDescriptions[mode]); desc != "" {
			return desc
		}
	}
	switch mode {
	case ChatModeAuto:
		return "Sage Auto sends the turn through the manager/orchestrator."
	case ChatModeSolo:
		return "Direct chat with this agent as a persona; worker routing stays off."
	case ChatModeLaunch:
		return "Start a bounded flow owned by this agent."
	default:
		return ""
	}
}

func (a AgentConfig) disabledChatModeReason(mode string) string {
	mode = normalizeChatMode(mode)
	if a.ID == SageAgentID && mode == ChatModeLaunch {
		return "Sage is a persona layer; launch flows belong to the manager/orchestrator."
	}
	if mode == ChatModeAuto {
		return "Auto mode is reserved for the front-of-house agent."
	}
	if mode == ChatModeLaunch && !a.Routable {
		return "Launch mode requires a routable worker."
	}
	return "This mode is not enabled for this agent."
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
