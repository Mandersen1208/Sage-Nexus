package sageagents

import "testing"

func TestRequiresSeniorGate(t *testing.T) {
	orch := &SageOrchestratorAgent{Registry: mustLoadPackageRegistry(t)}

	tests := []struct {
		name     string
		workerID string
		want     bool
	}{
		{name: "frontend gated", workerID: "AGT-frontend-dev-agent", want: true},
		{name: "backend gated", workerID: "AGT-backend-dev-agent", want: true},
		{name: "devops gated", workerID: "AGT-devops-agent", want: true},
		{name: "qa gated", workerID: "AGT-qa-agent", want: true},
		{name: "dba gated", workerID: "AGT-database-admin-agent", want: true},
		{name: "architect gated", workerID: "AGT-architect-agent", want: true},
		{name: "project manager not gated", workerID: "AGT-project-manager-agent", want: false},
		{name: "senior not gated", workerID: SeniorDevAgentID, want: false},
		{name: "research not gated", workerID: "AGT-research-agent", want: false},
		{name: "financial not gated", workerID: "AGT-financial-agent", want: false},
		{name: "runtime librarian not gated", workerID: "AGT-runtime-librarian-agent", want: false},
		{name: "office not gated", workerID: "AGT-office-document-agent", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := orch.requiresSeniorGate(tc.workerID); got != tc.want {
				t.Fatalf("requiresSeniorGate(%q) = %v, want %v", tc.workerID, got, tc.want)
			}
		})
	}
}

func TestRequiresSeniorGateForRequest(t *testing.T) {
	orch := &SageOrchestratorAgent{Registry: mustLoadPackageRegistry(t)}

	tests := []struct {
		name     string
		workerID string
		query    string
		want     bool
	}{
		{name: "frontend implementation gated", workerID: "AGT-frontend-dev-agent", query: "implement the mobile chat layout", want: true},
		{name: "application creation gated", workerID: "AGT-frontend-dev-agent", query: "make me an application", want: true},
		{name: "backend fix gated", workerID: "AGT-backend-dev-agent", query: "fix the session delete route", want: true},
		{name: "devops deploy gated", workerID: "AGT-devops-agent", query: "deploy the compose update", want: true},
		{name: "architect document gated", workerID: "AGT-architect-agent", query: "create an architecture document", want: true},
		{name: "architect read only skipped", workerID: "AGT-architect-agent", query: "analyze the MCP server architecture and explain if it uses RAG", want: false},
		{name: "qa read only skipped", workerID: "AGT-qa-agent", query: "explain the current test strategy", want: false},
		{name: "librarian never gated", workerID: "AGT-runtime-librarian-agent", query: "where is SOUL.md loaded from?", want: false},
		{name: "research never gated", workerID: "AGT-research-agent", query: "research current React guidance", want: false},
		{name: "office never gated", workerID: "AGT-office-document-agent", query: "create a DOCX handoff", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := orch.requiresSeniorGateForRequest(tc.workerID, tc.query); got != tc.want {
				t.Fatalf("requiresSeniorGateForRequest(%q, %q) = %v, want %v", tc.workerID, tc.query, got, tc.want)
			}
		})
	}
}

func TestRequiresOrchestratorWorkerRoute(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "application creation", input: "make me an application", want: true},
		{name: "wrapped current request", input: "Recent chat context:\n- talked about apps\n\nCurrent user request:\nmake me an application", want: true},
		{name: "work command", input: "fix the chat route", want: true},
		{name: "casual chat", input: "hey sage", want: false},
		{name: "read only question", input: "what makes Redis useful?", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := requiresOrchestratorWorkerRoute(tc.input); got != tc.want {
				t.Fatalf("requiresOrchestratorWorkerRoute(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestShouldRejectDirectOrchestratorReply(t *testing.T) {
	if !shouldRejectDirectOrchestratorReply("make me an application", nil) {
		t.Fatal("route-required request without route trace should be rejected")
	}
	if shouldRejectDirectOrchestratorReply("make me an application", []string{"call_frontend_dev_agent"}) {
		t.Fatal("route-required request with route trace should not be rejected")
	}
	if shouldRejectDirectOrchestratorReply("hey sage", nil) {
		t.Fatal("casual direct response should be allowed")
	}
}

func TestFallbackRouteWorkerIDPrefersProjectManager(t *testing.T) {
	orch := &SageOrchestratorAgent{Workers: map[string]*CopilotAgent{
		defaultSageAutoFallbackWorkerID: {BaseAgent: BaseAgent{AgentID: defaultSageAutoFallbackWorkerID}},
		SeniorDevAgentID:                {BaseAgent: BaseAgent{AgentID: SeniorDevAgentID}},
	}}

	if got := orch.fallbackRouteWorkerID(); got != defaultSageAutoFallbackWorkerID {
		t.Fatalf("fallbackRouteWorkerID() = %q, want %q", got, defaultSageAutoFallbackWorkerID)
	}
}

func TestCurrentRouteRequestPreservesOriginalCase(t *testing.T) {
	input := "Recent chat context:\n- prior app discussion\n\nCurrent user request:\nMake Me An Application"
	if got := currentRouteRequest(input); got != "Make Me An Application" {
		t.Fatalf("currentRouteRequest() = %q, want original-cased request", got)
	}
	if got := normalizeRouteRequest(input); got != "make me an application" {
		t.Fatalf("normalizeRouteRequest() = %q, want lowercase current request", got)
	}
}

func TestRuntimeLibrarianIsNotTopLevelRoute(t *testing.T) {
	cfg := mustLoadPackageRegistry(t)
	routes, warnings := cfg.BuildOrchestratorRoutes()
	if len(warnings) > 0 {
		t.Fatalf("unexpected route warnings: %v", warnings)
	}
	if _, ok := routes.Targets["call_runtime_librarian_agent"]; ok {
		t.Fatal("runtime librarian should not be exposed as a top-level orchestrator route")
	}

	for _, tool := range routes.Tools {
		if tool.Function.Name == "call_runtime_librarian_agent" {
			t.Fatal("runtime librarian tool should be reserved for senior/architect inventory workflow, not top-level routing")
		}
	}
	if got := routes.Targets["call_office_document_agent"]; got != "AGT-office-document-agent" {
		t.Fatalf("office route target = %q, want AGT-office-document-agent", got)
	}
}

func TestClassifySageRoute(t *testing.T) {
	t.Setenv("SAGE_FAST_PATH_ENABLED", "true")

	tests := []struct {
		name       string
		input      string
		wantDirect bool
		wantReason string
	}{
		{name: "greeting direct", input: "hey", wantDirect: true, wantReason: "conversation"},
		{name: "ack direct", input: "sounds good", wantDirect: true, wantReason: "acknowledgement"},
		{name: "preference direct", input: "I like when you use emoji a little more", wantDirect: true, wantReason: "conversation"},
		{name: "voice feedback direct", input: "huh that did not sound like Sage :(", wantDirect: true, wantReason: "voice_feedback"},
		{name: "story direct", input: "tell me a story", wantDirect: true, wantReason: "casual_creative"},
		{name: "fix delegates", input: "fix the chat session delete route", wantDirect: false, wantReason: "work_request"},
		{name: "soul provenance delegates", input: "where is your SOUL.md coming from?", wantDirect: false, wantReason: "system_or_runtime"},
		{name: "docker delegates", input: "look into the Docker compose networking", wantDirect: false, wantReason: "work_request"},
		{name: "doc delegates", input: "create a document for the architecture", wantDirect: false, wantReason: "work_request"},
		{name: "research delegates", input: "research current OpenAI vision models", wantDirect: false, wantReason: "research_or_current_fact"},
		{name: "finance delegates", input: "what did we spend on groceries?", wantDirect: false, wantReason: "finance"},
		{name: "image delegates", input: "check this screenshot for issues", wantDirect: false, wantReason: "attachment_or_vision"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := classifySageRoute(tc.input)
			if got.Direct != tc.wantDirect || got.Reason != tc.wantReason {
				t.Fatalf("classifySageRoute(%q) = direct %v reason %q, want direct %v reason %q", tc.input, got.Direct, got.Reason, tc.wantDirect, tc.wantReason)
			}
		})
	}
}

func TestResolveSageRevoicePolicy(t *testing.T) {
	t.Run("default selective", func(t *testing.T) {
		t.Setenv("SAGE_REVOICE_POLICY", "")
		t.Setenv("SAGE_REVOICE_DELEGATED", "")
		if got := resolveSageRevoicePolicy(); got != revoicePolicySelective {
			t.Fatalf("resolveSageRevoicePolicy() = %q, want %q", got, revoicePolicySelective)
		}
	})

	t.Run("explicit policies", func(t *testing.T) {
		tests := []sageRevoicePolicy{
			revoicePolicySelective,
			revoicePolicyAlways,
			revoicePolicyWrapper,
			revoicePolicyOff,
		}
		for _, want := range tests {
			t.Run(string(want), func(t *testing.T) {
				t.Setenv("SAGE_REVOICE_POLICY", string(want))
				t.Setenv("SAGE_REVOICE_DELEGATED", "")
				if got := resolveSageRevoicePolicy(); got != want {
					t.Fatalf("resolveSageRevoicePolicy() = %q, want %q", got, want)
				}
			})
		}
	})

	t.Run("legacy on maps to always", func(t *testing.T) {
		t.Setenv("SAGE_REVOICE_POLICY", "")
		t.Setenv("SAGE_REVOICE_DELEGATED", "true")
		if got := resolveSageRevoicePolicy(); got != revoicePolicyAlways {
			t.Fatalf("resolveSageRevoicePolicy() = %q, want %q", got, revoicePolicyAlways)
		}
	})

	t.Run("legacy off maps to off", func(t *testing.T) {
		t.Setenv("SAGE_REVOICE_POLICY", "")
		t.Setenv("SAGE_REVOICE_DELEGATED", "false")
		if got := resolveSageRevoicePolicy(); got != revoicePolicyOff {
			t.Fatalf("resolveSageRevoicePolicy() = %q, want %q", got, revoicePolicyOff)
		}
	})

	t.Run("policy wins over legacy", func(t *testing.T) {
		t.Setenv("SAGE_REVOICE_POLICY", "wrapper")
		t.Setenv("SAGE_REVOICE_DELEGATED", "true")
		if got := resolveSageRevoicePolicy(); got != revoicePolicyWrapper {
			t.Fatalf("resolveSageRevoicePolicy() = %q, want %q", got, revoicePolicyWrapper)
		}
	})
}

func TestClassifyDelegatedRevoice(t *testing.T) {
	t.Run("normal answer uses pass-through by default", func(t *testing.T) {
		t.Setenv("SAGE_REVOICE_POLICY", "")
		t.Setenv("SAGE_REVOICE_DELEGATED", "")
		got := classifyDelegatedRevoice("explain why the latency is better", "The latency improved because one model hop was removed.")
		if got.Policy != revoicePolicySelective || got.Mode != revoiceModeSkip || got.SkipReason != "pass_through_default" {
			t.Fatalf("classifyDelegatedRevoice() = policy %q mode %q reason %q", got.Policy, got.Mode, got.Reason)
		}
	})

	t.Run("technical docs use pass-through", func(t *testing.T) {
		t.Setenv("SAGE_REVOICE_POLICY", "selective")
		got := classifyDelegatedRevoice("create technical documentation for the chat API", "## Overview\n\nUse POST /chat.\n\n## Routes\n\n| Method | Route |\n| --- | --- |")
		if got.Mode != revoiceModeSkip || got.SkipReason != "technical_artifact" {
			t.Fatalf("classifyDelegatedRevoice() = mode %q skip %q, want skip technical_artifact", got.Mode, got.SkipReason)
		}
	})

	t.Run("code blocks use pass-through", func(t *testing.T) {
		t.Setenv("SAGE_REVOICE_POLICY", "selective")
		got := classifyDelegatedRevoice("show the config", "```yaml\nSAGE_REVOICE_POLICY: selective\n```")
		if got.Mode != revoiceModeSkip || got.SkipReason != "technical_artifact" {
			t.Fatalf("classifyDelegatedRevoice() = mode %q skip %q, want skip technical_artifact", got.Mode, got.SkipReason)
		}
	})

	t.Run("json is skipped", func(t *testing.T) {
		t.Setenv("SAGE_REVOICE_POLICY", "selective")
		got := classifyDelegatedRevoice("return the raw object", `{"status":"ok","count":2}`)
		if got.Mode != revoiceModeSkip || got.SkipReason != "machine_readable" {
			t.Fatalf("classifyDelegatedRevoice() = mode %q skip %q, want skip machine_readable", got.Mode, got.SkipReason)
		}
	})

	t.Run("raw logs are skipped", func(t *testing.T) {
		t.Setenv("SAGE_REVOICE_POLICY", "selective")
		logs := "2026-04-30T10:00:00Z level=warn first\n2026-04-30T10:00:01Z level=error second\nExit code: 1"
		got := classifyDelegatedRevoice("what happened in this log", logs)
		if got.Mode != revoiceModeSkip || got.SkipReason != "raw_output" {
			t.Fatalf("classifyDelegatedRevoice() = mode %q skip %q, want skip raw_output", got.Mode, got.SkipReason)
		}
	})

	t.Run("tool errors are skipped", func(t *testing.T) {
		t.Setenv("SAGE_REVOICE_POLICY", "selective")
		got := classifyDelegatedRevoice("search this", "MCP tool returned error: SearXNG returned zero results.")
		if got.Mode != revoiceModeSkip || got.SkipReason != "tool_error" {
			t.Fatalf("classifyDelegatedRevoice() = mode %q skip %q, want skip tool_error", got.Mode, got.SkipReason)
		}
	})
}
