package main

import (
	"path/filepath"
	"strings"
	"testing"

	sageagents "github.com/matta/sage-nexus/services/manager"
)

func TestRegistryControlledToolExposure(t *testing.T) {
	cfg := mustLoadTestRegistry(t)
	sageagents.ConfigurePeerPolicy(cfg.PeerPolicy())
	available := registryToolSet(cfg)

	pmTools := filteredRegistryTools(t, cfg, available, "AGT-project-manager-agent")
	if !hasTool(pmTools, "searxng_search") {
		t.Fatalf("project manager should have searxng_search")
	}
	if hasTool(pmTools, "budget_get_current_month") {
		t.Fatalf("project manager should not have budget tools")
	}

	finTools := filteredRegistryTools(t, cfg, available, "AGT-financial-agent")
	if !hasTool(finTools, "searxng_search") {
		t.Fatalf("financial agent should have searxng_search")
	}
	if !hasTool(finTools, "budget_get_current_month") {
		t.Fatalf("financial agent should have budget tools")
	}
	if !hasTool(finTools, "budget_get_month_checkup") {
		t.Fatalf("financial agent should have deterministic budget checkup tool")
	}

	backendTools := filteredRegistryTools(t, cfg, available, "AGT-backend-dev-agent")
	if hasTool(backendTools, "searxng_search") {
		t.Fatalf("backend agent should not have searxng_search")
	}
	if hasTool(backendTools, "budget_get_current_month") {
		t.Fatalf("backend agent should not have budget tools")
	}
	if !hasTool(backendTools, "skill_get") {
		t.Fatalf("backend agent should have skill tools")
	}
	if !hasTool(backendTools, "agent_context_read") || !hasTool(backendTools, "agent_context_append") {
		t.Fatalf("backend agent should have work context tools")
	}

	officeTools := filteredRegistryTools(t, cfg, available, "AGT-office-document-agent")
	for _, tool := range []string{"office_docx_create", "office_xlsx_create", "office_artifact_list", "agent_context_read"} {
		if !hasTool(officeTools, tool) {
			t.Fatalf("office agent missing %s in %v", tool, officeTools)
		}
	}
	if hasTool(officeTools, "skill_search") || hasTool(officeTools, "searxng_search") || hasTool(officeTools, "budget_get_current_month") {
		t.Fatalf("office agent should only get office/context tools: %v", officeTools)
	}
}

func TestCleanBrowserPathRejectsTraversalAndAbsolutePaths(t *testing.T) {
	for _, input := range []string{"..", "../sage-nexus", "sage/../../secret", "/tmp", `C:\Users\matta`} {
		if got := cleanBrowserPath(input); got != "" {
			t.Fatalf("cleanBrowserPath(%q) = %q, want rejection", input, got)
		}
	}
	if got := cleanBrowserPath(""); got != "." {
		t.Fatalf("cleanBrowserPath(empty) = %q, want root", got)
	}
	if got := cleanBrowserPath("sage-nexus/services"); got != "sage-nexus/services" {
		t.Fatalf("cleanBrowserPath(valid) = %q", got)
	}
}

func TestResolveBrowserPathStaysInsideRoot(t *testing.T) {
	root := t.TempDir()
	inside, ok := resolveBrowserPath(root, "sage-nexus")
	if !ok {
		t.Fatalf("expected path inside root to pass")
	}
	if !strings.HasPrefix(inside, root) {
		t.Fatalf("resolved inside path %q outside root %q", inside, root)
	}

	outsideRoot := root + "-other"
	outside, ok := resolveBrowserPath(root, "../"+filepath.Base(outsideRoot))
	if ok {
		t.Fatalf("expected sibling path to be rejected, got %q", outside)
	}
}

func TestRegistryToolExposureMeshCanBeDisabled(t *testing.T) {
	t.Setenv("SAGE_AGENT_MESH_ENABLED", "false")
	cfg := mustLoadTestRegistry(t)
	sageagents.ConfigurePeerPolicy(cfg.PeerPolicy())

	tools := filteredRegistryTools(t, cfg, registryToolSet(cfg), "AGT-frontend-dev-agent")
	if hasTool(tools, "list_agents") || hasTool(tools, "call_agent") {
		t.Fatalf("frontend agent got peer mesh tools while disabled: %v", tools)
	}
}

func TestRuntimeLibrarianToolsOnlyRuntimeInventory(t *testing.T) {
	cfg := mustLoadTestRegistry(t)
	sageagents.ConfigurePeerPolicy(cfg.PeerPolicy())

	tools := filteredRegistryTools(t, cfg, registryToolSet(cfg), "AGT-runtime-librarian-agent")
	want := []string{
		"runtime_inventory_scan",
		"runtime_inventory_search",
		"runtime_inventory_events",
		"agent_context_read",
		"agent_context_append",
		"agent_context_search",
	}
	if len(tools) != len(want) {
		t.Fatalf("runtime librarian tools = %v, want %v", tools, want)
	}
	for _, tool := range want {
		if !hasTool(tools, tool) {
			t.Fatalf("runtime librarian missing %s in %v", tool, tools)
		}
	}
	if hasTool(tools, "skill_search") || hasTool(tools, "searxng_search") || hasTool(tools, "list_agents") || hasTool(tools, "call_agent") {
		t.Fatalf("runtime librarian should not have broad tools: %v", tools)
	}
}

func TestUnavailableRegistryToolIsFiltered(t *testing.T) {
	cfg := mustLoadTestRegistry(t)
	tools := filteredRegistryTools(t, cfg, map[string]struct{}{"agent_context_read": {}}, "AGT-office-document-agent")
	if len(tools) != 1 || tools[0] != "agent_context_read" {
		t.Fatalf("unavailable MCP tools should be filtered, got %v", tools)
	}
}

func mustLoadTestRegistry(t *testing.T) *sageagents.AgentsConfig {
	t.Helper()
	cfg, err := sageagents.LoadAgentRegistry(filepath.Join("..", "..", "config", "agents.json"), "")
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	return cfg
}

func filteredRegistryTools(t *testing.T, cfg *sageagents.AgentsConfig, available map[string]struct{}, agentID string) []string {
	t.Helper()
	tools, warnings := cfg.ResolveToolsForAgent(agentID)
	if len(warnings) > 0 {
		t.Fatalf("unexpected tool warnings for %s: %v", agentID, warnings)
	}
	return filterAllowedMCPTools(agentID, tools, available)
}

func registryToolSet(cfg *sageagents.AgentsConfig) map[string]struct{} {
	available := map[string]struct{}{}
	for _, tools := range cfg.ToolBundles {
		for _, tool := range tools {
			available[tool] = struct{}{}
		}
	}
	return available
}

func hasTool(tools []string, name string) bool {
	for _, tool := range tools {
		if tool == name {
			return true
		}
	}
	return false
}
