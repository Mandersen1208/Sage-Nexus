package sageagents

import (
	"strings"
	"testing"
)

func TestRegistryBuildsWorkersAndRoutes(t *testing.T) {
	cfg := mustLoadPackageRegistry(t)

	workers := cfg.WorkerAgentIDs()
	for _, id := range []string{"AGT-backend-dev-agent", "AGT-office-document-agent", "AGT-runtime-librarian-agent"} {
		if !containsString(workers, id) {
			t.Fatalf("worker list missing %s: %v", id, workers)
		}
	}

	routes, warnings := cfg.BuildOrchestratorRoutes()
	if len(warnings) > 0 {
		t.Fatalf("unexpected route warnings: %v", warnings)
	}
	if got := routes.Targets["call_office_document_agent"]; got != "AGT-office-document-agent" {
		t.Fatalf("office route target = %q", got)
	}
	if _, ok := routes.Targets["call_runtime_librarian_agent"]; ok {
		t.Fatalf("runtime librarian must not be routable: %v", routes.Targets)
	}

	prompt := cfg.BuildOrchestratorPrompt("base prompt")
	if !containsText(prompt, "## Active Worker Registry") || !containsText(prompt, "call_office_document_agent") {
		t.Fatalf("orchestrator prompt missing generated registry: %s", prompt)
	}
}

func TestRegistryControlsToolsPeersAndSeniorGate(t *testing.T) {
	cfg := mustLoadPackageRegistry(t)

	officeTools, warnings := cfg.ResolveToolsForAgent("AGT-office-document-agent")
	if len(warnings) > 0 {
		t.Fatalf("office tool warnings: %v", warnings)
	}
	for _, tool := range []string{"office_docx_create", "office_xlsx_create", "office_artifact_list", "agent_context_read"} {
		if !containsString(officeTools, tool) {
			t.Fatalf("office tools missing %s: %v", tool, officeTools)
		}
	}
	if containsString(officeTools, "skill_search") || containsString(officeTools, "searxng_search") {
		t.Fatalf("office agent should not receive broad tools: %v", officeTools)
	}

	policy := cfg.PeerPolicy()
	if !containsString(policy.Allowlist["AGT-office-document-agent"], "AGT-qa-agent") {
		t.Fatalf("office peer policy missing QA: %v", policy.Allowlist["AGT-office-document-agent"])
	}
	if cfg.SeniorGateForAgent("AGT-backend-dev-agent") != "off" {
		t.Fatalf("backend senior gate should be off")
	}
	if cfg.SeniorGateForAgent("AGT-office-document-agent") != "off" {
		t.Fatalf("office senior gate should be off")
	}
}

func TestCopilotAgentUsesDiscoveredToolCatalog(t *testing.T) {
	agent := &CopilotAgent{
		AllowedTools: []string{"office_docx_create", "agent_context_read"},
		ToolCatalog: []ToolDefinition{
			toolFixture("office_docx_create"),
			toolFixture("searxng_search"),
			toolFixture("agent_context_read"),
		},
	}

	got := agent.availableTools()
	names := make([]string, 0, len(got))
	for _, tool := range got {
		names = append(names, tool.Function.Name)
	}
	if !containsString(names, "office_docx_create") || !containsString(names, "agent_context_read") {
		t.Fatalf("available tools missing allowed discovered tools: %v", names)
	}
	if containsString(names, "searxng_search") {
		t.Fatalf("available tools exposed unallowed tool: %v", names)
	}
}

func TestChatCatalogIsRegistryDriven(t *testing.T) {
	cfg := mustLoadPackageRegistry(t)
	catalog := cfg.BuildChatCatalog()
	if catalog.DefaultMode.AgentMode != ChatModeAuto || catalog.DefaultMode.Label != "Sage Auto" {
		t.Fatalf("default mode = %+v", catalog.DefaultMode)
	}

	frontend := findCatalogAgent(catalog, "AGT-frontend-dev-agent")
	if frontend == nil {
		t.Fatalf("frontend missing from catalog: %+v", catalog.Agents)
	}
	if frontend.DisplayName != "Frontend" {
		t.Fatalf("frontend display name = %q", frontend.DisplayName)
	}
	if !catalogModeEnabled(frontend, ChatModeSolo) || !catalogModeEnabled(frontend, ChatModeLaunch) {
		t.Fatalf("frontend modes = %+v", frontend.Modes)
	}
	if catalogModeEnabled(frontend, ChatModeAuto) {
		t.Fatalf("frontend must not expose auto mode: %+v", frontend.Modes)
	}

	sage := findCatalogAgent(catalog, SageAgentID)
	if sage == nil {
		t.Fatalf("Sage missing from catalog")
	}
	if !catalogModeEnabled(sage, ChatModeAuto) || !catalogModeEnabled(sage, ChatModeSolo) {
		t.Fatalf("Sage modes = %+v", sage.Modes)
	}
	if catalogModeEnabled(sage, ChatModeLaunch) {
		t.Fatalf("Sage must not expose launch mode: %+v", sage.Modes)
	}
}

func TestResolveChatSelectionValidatesRegistryPolicy(t *testing.T) {
	cfg := mustLoadPackageRegistry(t)
	if selection, err := cfg.ResolveChatSelection(ChatModeSolo, "AGT-frontend-dev-agent"); err != nil || selection.Label != "Frontend Only" {
		t.Fatalf("frontend solo selection = %+v err=%v", selection, err)
	}
	if _, err := cfg.ResolveChatSelection(ChatModeLaunch, SageAgentID); err == nil {
		t.Fatal("Sage launch should be rejected")
	}
	if _, err := cfg.ResolveChatSelection(ChatModeLaunch, "AGT-runtime-librarian-agent"); err == nil {
		t.Fatal("librarian launch should be rejected")
	}
	if _, err := cfg.ResolveChatSelection(ChatModeSolo, "AGT-missing-agent"); err == nil {
		t.Fatal("unknown target should be rejected")
	}
}

func TestSuppressedToolsHidePeerDispatch(t *testing.T) {
	agent := &CopilotAgent{
		AllowedTools: []string{"handoff_to_agent", "complete_task", "list_agents", "agent_context_read"},
		ToolCatalog: []ToolDefinition{
			toolFixture("handoff_to_agent"),
			toolFixture("complete_task"),
			toolFixture("list_agents"),
			toolFixture("agent_context_read"),
		},
	}
	got := agent.availableTools(WithSuppressedTools(nil, "handoff_to_agent", "complete_task", "list_agents"))
	names := make([]string, 0, len(got))
	for _, tool := range got {
		names = append(names, tool.Function.Name)
	}
	if containsString(names, "handoff_to_agent") || containsString(names, "complete_task") || containsString(names, "list_agents") {
		t.Fatalf("suppressed peer tools were exposed: %v", names)
	}
	if !containsString(names, "agent_context_read") {
		t.Fatalf("context tool should remain available: %v", names)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func findCatalogAgent(catalog ChatCatalog, id string) *ChatCatalogAgent {
	for i := range catalog.Agents {
		if catalog.Agents[i].ID == id {
			return &catalog.Agents[i]
		}
	}
	return nil
}

func catalogModeEnabled(agent *ChatCatalogAgent, mode string) bool {
	if agent == nil {
		return false
	}
	for _, item := range agent.Modes {
		if item.ID == mode {
			return item.Enabled
		}
	}
	return false
}

func containsText(value, want string) bool {
	return strings.Contains(value, want)
}

func toolFixture(name string) ToolDefinition {
	return ToolDefinition{
		Type: "function",
		Function: toolFuncDef{
			Name:        name,
			Description: "test tool",
			Parameters:  map[string]interface{}{"type": "object"},
		},
	}
}
