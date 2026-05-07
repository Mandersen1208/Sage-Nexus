package sageagents

import (
	"context"
	"strings"
	"testing"
)

func TestWorkContextTokenHashRedactsRawToken(t *testing.T) {
	token := "wct-0123456789abcdef0123456789abcdef0123456789abcdef"
	hash := WorkContextTokenHash(token)
	if hash == "" || hash == token {
		t.Fatalf("hash should not expose raw token")
	}
	if hash != WorkContextTokenHash(token) {
		t.Fatalf("hash should be deterministic")
	}
	if hash == WorkContextTokenHash(token+"x") {
		t.Fatalf("different tokens should not share a hash")
	}
}

func TestWorkContextRedaction(t *testing.T) {
	redacted := RedactWorkContextValue(map[string]interface{}{
		"apiKey":     "sk-this-should-not-leak-01234567890123456789",
		"capability": "acp:cap:skill.agent-delegate",
		"nested": map[string]interface{}{
			"normal": "Bearer abc123def456ghi789",
			"note":   "safe",
		},
	}).(map[string]interface{})

	if redacted["apiKey"] != "[REDACTED]" {
		t.Fatalf("apiKey not redacted: %#v", redacted["apiKey"])
	}
	if redacted["capability"] != "acp:cap:skill.agent-delegate" {
		t.Fatalf("capability should not be redacted: %#v", redacted["capability"])
	}
	nested := redacted["nested"].(map[string]interface{})
	if strings.Contains(nested["normal"].(string), "abc123") {
		t.Fatalf("bearer token leaked: %s", nested["normal"])
	}
	if nested["note"] != "safe" {
		t.Fatalf("normal value should be preserved")
	}
}

func TestWorkerDispatchMessagesIncludeWorkContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), workContextKey{}, workContextRuntime{
		Store: &WorkContextStore{},
		Access: WorkContextAccess{
			ID:    "wc-test",
			Token: "wct-secret-token",
		},
	})
	messages := workerDispatchMessages(&CopilotAgent{SystemPrompt: "worker rules"}, ctx, "inspect runtime")
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(messages))
	}
	if messages[0].Role != "system" || !strings.Contains(messages[0].Content, "worker rules") || !strings.Contains(messages[0].Content, "wc-test") {
		t.Fatalf("system context missing: %#v", messages[0])
	}
	if messages[1].Content != "inspect runtime" {
		t.Fatalf("query changed: %q", messages[1].Content)
	}
}

func TestSplitContentForWorkContext_NoLoss(t *testing.T) {
	input := strings.Repeat("context-payload-", 200)
	chunks := splitContentForWorkContext(input, 128)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		if len(chunk) == 0 {
			t.Fatalf("chunk %d is empty", i)
		}
		if len(chunk) > 128 {
			t.Fatalf("chunk %d too large: %d", i, len(chunk))
		}
	}
	if got := strings.Join(chunks, ""); got != input {
		t.Fatalf("chunk join mismatch")
	}
}

func TestSplitContentForWorkContext_UnicodeNoLoss(t *testing.T) {
	input := strings.Repeat("naive cafe - ", 30) + strings.Repeat("delta-", 30)
	chunks := splitContentForWorkContext(input, 37)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	if got := strings.Join(chunks, ""); got != input {
		t.Fatalf("unicode chunk join mismatch")
	}
}

func TestCloneWorkContextMetadata_NoMutation(t *testing.T) {
	meta := map[string]interface{}{"a": 1, "b": "x"}
	clone := cloneWorkContextMetadata(meta)
	clone["a"] = 2
	if meta["a"].(int) != 1 {
		t.Fatalf("original metadata was mutated")
	}
}
