package sageagents

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	SageAgentID          = "AGT-sage"
	OrchestratorAgentID  = "AGT-sage-orchestrator"
	SeniorDevAgentID     = "AGT-senior-dev-agent"
	DefaultRegistryPath  = "/sage-state/workspace/sage/agents.registry.json"
	defaultPeerMeshDepth = 3
)

var (
	agentIDPattern   = regexp.MustCompile(`^AGT-[A-Za-z0-9][A-Za-z0-9-]*$`)
	routeToolPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	requiredAgentIDs = map[string]struct{}{
		SageAgentID:                   {},
		OrchestratorAgentID:           {},
		"AGT-project-manager-agent":   {},
		SeniorDevAgentID:              {},
		"AGT-frontend-dev-agent":      {},
		"AGT-backend-dev-agent":       {},
		"AGT-devops-agent":            {},
		"AGT-qa-agent":                {},
		"AGT-database-admin-agent":    {},
		"AGT-architect-agent":         {},
		"AGT-research-agent":          {},
		"AGT-financial-agent":         {},
		"AGT-runtime-librarian-agent": {},
	}
)

// LoadAgentRegistry loads the bundled registry and overlays the workspace
// registry when present. The bundled file remains the safety net for required
// core agents; normal agent-specific behavior should come from registry data,
// not Go maps.
func LoadAgentRegistry(bundledPath, workspacePath string) (*AgentsConfig, error) {
	base, err := LoadAgentsConfig(bundledPath)
	if err != nil {
		return nil, err
	}
	normalizeRegistry(base)

	workspacePath = strings.TrimSpace(workspacePath)
	if workspacePath == "" {
		return base, nil
	}
	overlay, err := LoadAgentsConfig(workspacePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return base, nil
		}
		if errors.Is(err, os.ErrPermission) {
			base.Warnings = append(base.Warnings, fmt.Sprintf("workspace registry %s not loaded: %v", workspacePath, err))
			return base, nil
		}
		base.Warnings = append(base.Warnings, fmt.Sprintf("workspace registry %s invalid: %v", workspacePath, err))
		return base, nil
	}
	normalizeRegistry(overlay)
	mergeRegistry(base, overlay)
	return base, nil
}

func normalizeRegistry(cfg *AgentsConfig) {
	if cfg == nil {
		return
	}
	if cfg.Agents == nil {
		cfg.Agents = make(map[string]AgentConfig)
	}
	if cfg.ToolBundles == nil {
		cfg.ToolBundles = make(map[string][]string)
	}
	for id, agent := range cfg.Agents {
		id = strings.TrimSpace(id)
		if agent.ID == "" {
			agent.ID = id
		}
		agent.ID = strings.TrimSpace(agent.ID)
		if agent.sourceDir == "" {
			agent.sourceDir = cfg.configDir
		}
		agent.SeniorGate = normalizeSeniorGate(agent.SeniorGate)
		agent.SupportedChatModes = normalizeChatModes(agent.SupportedChatModes)
		cfg.Agents[id] = agent
	}
}

func mergeRegistry(base, overlay *AgentsConfig) {
	if base == nil || overlay == nil {
		return
	}
	base.SourceFiles = append(base.SourceFiles, overlay.SourceFiles...)
	for name, tools := range overlay.ToolBundles {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		base.ToolBundles[name] = uniqueStrings(tools)
	}
	for key, agent := range overlay.Agents {
		id := strings.TrimSpace(agent.ID)
		if id == "" {
			id = strings.TrimSpace(key)
		}
		agent.ID = id
		if existing, ok := base.Agents[id]; ok {
			agent = mergeAgentConfig(existing, agent)
		}
		if err := validateAgentConfig(key, agent); err != nil {
			base.Warnings = append(base.Warnings, fmt.Sprintf("skipping registry agent %s: %v", id, err))
			continue
		}
		if !agent.EnabledValue() && IsRequiredAgent(id) {
			base.Warnings = append(base.Warnings, fmt.Sprintf("required agent %s cannot be disabled by workspace registry", id))
			continue
		}
		base.Agents[id] = agent
	}
}

func mergeAgentConfig(base, overlay AgentConfig) AgentConfig {
	out := base
	if overlay.ID != "" {
		out.ID = overlay.ID
	}
	if overlay.Enabled != nil {
		out.Enabled = overlay.Enabled
	}
	if overlay.Model != "" {
		out.Model = overlay.Model
	}
	if overlay.SystemPromptFile != "" {
		out.SystemPromptFile = overlay.SystemPromptFile
		out.sourceDir = overlay.sourceDir
	}
	if overlay.Routable {
		out.Routable = overlay.Routable
	}
	if overlay.Targetable != nil {
		out.Targetable = overlay.Targetable
	}
	if overlay.DisplayName != "" {
		out.DisplayName = overlay.DisplayName
	}
	if len(overlay.SupportedChatModes) > 0 {
		out.SupportedChatModes = overlay.SupportedChatModes
	}
	if len(overlay.ModeLabels) > 0 {
		out.ModeLabels = overlay.ModeLabels
	}
	if len(overlay.ModeDescriptions) > 0 {
		out.ModeDescriptions = overlay.ModeDescriptions
	}
	if overlay.RouteToolName != "" {
		out.RouteToolName = overlay.RouteToolName
	}
	if overlay.RouteDescription != "" {
		out.RouteDescription = overlay.RouteDescription
	}
	if len(overlay.ToolBundles) > 0 {
		out.ToolBundles = overlay.ToolBundles
	}
	if len(overlay.Tools) > 0 {
		out.Tools = overlay.Tools
	}
	if len(overlay.PeerTargets) > 0 {
		out.PeerTargets = overlay.PeerTargets
	}
	if overlay.MaxPeerDepth > 0 {
		out.MaxPeerDepth = overlay.MaxPeerDepth
	}
	if overlay.SeniorGate != "" {
		out.SeniorGate = overlay.SeniorGate
	}
	if overlay.Authority != "" {
		out.Authority = overlay.Authority
	}
	if len(overlay.MustNotOwn) > 0 {
		out.MustNotOwn = overlay.MustNotOwn
	}
	if out.sourceDir == "" {
		out.sourceDir = overlay.sourceDir
	}
	return out
}

func validateAgentConfig(key string, agent AgentConfig) error {
	id := strings.TrimSpace(agent.ID)
	if id == "" {
		return fmt.Errorf("missing id")
	}
	if strings.TrimSpace(key) != "" && strings.TrimSpace(key) != id {
		return fmt.Errorf("key %q does not match id %q", key, id)
	}
	if !agentIDPattern.MatchString(id) {
		return fmt.Errorf("invalid agent id")
	}
	if agent.Routable {
		if strings.TrimSpace(agent.RouteToolName) == "" {
			return fmt.Errorf("routable agent missing routeToolName")
		}
		if !routeToolPattern.MatchString(agent.RouteToolName) {
			return fmt.Errorf("invalid routeToolName %q", agent.RouteToolName)
		}
		if strings.TrimSpace(agent.RouteDescription) == "" {
			return fmt.Errorf("routable agent missing routeDescription")
		}
	}
	if normalizeSeniorGate(agent.SeniorGate) == "" {
		return fmt.Errorf("invalid seniorGate %q", agent.SeniorGate)
	}
	for _, mode := range agent.SupportedChatModes {
		if !isKnownChatMode(mode) {
			return fmt.Errorf("invalid supportedChatModes value %q", mode)
		}
	}
	return nil
}

func normalizeSeniorGate(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "off", "false", "none", "never":
		return "off"
	case "delivery", "mutating", "write", "writes":
		return "delivery"
	case "always", "true":
		return "always"
	default:
		return ""
	}
}

func IsRequiredAgent(agentID string) bool {
	_, ok := requiredAgentIDs[strings.TrimSpace(agentID)]
	return ok
}

func (a AgentConfig) EnabledValue() bool {
	return a.Enabled == nil || *a.Enabled
}

func (a AgentConfig) IsWorker() bool {
	return a.EnabledValue() && a.ID != "" && a.ID != SageAgentID && a.ID != OrchestratorAgentID
}

func (a AgentConfig) TargetableValue() bool {
	if a.Targetable != nil {
		return *a.Targetable
	}
	if a.ID == SageAgentID {
		return a.EnabledValue()
	}
	if a.ID == OrchestratorAgentID {
		return false
	}
	return a.IsWorker() && a.Routable
}

func (c *AgentsConfig) ActiveAgentIDs() []string {
	if c == nil {
		return nil
	}
	ids := make([]string, 0, len(c.Agents))
	for id, agent := range c.Agents {
		if agent.ID == "" {
			agent.ID = id
		}
		if agent.EnabledValue() {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func (c *AgentsConfig) WorkerAgentIDs() []string {
	if c == nil {
		return nil
	}
	ids := make([]string, 0, len(c.Agents))
	for id, agent := range c.Agents {
		if agent.ID == "" {
			agent.ID = id
		}
		if agent.IsWorker() {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func (c *AgentsConfig) ResolveToolsForAgent(agentID string) (tools []string, warnings []string) {
	if c == nil {
		return nil, nil
	}
	agent := c.Get(agentID)
	seen := map[string]struct{}{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		tools = append(tools, name)
	}
	for _, bundle := range agent.ToolBundles {
		bundle = strings.TrimSpace(bundle)
		bundleTools, ok := c.ToolBundles[bundle]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("%s references unknown tool bundle %q", agentID, bundle))
			continue
		}
		for _, tool := range bundleTools {
			add(tool)
		}
	}
	for _, tool := range agent.Tools {
		add(tool)
	}
	sort.Strings(tools)
	return tools, warnings
}

type OrchestratorRoutes struct {
	Tools   []toolDef
	Targets map[string]string
}

func (c *AgentsConfig) BuildOrchestratorRoutes() (OrchestratorRoutes, []string) {
	routes := OrchestratorRoutes{Targets: map[string]string{}}
	if c == nil {
		return routes, nil
	}
	warnings := []string{}
	for _, id := range c.ActiveAgentIDs() {
		agent := c.Get(id)
		if !agent.Routable || !agent.IsWorker() {
			continue
		}
		name := strings.TrimSpace(agent.RouteToolName)
		if name == "" {
			continue
		}
		if existing, ok := routes.Targets[name]; ok {
			warnings = append(warnings, fmt.Sprintf("route tool %s already points to %s; skipping %s", name, existing, id))
			continue
		}
		routes.Targets[name] = id
		routes.Tools = append(routes.Tools, toolDef{
			Type: "function",
			Function: toolFuncDef{
				Name:        name,
				Description: strings.TrimSpace(agent.RouteDescription),
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{"type": "string", "description": "The full request for this specialist agent"},
					},
					"required": []string{"query"},
				},
			},
		})
	}
	sort.Slice(routes.Tools, func(i, j int) bool {
		return routes.Tools[i].Function.Name < routes.Tools[j].Function.Name
	})
	return routes, warnings
}

func (c *AgentsConfig) BuildOrchestratorPrompt(base string) string {
	base = strings.TrimSpace(base)
	catalog := c.OrchestratorWorkerCatalog()
	if catalog == "" {
		return base
	}
	if base == "" {
		return catalog
	}
	return base + "\n\n" + catalog
}

func (c *AgentsConfig) OrchestratorWorkerCatalog() string {
	routes, _ := c.BuildOrchestratorRoutes()
	if len(routes.Tools) == 0 {
		return ""
	}
	lines := []string{"## Active Worker Registry", "", "These workers are loaded from the active agent registry. Use only these route tools:"}
	for _, tool := range routes.Tools {
		target := routes.Targets[tool.Function.Name]
		lines = append(lines, fmt.Sprintf("- `%s(query)` -> `%s`: %s", tool.Function.Name, target, tool.Function.Description))
	}
	return strings.Join(lines, "\n")
}

func (c *AgentsConfig) PeerPolicy() PeerPolicy {
	policy := PeerPolicy{
		Allowlist:        map[string][]string{},
		MaxDepth:         defaultPeerMeshDepth,
		MaxDepthByCaller: map[string]int{},
	}
	if c == nil {
		return policy
	}
	for _, id := range c.ActiveAgentIDs() {
		agent := c.Get(id)
		if len(agent.PeerTargets) > 0 {
			policy.Allowlist[id] = uniqueStrings(agent.PeerTargets)
		}
		if agent.MaxPeerDepth > 0 {
			policy.MaxDepthByCaller[id] = agent.MaxPeerDepth
		}
	}
	return policy
}

func (c *AgentsConfig) SeniorGateForAgent(agentID string) string {
	if c == nil {
		return "off"
	}
	return normalizeSeniorGate(c.Get(agentID).SeniorGate)
}

func (c *AgentsConfig) ResolveRegistryPath(pathValue string) string {
	pathValue = strings.TrimSpace(pathValue)
	if pathValue == "" {
		return ""
	}
	if filepath.IsAbs(pathValue) {
		return pathValue
	}
	if c == nil || c.configDir == "" {
		return pathValue
	}
	return filepath.Join(c.configDir, pathValue)
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
