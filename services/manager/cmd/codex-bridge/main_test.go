package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	sageagents "github.com/matta/sage-nexus/services/manager"
)

func TestRunCodexUsesRequestedModel(t *testing.T) {
	fake, argsFile := writeFakeCodex(t, "ok from fake", 0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got, err := runCodex(ctx, fake, "gpt-5.5", "hello")
	if err != nil {
		t.Fatalf("runCodex failed: %v", err)
	}
	if got != "ok from fake" {
		t.Fatalf("response = %q", got)
	}
	args := readFile(t, argsFile)
	if !strings.Contains(args, "exec") || !strings.Contains(args, "--ignore-user-config") || !strings.Contains(args, "--model") || !strings.Contains(args, "gpt-5.5") {
		t.Fatalf("codex args did not include requested model: %q", args)
	}
}

func TestRunCodexFailureReportsSafeError(t *testing.T) {
	fake, _ := writeFakeCodex(t, "auth/model rejected", 7)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := runCodex(ctx, fake, "gpt-5.5", "hello")
	if err == nil {
		t.Fatal("expected failure")
	}
	if !strings.Contains(err.Error(), "gpt-5.5") || !strings.Contains(err.Error(), "auth/model rejected") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBridgeChatEndpoint(t *testing.T) {
	fake, _ := writeFakeCodex(t, "bridge reply", 0)
	srv := &bridgeServer{
		codexBin:     fake,
		defaultModel: sageagents.DefaultCodexModel,
		timeout:      5 * time.Second,
		probeTTL:     time.Minute,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/chat", srv.handleChat)

	body, _ := json.Marshal(sageagents.CodexBridgeChatRequest{
		Model:  sageagents.DefaultCodexModel,
		System: "system",
		Messages: []sageagents.ChatMessage{
			{Role: "user", Content: "hello"},
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(body))
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out sageagents.CodexBridgeChatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Text != "bridge reply" {
		t.Fatalf("text=%q", out.Text)
	}
}

func TestBuildCodexPromptIncludesRuntimeModelRoute(t *testing.T) {
	prompt := buildCodexPrompt("gpt-5.5", "", []sageagents.ChatMessage{
		{Role: "user", Content: "what model are you running?"},
	}, nil)
	if !strings.Contains(prompt, "Runtime model route: codex/gpt-5.5") {
		t.Fatalf("prompt missing runtime model route: %s", prompt)
	}
	if !strings.Contains(prompt, "including telling stories") {
		t.Fatalf("prompt missing casual/story allowance: %s", prompt)
	}
	if !strings.Contains(prompt, "not the technical router or orchestrator") {
		t.Fatalf("prompt missing persona/control-plane boundary: %s", prompt)
	}
	if !strings.Contains(prompt, "Sage Only is direct persona chat") {
		t.Fatalf("prompt missing Sage Only mode boundary: %s", prompt)
	}
}

func TestParseStructuredCodexOutputFinal(t *testing.T) {
	out, err := parseStructuredCodexOutput("gpt-5.5", `{"final_text":"done","tool_calls":[]}`)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if out.FinishReason != "final" || out.Text != "done" {
		t.Fatalf("unexpected output: %+v", out)
	}
}

func TestParseStructuredCodexOutputToolCalls(t *testing.T) {
	out, err := parseStructuredCodexOutput("gpt-5.5", `{"final_text":"","tool_calls":[{"id":"","name":"searxng_search","arguments_json":"{\"query\":\"codex\"}"}]}`)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if out.FinishReason != "tool_calls" || len(out.ToolCalls) != 1 {
		t.Fatalf("unexpected output: %+v", out)
	}
	if out.ToolCalls[0].Function.Name != "searxng_search" {
		t.Fatalf("wrong tool call: %+v", out.ToolCalls[0])
	}
	if !strings.Contains(out.ToolCalls[0].Function.Arguments, "codex") {
		t.Fatalf("wrong arguments: %s", out.ToolCalls[0].Function.Arguments)
	}
}

func writeFakeCodex(t *testing.T, output string, code int) (string, string) {
	t.Helper()
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	var path string
	var content string
	if runtime.GOOS == "windows" {
		path = filepath.Join(dir, "codex.cmd")
		if code == 0 {
			content = "@echo off\r\necho %* > \"%CODEX_FAKE_ARGS%\"\r\necho " + output + "\r\n"
		} else {
			content = "@echo off\r\necho %* > \"%CODEX_FAKE_ARGS%\"\r\necho " + output + " 1>&2\r\nexit /b " + strconv.Itoa(code) + "\r\n"
		}
	} else {
		path = filepath.Join(dir, "codex")
		if code == 0 {
			content = "#!/bin/sh\nprintf '%s\n' \"$*\" > \"$CODEX_FAKE_ARGS\"\nprintf '" + output + "\\n'\n"
		} else {
			content = "#!/bin/sh\nprintf '%s\n' \"$*\" > \"$CODEX_FAKE_ARGS\"\nprintf '" + output + "\\n' >&2\nexit " + strconv.Itoa(code) + "\n"
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	t.Setenv("CODEX_FAKE_ARGS", argsFile)
	return path, argsFile
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
