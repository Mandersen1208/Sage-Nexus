package main

import (
	"strings"

	sageagents "github.com/matta/sage-nexus/services/manager"
)

type initialRouteDecision struct {
	Target          string   `json:"target"`
	AgentID         string   `json:"agentId,omitempty"`
	SuggestedSkills []string `json:"suggestedSkills,omitempty"`
	Reason          string   `json:"reason"`
}

func decideInitialRoute(cfg *sageagents.AgentsConfig, request string) initialRouteDecision {
	normalized := normalizeInitialRouteRequest(request)
	if isSageConversation(normalized) {
		return initialRouteDecision{Target: "sage", Reason: "conversation"}
	}

	candidates := []struct {
		agentID string
		reason  string
		skills  []string
		terms   []string
	}{
		{"AGT-financial-agent", "finance", []string{"personal-finance"}, []string{"budget", "spend", "spent", "groceries", "income", "savings", "debt", "invest", "finance", "money", "transaction"}},
		{"AGT-office-document-agent", "office_artifact", nil, []string{"docx", "xlsx", "spreadsheet", "workbook", "word document", "excel", "calendar", "ics", "formatted document"}},
		{"AGT-senior-dev-agent", "code_review", []string{"clean-code"}, []string{"code review", "review this code", "review the code", "clean code", "quality review", "maintainability", "warnings"}},
		{"AGT-architect-agent", "architecture_or_build_plan", nil, []string{"architecture", "architect", "system design", "design the system", "ground up", "from scratch", "make me an application", "make me an app", "make me a website", "build an application", "build an app"}},
		{"AGT-devops-agent", "infrastructure", nil, []string{"docker", "compose", "deploy", "deployment", "ci", "cd", "pipeline", "container", "networking", "runtime", "startup.ps1"}},
		{"AGT-database-admin-agent", "database", nil, []string{"database", "schema", "migration", "sql", "postgres", "query", "index"}},
		{"AGT-frontend-dev-agent", "frontend", nil, []string{"frontend", "react", "css", "ui", "component", "responsive", "layout", "dashboard", "browser", "screen"}},
		{"AGT-backend-dev-agent", "backend", nil, []string{"backend", "api", "endpoint", "handler", "server", "service", "redis", "go route", "http route"}},
		{"AGT-qa-agent", "quality_assurance", nil, []string{"test", "tests", "qa", "regression", "validation", "acceptance"}},
		{"AGT-research-agent", "research", []string{"web-search"}, []string{"research", "latest", "current", "docs", "documentation", "look up", "source", "cite"}},
		{"AGT-project-manager-agent", "requirements", nil, []string{"requirements", "acceptance criteria", "project plan", "timeline", "milestone", "scope"}},
	}
	for _, candidate := range candidates {
		if containsAnyInitialRouteTerm(normalized, candidate.terms) && routeAgentAvailable(cfg, candidate.agentID) {
			return initialRouteDecision{
				Target:          "agent",
				AgentID:         candidate.agentID,
				SuggestedSkills: candidate.skills,
				Reason:          candidate.reason,
			}
		}
	}
	if routeAgentAvailable(cfg, "AGT-architect-agent") && containsAnyInitialRouteTerm(normalized, []string{"build", "create", "implement", "refactor", "fix", "change", "update"}) {
		return initialRouteDecision{Target: "agent", AgentID: "AGT-architect-agent", Reason: "delivery_work"}
	}
	return initialRouteDecision{Target: "sage", Reason: "sage_handles"}
}

func normalizeInitialRouteRequest(request string) string {
	request = strings.TrimSpace(request)
	lower := strings.ToLower(request)
	for _, marker := range []string{"current user request:", "current request:"} {
		if idx := strings.LastIndex(lower, marker); idx >= 0 {
			return strings.TrimSpace(lower[idx+len(marker):])
		}
	}
	return lower
}

func isSageConversation(normalized string) bool {
	if normalized == "" {
		return true
	}
	short := strings.TrimSpace(normalized)
	switch short {
	case "hi", "hey", "hello", "yo", "thanks", "thank you", "ok", "okay", "sounds good", "lol":
		return true
	}
	if strings.HasPrefix(short, "tell me a story") || strings.HasPrefix(short, "who are you") {
		return true
	}
	return false
}

func containsAnyInitialRouteTerm(input string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(input, term) {
			return true
		}
	}
	return false
}

func routeAgentAvailable(cfg *sageagents.AgentsConfig, agentID string) bool {
	if cfg == nil {
		return true
	}
	agent := cfg.Get(agentID)
	return agent.ID != "" && agent.EnabledValue()
}
