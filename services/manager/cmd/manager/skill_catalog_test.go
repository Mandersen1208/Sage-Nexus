package main

import (
	"strings"
	"testing"
)

func TestSkillCatalogUpdate_TogglesEnabled(t *testing.T) {
	runtime := newSkillCatalogRuntime(t.TempDir())
	enabled := true

	created, err := runtime.Create(skillUpsertRequest{
		ID:          "architecture-review",
		Name:        "Architecture Review",
		Description: "Review architecture decisions",
		Tags:        []string{"review"},
		Enabled:     &enabled,
		Content:     "# Architecture Review\n\nReview architecture decisions.\n",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if !created.Enabled {
		t.Fatalf("expected created skill to be enabled")
	}

	enabled = false
	updated, err := runtime.Update("architecture-review", skillPatchRequest{Enabled: &enabled})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.Enabled {
		t.Fatalf("expected updated skill to be disabled")
	}

	catalog, err := runtime.Catalog()
	if err != nil {
		t.Fatalf("Catalog failed: %v", err)
	}
	if len(catalog.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(catalog.Skills))
	}
	if catalog.Skills[0].Enabled {
		t.Fatalf("expected catalog skill to stay disabled")
	}
}

func TestSkillCatalogCompose_CreatesStructuredSkill(t *testing.T) {
	runtime := newSkillCatalogRuntime(t.TempDir())

	created, err := runtime.Compose(skillComposeRequest{
		ID:               "handoff-review",
		Name:             "Handoff Review",
		Description:      "Prepare a repo handoff from live project state.",
		Tags:             []string{"handoff", "review"},
		Trigger:          "Use when a project needs a durable handoff document.",
		AssignedAgentIDs: []string{"AGT-project-manager-agent", "AGT-sage"},
		Inputs:           "Repo path\nTarget audience",
		Outputs:          "Markdown handoff\nValidation notes",
		Notes:            "Check live source before trusting old docs.",
	})
	if err != nil {
		t.Fatalf("Compose failed: %v", err)
	}
	if created.ID != "handoff-review" {
		t.Fatalf("expected id handoff-review, got %s", created.ID)
	}
	for _, want := range []string{"## When To Use", "## Intended Agents", "AGT-project-manager-agent", "## Expected Output"} {
		if !strings.Contains(created.Content, want) {
			t.Fatalf("composed content missing %q:\n%s", want, created.Content)
		}
	}
}
