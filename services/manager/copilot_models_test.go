package sageagents

import (
	"reflect"
	"testing"
)

func TestParseCopilotModelIDs_ListShape(t *testing.T) {
	body := []byte(`[
		{"id":"gpt-4.1"},
		{"id":"o3-mini"},
		{"id":"gpt-4.1"}
	]`)
	got := parseCopilotModelIDs(body)
	want := []string{"gpt-4.1", "o3-mini"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected parsed IDs: got=%v want=%v", got, want)
	}
}

func TestParseCopilotModelIDs_DataShape(t *testing.T) {
	body := []byte(`{"data":[{"id":"claude-sonnet-4-5"},{"id":"gpt-4.1"}]}`)
	got := parseCopilotModelIDs(body)
	want := []string{"claude-sonnet-4-5", "gpt-4.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected parsed IDs: got=%v want=%v", got, want)
	}
}
