package sageagents

import (
	"context"
	"path/filepath"
	"testing"
)

func TestIsPeerCallAllowedFromRegistryPolicy(t *testing.T) {
	cfg := mustLoadPackageRegistry(t)
	ConfigurePeerPolicy(cfg.PeerPolicy())

	tests := []struct {
		name   string
		caller string
		target string
		want   bool
	}{
		{name: "frontend can consult backend", caller: "AGT-frontend-dev-agent", target: "AGT-backend-dev-agent", want: true},
		{name: "backend can consult frontend", caller: "AGT-backend-dev-agent", target: "AGT-frontend-dev-agent", want: true},
		{name: "architect can consult research", caller: "AGT-architect-agent", target: "AGT-research-agent", want: true},
		{name: "qa can consult architect", caller: "AGT-qa-agent", target: "AGT-architect-agent", want: true},
		{name: "senior can consult devops", caller: SeniorDevAgentID, target: "AGT-devops-agent", want: true},
		{name: "runtime librarian cannot take peers", caller: "AGT-runtime-librarian-agent", target: "AGT-backend-dev-agent", want: false},
		{name: "financial isolated from engineering mesh", caller: "AGT-financial-agent", target: "AGT-research-agent", want: false},
		{name: "self call rejected", caller: "AGT-backend-dev-agent", target: "AGT-backend-dev-agent", want: false},
		{name: "unknown rejected", caller: "AGT-unknown-agent", target: "AGT-backend-dev-agent", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPeerCallAllowed(tc.caller, tc.target); got != tc.want {
				t.Fatalf("IsPeerCallAllowed(%q, %q) = %v, want %v", tc.caller, tc.target, got, tc.want)
			}
		})
	}
}

func TestPeerCallDepthContext(t *testing.T) {
	t.Parallel()

	ctx := WithPeerCallDepth(context.Background(), 2)
	if got := PeerCallDepthFromContext(ctx); got != 2 {
		t.Fatalf("PeerCallDepthFromContext() = %d, want 2", got)
	}

	ctx = WithPeerCallDepth(context.Background(), -10)
	if got := PeerCallDepthFromContext(ctx); got != 0 {
		t.Fatalf("negative depth = %d, want 0", got)
	}
}

func TestValidatePeerCallDepth(t *testing.T) {
	t.Setenv("SAGE_AGENT_MESH_MAX_DEPTH", "2")
	cfg := mustLoadPackageRegistry(t)
	ConfigurePeerPolicy(cfg.PeerPolicy())

	if err := ValidatePeerCall("AGT-frontend-dev-agent", "AGT-backend-dev-agent", 0); err != nil {
		t.Fatalf("allowed peer call rejected: %v", err)
	}
	if err := ValidatePeerCall("AGT-frontend-dev-agent", "AGT-runtime-librarian-agent", 0); err == nil {
		t.Fatal("disallowed peer call was accepted")
	}
	if err := ValidatePeerCall("AGT-frontend-dev-agent", "AGT-backend-dev-agent", 2); err == nil {
		t.Fatal("depth-limited peer call was accepted")
	}
}

func mustLoadPackageRegistry(t *testing.T) *AgentsConfig {
	t.Helper()
	cfg, err := LoadAgentRegistry(filepath.Join("config", "agents.json"), "")
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	return cfg
}
