package main

import (
	"context"
	"testing"

	sageagents "github.com/matta/sage-nexus/services/manager"
)

type testSessionStore struct {
	messages map[string][]sageagents.ChatMessage
}

func newTestSessionStore() *testSessionStore {
	return &testSessionStore{messages: map[string][]sageagents.ChatMessage{}}
}

func (s *testSessionStore) Append(_ context.Context, contextID string, msg sageagents.ChatMessage) {
	s.messages[contextID] = append(s.messages[contextID], msg)
}

func (s *testSessionStore) Load(_ context.Context, contextID string) []sageagents.ChatMessage {
	existing := s.messages[contextID]
	out := make([]sageagents.ChatMessage, len(existing))
	copy(out, existing)
	return out
}

func (s *testSessionStore) Trim(_ context.Context, contextID string, maxTurns int) {
	if maxTurns <= 0 {
		return
	}
	items := s.messages[contextID]
	if len(items) <= maxTurns {
		return
	}
	s.messages[contextID] = append([]sageagents.ChatMessage{}, items[len(items)-maxTurns:]...)
}

func TestShouldUseDispatchRollingContext(t *testing.T) {
	if got := shouldUseDispatchRollingContext(inboundTask{ContextID: "ctx-1", Source: "dispatch"}, false); !got {
		t.Fatalf("expected dispatch source to enable rolling context")
	}
	if got := shouldUseDispatchRollingContext(inboundTask{ContextID: "ctx-1", Source: ""}, false); !got {
		t.Fatalf("expected empty source to enable rolling context for compatibility")
	}
	if got := shouldUseDispatchRollingContext(inboundTask{ContextID: "ctx-1", Source: "local-chat"}, false); got {
		t.Fatalf("expected local-chat source to skip dispatch rolling context")
	}
	if got := shouldUseDispatchRollingContext(inboundTask{ContextID: "", Source: "dispatch"}, false); got {
		t.Fatalf("expected empty context to skip rolling context")
	}
	if got := shouldUseDispatchRollingContext(inboundTask{ContextID: "ctx-1", Source: "dispatch"}, true); got {
		t.Fatalf("expected Sage-routed tasks to skip dispatch rolling context")
	}
}

func TestBuildDispatchRollingInput(t *testing.T) {
	t.Setenv("MANAGER_DISPATCH_ROLLING_MAX_MESSAGES", "4")
	t.Setenv("MANAGER_DISPATCH_ROLLING_MAX_CHARS", "120")

	store := newTestSessionStore()
	store.Append(context.Background(), "ctx-1", sageagents.ChatMessage{Role: "user", Content: "first request"})
	store.Append(context.Background(), "ctx-1", sageagents.ChatMessage{Role: "assistant", Content: "first response"})
	store.Append(context.Background(), "ctx-1", sageagents.ChatMessage{Role: "user", Content: "second request"})
	store.Append(context.Background(), "ctx-1", sageagents.ChatMessage{Role: "assistant", Content: "second response"})

	got := buildDispatchRollingInput(context.Background(), store, "ctx-1", "latest request")
	if got == "latest request" {
		t.Fatalf("expected rolling context wrapper, got raw input")
	}
	if !containsAll(got, []string{
		"Recent rolling context for continuity:",
		"User: first request",
		"Manager: first response",
		"User: second request",
		"Manager: second response",
		"Current request:",
		"latest request",
	}) {
		t.Fatalf("rolling context body missing expected lines:\n%s", got)
	}
}

func TestPersistDispatchRollingTurn(t *testing.T) {
	t.Setenv("MANAGER_DISPATCH_ROLLING_MAX_MESSAGES", "3")
	store := newTestSessionStore()
	ctx := context.Background()

	persistDispatchRollingTurn(ctx, store, inboundTask{
		ContextID: "ctx-1",
		Source:    "dispatch",
	}, "request one", SageResponse{Status: "ok", Content: "reply one"}, false)

	persistDispatchRollingTurn(ctx, store, inboundTask{
		ContextID: "ctx-1",
		Source:    "dispatch",
	}, "request two", SageResponse{Status: "error", Error: "reply two error"}, false)

	items := store.Load(ctx, "ctx-1")
	if len(items) != 3 {
		t.Fatalf("expected trimmed rolling session size of 3, got %d", len(items))
	}
	if items[0].Role != "assistant" || items[0].Content != "reply one" {
		t.Fatalf("unexpected first kept message: %+v", items[0])
	}
	if items[1].Role != "user" || items[1].Content != "request two" {
		t.Fatalf("unexpected second kept message: %+v", items[1])
	}
	if items[2].Role != "assistant" || items[2].Content != "reply two error" {
		t.Fatalf("unexpected third kept message: %+v", items[2])
	}
}

func containsAll(text string, parts []string) bool {
	for _, part := range parts {
		if part == "" {
			continue
		}
		if !contains(text, part) {
			return false
		}
	}
	return true
}

func contains(text, part string) bool {
	return len(part) == 0 || (len(text) >= len(part) && indexOf(text, part) >= 0)
}

func indexOf(text, part string) int {
outer:
	for i := 0; i+len(part) <= len(text); i++ {
		for j := 0; j < len(part); j++ {
			if text[i+j] != part[j] {
				continue outer
			}
		}
		return i
	}
	return -1
}
