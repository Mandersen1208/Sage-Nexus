package sageagents

import (
	"context"
	"testing"

	"github.com/go-redis/redis/v8"
)

func TestInjectToolArgs_AgentContextUsesWorkContextFromContext(t *testing.T) {
	a := &CopilotAgent{BaseAgent: BaseAgent{AgentID: "AGT-test"}}
	ctx := WithWorkContext(context.Background(), &WorkContextStore{Client: &redis.Client{}}, WorkContextAccess{
		ID:    "wc-123",
		Token: "wct-1234567890abcdefghijklmnopqrstuvwxyz",
	})
	args := map[string]interface{}{
		"kind":    "finding",
		"summary": "note",
	}

	a.injectToolArgs(ctx, "agent_context_append", args)

	if got, _ := args["work_context_id"].(string); got != "wc-123" {
		t.Fatalf("work_context_id not injected, got %q", got)
	}
	if got, _ := args["token"].(string); got == "" {
		t.Fatalf("token not injected")
	}
	if !hasWorkContextArgs(args) {
		t.Fatalf("expected hasWorkContextArgs to be true")
	}
}

func TestInjectToolArgs_AgentContextDoesNotOverrideProvidedValues(t *testing.T) {
	a := &CopilotAgent{BaseAgent: BaseAgent{AgentID: "AGT-test"}}
	ctx := WithWorkContext(context.Background(), &WorkContextStore{Client: &redis.Client{}}, WorkContextAccess{
		ID:    "wc-new",
		Token: "wct-new",
	})
	args := map[string]interface{}{
		"work_context_id": "wc-existing",
		"token":           "wct-existing",
	}

	a.injectToolArgs(ctx, "agent_context_read", args)

	if got, _ := args["work_context_id"].(string); got != "wc-existing" {
		t.Fatalf("work_context_id was overwritten, got %q", got)
	}
	if got, _ := args["token"].(string); got != "wct-existing" {
		t.Fatalf("token was overwritten, got %q", got)
	}
}

func TestInjectToolArgs_CallAgentIncludesCallerAndDepth(t *testing.T) {
	a := &CopilotAgent{BaseAgent: BaseAgent{AgentID: "AGT-caller"}}
	ctx := WithPeerCallDepth(context.Background(), 2)
	args := map[string]interface{}{}

	a.injectToolArgs(ctx, "call_agent", args)

	if got, _ := args["caller_agent_id"].(string); got != "AGT-caller" {
		t.Fatalf("caller_agent_id not injected, got %q", got)
	}
	if got, ok := args["depth"].(int); !ok || got != 2 {
		t.Fatalf("depth not injected, got %#v", args["depth"])
	}
}
