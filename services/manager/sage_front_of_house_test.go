package sageagents

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSageSystemPromptPrefersSoulPath(t *testing.T) {
	t.Setenv("SAGE_SOUL_PATH", filepath.Join(t.TempDir(), "SOUL.md"))
	if err := os.WriteFile(os.Getenv("SAGE_SOUL_PATH"), []byte("real soul"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &AgentsConfig{}
	prompt, source := loadSageSystemPrompt(cfg)
	if prompt != "real soul" {
		t.Fatalf("expected SOUL.md prompt, got %q", prompt)
	}
	if source != os.Getenv("SAGE_SOUL_PATH") {
		t.Fatalf("expected source %q, got %q", os.Getenv("SAGE_SOUL_PATH"), source)
	}
}

func TestLoadSageSystemPromptFallsBackToBundledPrompt(t *testing.T) {
	dir := t.TempDir()
	fallbackPath := filepath.Join(dir, "sage.md")
	if err := os.WriteFile(fallbackPath, []byte("bundled sage"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SAGE_SOUL_PATH", filepath.Join(dir, "missing-SOUL.md"))

	cfg := &AgentsConfig{
		Agents: map[string]AgentConfig{
			SageAgentID: {SystemPromptFile: "sage.md"},
		},
		configDir: dir,
	}
	prompt, source := loadSageSystemPrompt(cfg)
	if prompt != "bundled sage" {
		t.Fatalf("expected bundled prompt, got %q", prompt)
	}
	if source != "bundled" {
		t.Fatalf("expected bundled source, got %q", source)
	}
}
