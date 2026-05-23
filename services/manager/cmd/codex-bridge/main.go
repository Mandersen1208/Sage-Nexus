package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sageagents "github.com/matta/sage-nexus/services/manager"
)

const bridgeVersion = "0.1.0"

type bridgeServer struct {
	codexBin     string
	defaultModel string
	timeout      time.Duration
	probeTTL     time.Duration

	mu          sync.Mutex
	lastProbeAt time.Time
	lastModel   string
	lastStatus  sageagents.CodexBridgeStatus
}

func main() {
	listenAddr := envOr("CODEX_BRIDGE_LISTEN_ADDR", "127.0.0.1:8765")
	srv := &bridgeServer{
		codexBin:     strings.TrimSpace(os.Getenv("CODEX_BIN")),
		defaultModel: envOr("CODEX_DEFAULT_MODEL", sageagents.DefaultCodexModel),
		timeout:      durationEnv("CODEX_EXEC_TIMEOUT_MS", 180000),
		probeTTL:     durationEnv("CODEX_HEALTH_PROBE_TTL_MS", 600000),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", srv.handleHealth)
	mux.HandleFunc("/chat", srv.handleChat)

	log.Printf("codex-bridge listening on %s (model=%s)", listenAddr, srv.defaultModel)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		log.Fatal(err)
	}
}

func (s *bridgeServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	model := strings.TrimSpace(r.URL.Query().Get("model"))
	if model == "" {
		model = s.defaultModel
	}
	status := s.health(r.Context(), model)
	writeJSON(w, http.StatusOK, status)
}

func (s *bridgeServer) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req sageagents.CodexBridgeChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = s.defaultModel
	}
	prompt := buildCodexPrompt(model, req.System, req.Messages, req.Tools)
	if strings.TrimSpace(prompt) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "messages are required"})
		return
	}
	start := time.Now()
	out := sageagents.CodexBridgeChatResponse{
		Model: model,
	}
	var err error
	if len(req.Tools) > 0 {
		out, err = s.runCodexStructured(r.Context(), model, prompt)
	} else {
		out.Text, err = s.runCodex(r.Context(), model, prompt)
		out.FinishReason = "final"
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, sageagents.CodexBridgeChatResponse{
			Model:      model,
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return
	}
	out.Model = model
	out.DurationMS = time.Since(start).Milliseconds()
	writeJSON(w, http.StatusOK, out)
}

func (s *bridgeServer) health(ctx context.Context, model string) sageagents.CodexBridgeStatus {
	base := sageagents.CodexBridgeStatus{
		Connected:     false,
		Model:         model,
		BridgeVersion: bridgeVersion,
	}
	path, err := s.resolveCodexPath()
	if err != nil {
		base.Error = err.Error()
		return base
	}
	base.CodexFound = true

	s.mu.Lock()
	if s.lastModel == model && !s.lastProbeAt.IsZero() && time.Since(s.lastProbeAt) < s.probeTTL {
		cached := s.lastStatus
		s.mu.Unlock()
		return cached
	}
	s.mu.Unlock()

	probeCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	_, err = runCodex(probeCtx, path, model, "Reply exactly: ok")
	if err != nil {
		base.Error = err.Error()
		base.ProbeOK = false
	} else {
		base.ProbeOK = true
		base.Connected = true
	}

	s.mu.Lock()
	s.lastModel = model
	s.lastProbeAt = time.Now()
	s.lastStatus = base
	s.mu.Unlock()
	return base
}

func (s *bridgeServer) runCodex(ctx context.Context, model, prompt string) (string, error) {
	path, err := s.resolveCodexPath()
	if err != nil {
		return "", err
	}
	runCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	return runCodex(runCtx, path, model, prompt)
}

func (s *bridgeServer) runCodexStructured(ctx context.Context, model, prompt string) (sageagents.CodexBridgeChatResponse, error) {
	path, err := s.resolveCodexPath()
	if err != nil {
		return sageagents.CodexBridgeChatResponse{}, err
	}
	schemaPath, err := writeOutputSchema()
	if err != nil {
		return sageagents.CodexBridgeChatResponse{}, err
	}
	defer func() {
		if err := os.Remove(schemaPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("temporary Codex schema cleanup failed: %v", err)
		}
	}()

	runCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	out, err := runCodexWithSchema(runCtx, path, model, prompt, schemaPath)
	if err != nil {
		return sageagents.CodexBridgeChatResponse{}, err
	}
	return parseStructuredCodexOutput(model, out)
}

func (s *bridgeServer) resolveCodexPath() (string, error) {
	bin := strings.TrimSpace(s.codexBin)
	if bin == "" {
		bin = "codex"
	}
	if strings.ContainsAny(bin, `\/`) {
		if _, err := os.Stat(bin); err != nil {
			return "", fmt.Errorf("codex binary not found at %s: %w", bin, err)
		}
		return bin, nil
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return "", fmt.Errorf("codex binary %q not found on PATH", bin)
	}
	return path, nil
}

func runCodex(ctx context.Context, bin, model, prompt string) (string, error) {
	return runCodexArgs(ctx, bin, model, prompt, nil)
}

func runCodexWithSchema(ctx context.Context, bin, model, prompt, schemaPath string) (string, error) {
	return runCodexArgs(ctx, bin, model, prompt, []string{"--output-schema", schemaPath})
}

func runCodexArgs(ctx context.Context, bin, model, prompt string, extra []string) (string, error) {
	args := []string{"exec", "--ignore-user-config", "--model", model, "--sandbox", "read-only"}
	args = append(args, extra...)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = strings.NewReader(prompt)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	errText := strings.TrimSpace(stderr.String())
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return out, fmt.Errorf("codex exec timed out for model %s", model)
	}
	if err != nil {
		detail := errText
		if detail == "" {
			detail = out
		}
		if detail == "" {
			detail = err.Error()
		}
		return out, fmt.Errorf("codex exec failed for model %s: %s", model, trim(detail, 800))
	}
	if out == "" {
		return "", fmt.Errorf("codex exec returned an empty response for model %s", model)
	}
	return out, nil
}

func buildCodexPrompt(model, system string, messages []sageagents.ChatMessage, tools []sageagents.ToolDefinition) string {
	var b strings.Builder
	b.WriteString("You are running as Sage, the front-of-house persona layer inside Sage Nexus.\n")
	b.WriteString("You are not the technical router or orchestrator. The manager/orchestrator is the control plane that routes technical work to specialist agents.\n")
	b.WriteString("Do not edit files or run project commands.\n")
	b.WriteString("Sage Only is direct persona chat with no self-imposed domain lane; Sage Auto is routed by the manager/orchestrator before it reaches specialist work.\n")
	b.WriteString("You are allowed to answer casual conversation, personality, and creative requests directly, including telling stories. Do not call storytelling outside your lane.\n")
	if len(tools) > 0 {
		b.WriteString("You may request manager-mediated tools. Do not claim you ran tools yourself. If a tool is needed, return tool_calls instead of final_text.\n")
		b.WriteString("When returning tool_calls, use the exact tool name and put the JSON object arguments in arguments_json. The manager will execute approved MCP/local tools and send results back.\n")
	} else {
		b.WriteString("Reply only with the assistant response text.\n")
	}
	if strings.TrimSpace(model) != "" {
		b.WriteString("Runtime model route: codex/")
		b.WriteString(strings.TrimSpace(model))
		b.WriteString(". If the user asks what model you are running, answer with this route.\n")
	}
	if strings.TrimSpace(system) != "" {
		b.WriteString("\nSystem prompt:\n")
		b.WriteString(strings.TrimSpace(system))
		b.WriteString("\n")
	}
	if len(tools) > 0 {
		b.WriteString("\nAvailable manager-mediated tools:\n")
		encoded, err := json.MarshalIndent(tools, "", "  ")
		if err != nil {
			log.Printf("Codex bridge tool prompt encoding failed: %v", err)
		} else {
			b.WriteString(string(encoded))
		}
		b.WriteString("\n")
		b.WriteString("\nResponse contract: return JSON matching the supplied schema. Use final_text for a final answer, or tool_calls when you need tool results.\n")
	}
	b.WriteString("\nConversation:\n")
	for _, msg := range messages {
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			role = "user"
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" && len(msg.ToolCalls) == 0 {
			continue
		}
		b.WriteString(strings.ToUpper(role[:1]))
		if len(role) > 1 {
			b.WriteString(role[1:])
		}
		b.WriteString(": ")
		if len(msg.ToolCalls) > 0 {
			for i, tc := range msg.ToolCalls {
				if i > 0 {
					b.WriteString("; ")
				}
				b.WriteString("requested tool ")
				b.WriteString(tc.Function.Name)
				b.WriteString(" args=")
				b.WriteString(tc.Function.Arguments)
			}
		} else {
			b.WriteString(content)
		}
		b.WriteString("\n")
	}
	b.WriteString("\nAssistant:")
	return b.String()
}

func writeOutputSchema() (string, error) {
	schema := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"final_text": map[string]interface{}{"type": "string"},
			"tool_calls": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]interface{}{
						"id":             map[string]interface{}{"type": "string"},
						"name":           map[string]interface{}{"type": "string"},
						"arguments_json": map[string]interface{}{"type": "string"},
					},
					"required": []string{"id", "name", "arguments_json"},
				},
			},
		},
		"required": []string{"final_text", "tool_calls"},
	}
	body, err := json.Marshal(schema)
	if err != nil {
		return "", err
	}
	path := filepath.Join(os.TempDir(), fmt.Sprintf("codex-tools-schema-%d.json", time.Now().UnixNano()))
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func parseStructuredCodexOutput(model, raw string) (sageagents.CodexBridgeChatResponse, error) {
	var decoded struct {
		FinalText string `json:"final_text"`
		ToolCalls []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			ArgumentsJSON string `json:"arguments_json"`
		} `json:"tool_calls"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &decoded); err != nil {
		return sageagents.CodexBridgeChatResponse{}, fmt.Errorf("codex structured output parse failed for model %s: %w", model, err)
	}
	out := sageagents.CodexBridgeChatResponse{
		Text:         strings.TrimSpace(decoded.FinalText),
		Model:        model,
		FinishReason: "final",
	}
	for i, call := range decoded.ToolCalls {
		name := strings.TrimSpace(call.Name)
		if name == "" {
			continue
		}
		args := strings.TrimSpace(call.ArgumentsJSON)
		if args == "" {
			args = "{}"
		}
		var argsObject map[string]interface{}
		if err := json.Unmarshal([]byte(args), &argsObject); err != nil {
			return sageagents.CodexBridgeChatResponse{}, fmt.Errorf("codex structured tool arguments_json parse failed for %s: %w", name, err)
		}
		id := strings.TrimSpace(call.ID)
		if id == "" {
			id = fmt.Sprintf("codex_tool_%d", i+1)
		}
		tc := sageagents.ToolCall{ID: id, Type: "function"}
		tc.Function.Name = name
		tc.Function.Arguments = args
		out.ToolCalls = append(out.ToolCalls, tc)
	}
	if len(out.ToolCalls) > 0 {
		out.FinishReason = "tool_calls"
		out.Text = ""
	}
	if out.Text == "" && len(out.ToolCalls) == 0 {
		return out, fmt.Errorf("codex structured output contained neither final_text nor tool_calls for model %s", model)
	}
	return out, nil
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("bridge JSON response write failed: %v", err)
	}
}

func envOr(key, def string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return def
}

func durationEnv(key string, defMS int) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return time.Duration(defMS) * time.Millisecond
	}
	var ms int
	if _, err := fmt.Sscanf(raw, "%d", &ms); err != nil || ms <= 0 {
		return time.Duration(defMS) * time.Millisecond
	}
	return time.Duration(ms) * time.Millisecond
}

func trim(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max] + "..."
}
