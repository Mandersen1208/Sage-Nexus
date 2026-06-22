package sageagents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	copilotDefaultModel = "gpt-4o"
)

// copilotHTTPClient bounds how long any single chat completion call can hang.
// Tool-calling loops can still take multiple rounds; this is per round.
var copilotHTTPClient = &http.Client{Timeout: 120 * time.Second}

// CopilotAgent wraps BaseAgent to call GitHub Copilot's chat completions API.
// It holds a short-lived API token and refreshes it automatically.
type CopilotAgent struct {
	BaseAgent

	// Model is the Copilot model identifier for completions.
	// Defaults to copilotDefaultModel ("gpt-4o") if empty.
	// Other options: "claude-sonnet-4-5", "o3-mini", "gemini-2.0-flash", etc.
	Model string

	// SystemPrompt is injected as the first system message on every chat call.
	// Loaded from the markdown file specified in agents.json at startup.
	SystemPrompt string

	// MCP is an optional client for the sage-mcp tool server.
	// When set, the model can call registry-approved MCP tools.
	MCP *MCPClient

	// AllowedTools constrains which MCP tool names are exposed to this worker.
	// Empty means no MCP tools are exposed.
	AllowedTools []string

	// ToolCatalog is discovered from MCP tools/list and then filtered through
	// AllowedTools. New MCP tools should not require Go schema edits.
	ToolCatalog []ToolDefinition

	// copilotToken is the short-lived bearer token for GitHub Copilot's runtime API.
	copilotToken   string
	tokenExpiresAt time.Time
	copilotBaseURL string
	stateDir       string
	codexBridgeURL string
	codexAllowed   bool

	// toolTrace records the MCP tool calls made during the last Chat() call.
	// Reset at the start of each Chat() invocation.
	toolTrace []string

	// toolErrors records only the tool calls that failed during the last Chat().
	// Empty if every call succeeded.
	toolErrors []ToolCallLog
}

// LastToolTrace returns the list of MCP tool names called during the most recent Chat().
func (a *CopilotAgent) LastToolTrace() []string {
	return a.toolTrace
}

// LastToolErrors returns the per-tool failures recorded during the most recent Chat().
// Empty when every tool call succeeded.
func (a *CopilotAgent) LastToolErrors() []ToolCallLog {
	return a.toolErrors
}

// ActiveModel returns the model that will be used for completions.
func (a *CopilotAgent) ActiveModel() string {
	if a.Model != "" {
		return a.Model
	}
	return copilotDefaultModel
}

// ChatMessage is a single turn in an OpenAI-format conversation.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// ToolCall is an OpenAI function/tool call issued by the assistant.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON string
	} `json:"function"`
}

type suppressedToolsKey struct{}

func WithSuppressedTools(ctx context.Context, names ...string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	set := map[string]struct{}{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			set[name] = struct{}{}
		}
	}
	if len(set) == 0 {
		return ctx
	}
	return context.WithValue(ctx, suppressedToolsKey{}, set)
}

func suppressedToolsFromContext(ctx context.Context) map[string]struct{} {
	if ctx == nil {
		return nil
	}
	set, _ := ctx.Value(suppressedToolsKey{}).(map[string]struct{})
	return set
}

// toolDef defines an OpenAI function tool for the model to call.
type toolDef struct {
	Type     string      `json:"type"` // "function"
	Function toolFuncDef `json:"function"`
}

// ToolDefinition is the public alias used by manager startup when it passes
// MCP-discovered tool schemas into workers.
type ToolDefinition = toolDef

type toolFuncDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

// chatRequest is the request body for the Copilot chat completions endpoint.
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Tools       []toolDef     `json:"tools,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

// chatResponse is the response from the Copilot chat completions endpoint.
type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

type chatChoice struct {
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// SetStateDir tells the agent where to find Sage's state directory so it can
// read and refresh the Copilot API token automatically.
func (a *CopilotAgent) SetStateDir(dir string) {
	a.stateDir = dir
}

func (a *CopilotAgent) SetCodexBridge(url string, allowed bool) {
	a.codexBridgeURL = strings.TrimSpace(url)
	a.codexAllowed = allowed
}

// Metadata returns the agent's registration info for the Manager heartbeat.
func (a *CopilotAgent) Metadata() AgentMetadata {
	return AgentMetadata{
		AgentID:      a.AgentID,
		Endpoint:     a.Endpoint,
		Capabilities: []string{"copilot-chat", "copilot-completion"},
		LastSeen:     time.Now().Unix(),
	}
}

// SendHeartbeat posts a heartbeat to the Manager.
func (a *CopilotAgent) SendHeartbeat(managerURL string) error {
	meta := a.Metadata()
	body, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	resp, err := http.Post(managerURL+"/heartbeat", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("heartbeat response body close failed: %v", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		b, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("heartbeat rejected (%d); response read failed: %w", resp.StatusCode, readErr)
		}
		return fmt.Errorf("heartbeat rejected (%d): %s", resp.StatusCode, b)
	}
	return nil
}

// UseCopilotModel sends a prompt to the Copilot chat completions API and
// returns the assistant's reply text.
func (a *CopilotAgent) UseCopilotModel(prompt string) (string, error) {
	return a.Chat(context.Background(), []ChatMessage{{Role: "user", Content: prompt}})
}

// Chat sends a full message history and returns the assistant reply.
// If a SystemPrompt is configured it is prepended as a system message.
// If an MCP client is attached, registry-approved MCP tools are exposed to the
// model and the tool-calling loop runs until the model returns a final text response.
func (a *CopilotAgent) Chat(ctx context.Context, messages []ChatMessage) (string, error) {
	// Prepend system prompt if set and not already present.
	if a.SystemPrompt != "" && (len(messages) == 0 || messages[0].Role != "system") {
		messages = append([]ChatMessage{{Role: "system", Content: a.SystemPrompt}}, messages...)
	}

	// Reset trace for this call.
	a.toolTrace = nil
	a.toolErrors = nil

	tools := []toolDef{}
	if a.MCP != nil {
		tools = a.availableTools(ctx)
	}

	pm := ParseProviderModel(a.ActiveModel())
	if pm.Provider == ProviderCodex {
		return a.chatCodexBridge(ctx, pm.Model, messages, tools)
	}

	maxRounds := envInt("SAGE_WORKER_MAX_ROUNDS", 12) // guard against infinite tool loops
	for round := 0; round < maxRounds; round++ {
		if encoded, err := json.Marshal(messages); err == nil {
			AppendWorkContextEvent(ctx, "model_input", a.AgentID, "Model input context snapshot", string(encoded), map[string]interface{}{
				"round":         round + 1,
				"model":         a.ActiveModel(),
				"message_count": len(messages),
				"tool_count":    len(tools),
			})
		}
		result, err := a.callCopilot(ctx, messages, tools)
		if err != nil {
			return "", err
		}
		if len(result.Choices) == 0 {
			return "", fmt.Errorf("no choices in response")
		}

		choice := result.Choices[0]

		// No tool calls — model gave a final answer.
		if choice.FinishReason != "tool_calls" || len(choice.Message.ToolCalls) == 0 {
			return choice.Message.Content, nil
		}

		// Append the assistant's tool-call message to history.
		messages = append(messages, choice.Message)

		// Execute each tool call against the MCP server and append results.
		for _, tc := range choice.Message.ToolCalls {
			a.toolTrace = append(a.toolTrace, tc.Function.Name)
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = map[string]interface{}{}
			}
			hadWorkContextArgs := hasWorkContextArgs(args)
			a.injectToolArgs(ctx, tc.Function.Name, args)
			if isAgentContextTool(tc.Function.Name) && !hadWorkContextArgs && hasWorkContextArgs(args) {
				log.Printf("    [%s] tool ↺ %s auto-injected work_context_id/token from runtime context", a.AgentID, tc.Function.Name)
			}
			start := time.Now()
			redactedArgs := RedactSecretsInString(tc.Function.Arguments)
			if marshaled, err := json.Marshal(args); err == nil {
				redactedArgs = RedactSecretsInString(string(marshaled))
			}
			if isAgentContextTool(tc.Function.Name) && !hasWorkContextArgs(args) {
				err := fmt.Errorf("%s requires active work context (work_context_id/token missing)", tc.Function.Name)
				dur := time.Since(start).Round(time.Millisecond)
				log.Printf("    [%s] tool ✗ %s (%s) err=%v", a.AgentID, tc.Function.Name, dur, err)
				EmitProgress(ctx, ProgressEvent{Type: "tool", Agent: a.AgentID, Tool: tc.Function.Name, Phase: "end", DurationMS: dur.Milliseconds(), Error: err.Error(), Timestamp: time.Now().Unix()})
				PublishError(ctx, ErrorEvent{Kind: "tool", Agent: a.AgentID, Tool: tc.Function.Name, Error: err.Error(), DurationMS: dur.Milliseconds()})
				a.toolErrors = append(a.toolErrors, ToolCallLog{Agent: a.AgentID, Tool: tc.Function.Name, DurationMS: dur.Milliseconds(), Error: err.Error()})
				AppendWorkContextEvent(ctx, "tool_end", a.AgentID, "MCP tool failed "+tc.Function.Name, err.Error(), map[string]interface{}{
					"tool":        tc.Function.Name,
					"duration_ms": dur.Milliseconds(),
				})
				messages = append(messages, ChatMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    fmt.Sprintf("error: %v", err),
				})
				continue
			}
			log.Printf("    [%s] tool → %s args=%s", a.AgentID, tc.Function.Name, truncate(redactedArgs, 120))
			AppendWorkContextEvent(ctx, "tool_start", a.AgentID, "Calling MCP tool "+tc.Function.Name, "", map[string]interface{}{
				"tool": tc.Function.Name,
				"args": redactedArgs,
			})
			EmitProgress(ctx, ProgressEvent{Type: "tool", Agent: a.AgentID, Tool: tc.Function.Name, Phase: "start", Timestamp: time.Now().Unix()})
			toolResult, err := a.MCP.CallTool(tc.Function.Name, args)
			EmitLatencySpan(ctx, "mcp_tool_"+tc.Function.Name, start)
			dur := time.Since(start).Round(time.Millisecond)
			if err != nil {
				log.Printf("    [%s] tool ✗ %s (%s) err=%v", a.AgentID, tc.Function.Name, dur, err)
				EmitProgress(ctx, ProgressEvent{Type: "tool", Agent: a.AgentID, Tool: tc.Function.Name, Phase: "end", DurationMS: dur.Milliseconds(), Error: err.Error(), Timestamp: time.Now().Unix()})
				PublishError(ctx, ErrorEvent{Kind: "tool", Agent: a.AgentID, Tool: tc.Function.Name, Error: err.Error(), DurationMS: dur.Milliseconds()})
				a.toolErrors = append(a.toolErrors, ToolCallLog{Agent: a.AgentID, Tool: tc.Function.Name, DurationMS: dur.Milliseconds(), Error: err.Error()})
				AppendWorkContextEvent(ctx, "tool_end", a.AgentID, "MCP tool failed "+tc.Function.Name, err.Error(), map[string]interface{}{
					"tool":        tc.Function.Name,
					"duration_ms": dur.Milliseconds(),
				})
				toolResult = fmt.Sprintf("error: %v", err)
			} else {
				log.Printf("    [%s] tool ✓ %s (%s) %dB", a.AgentID, tc.Function.Name, dur, len(toolResult))
				EmitProgress(ctx, ProgressEvent{Type: "tool", Agent: a.AgentID, Tool: tc.Function.Name, Phase: "end", DurationMS: dur.Milliseconds(), Timestamp: time.Now().Unix()})
				AppendWorkContextEvent(ctx, "tool_end", a.AgentID, "MCP tool completed "+tc.Function.Name, toolResult, map[string]interface{}{
					"tool":        tc.Function.Name,
					"duration_ms": dur.Milliseconds(),
				})
			}
			messages = append(messages, ChatMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    toolResult,
			})
		}
	}

	return "", fmt.Errorf("tool loop exceeded %d rounds", maxRounds)
}

func (a *CopilotAgent) availableTools(ctxs ...context.Context) []toolDef {
	if len(a.ToolCatalog) == 0 || len(a.AllowedTools) == 0 {
		return nil
	}

	allowed := make(map[string]struct{}, len(a.AllowedTools))
	for _, name := range a.AllowedTools {
		allowed[name] = struct{}{}
	}
	var suppressed map[string]struct{}
	if len(ctxs) > 0 {
		suppressed = suppressedToolsFromContext(ctxs[0])
	}

	filtered := make([]toolDef, 0, len(a.ToolCatalog))
	for _, tool := range a.ToolCatalog {
		if _, ok := suppressed[tool.Function.Name]; ok {
			continue
		}
		if _, ok := allowed[tool.Function.Name]; ok {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

func (a *CopilotAgent) chatCodexBridge(ctx context.Context, model string, messages []ChatMessage, tools []toolDef) (string, error) {
	if !a.codexAllowed {
		return "", fmt.Errorf("codex provider is not enabled for %s", a.AgentID)
	}
	bridgeURL := strings.TrimSpace(a.codexBridgeURL)
	if bridgeURL == "" {
		return "", fmt.Errorf("CODEX_BRIDGE_URL is not configured")
	}
	start := time.Now()
	system := ""
	filtered := make([]ChatMessage, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "system" && system == "" {
			system = msg.Content
			continue
		}
		filtered = append(filtered, msg)
	}
	client := NewCodexBridgeClient(bridgeURL)

	if len(tools) == 0 {
		reply, err := client.Chat(ctx, CodexBridgeChatRequest{
			Model:    model,
			System:   system,
			Messages: filtered,
		})
		EmitLatencySpan(ctx, "codex_bridge_chat", start)
		if err != nil {
			return "", err
		}
		return reply, nil
	}

	maxRounds := envInt("SAGE_WORKER_MAX_ROUNDS", 12)
	for round := 0; round < maxRounds; round++ {
		if encoded, err := json.Marshal(filtered); err == nil {
			AppendWorkContextEvent(ctx, "model_input", a.AgentID, "Codex model input context snapshot", string(encoded), map[string]interface{}{
				"round":         round + 1,
				"model":         ProviderCodex + "/" + model,
				"message_count": len(filtered),
				"tool_count":    len(tools),
			})
		}
		resp, err := client.ChatResponse(ctx, CodexBridgeChatRequest{
			Model:    model,
			System:   system,
			Messages: filtered,
			Tools:    tools,
		})
		EmitLatencySpan(ctx, "codex_bridge_chat", start)
		if err != nil {
			return "", err
		}
		if resp.FinishReason != "tool_calls" || len(resp.ToolCalls) == 0 {
			return resp.Text, nil
		}
		filtered = append(filtered, ChatMessage{Role: "assistant", ToolCalls: resp.ToolCalls})
		for _, tc := range resp.ToolCalls {
			filtered = append(filtered, a.executeMCPToolCall(ctx, tc))
		}
	}
	return "", fmt.Errorf("tool loop exceeded %d rounds", maxRounds)
}

func (a *CopilotAgent) executeMCPToolCall(ctx context.Context, tc ToolCall) ChatMessage {
	a.toolTrace = append(a.toolTrace, tc.Function.Name)
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		args = map[string]interface{}{}
	}
	hadWorkContextArgs := hasWorkContextArgs(args)
	a.injectToolArgs(ctx, tc.Function.Name, args)
	if isAgentContextTool(tc.Function.Name) && !hadWorkContextArgs && hasWorkContextArgs(args) {
		log.Printf("    [%s] tool injected %s work_context_id/token from runtime context", a.AgentID, tc.Function.Name)
	}
	start := time.Now()
	redactedArgs := RedactSecretsInString(tc.Function.Arguments)
	if marshaled, err := json.Marshal(args); err == nil {
		redactedArgs = RedactSecretsInString(string(marshaled))
	}
	if isAgentContextTool(tc.Function.Name) && !hasWorkContextArgs(args) {
		err := fmt.Errorf("%s requires active work context (work_context_id/token missing)", tc.Function.Name)
		dur := time.Since(start).Round(time.Millisecond)
		log.Printf("    [%s] tool failed %s (%s) err=%v", a.AgentID, tc.Function.Name, dur, err)
		EmitProgress(ctx, ProgressEvent{Type: "tool", Agent: a.AgentID, Tool: tc.Function.Name, Phase: "end", DurationMS: dur.Milliseconds(), Error: err.Error(), Timestamp: time.Now().Unix()})
		PublishError(ctx, ErrorEvent{Kind: "tool", Agent: a.AgentID, Tool: tc.Function.Name, Error: err.Error(), DurationMS: dur.Milliseconds()})
		a.toolErrors = append(a.toolErrors, ToolCallLog{Agent: a.AgentID, Tool: tc.Function.Name, DurationMS: dur.Milliseconds(), Error: err.Error()})
		AppendWorkContextEvent(ctx, "tool_end", a.AgentID, "MCP tool failed "+tc.Function.Name, err.Error(), map[string]interface{}{
			"tool":        tc.Function.Name,
			"duration_ms": dur.Milliseconds(),
		})
		return ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: fmt.Sprintf("error: %v", err)}
	}
	log.Printf("    [%s] tool -> %s args=%s", a.AgentID, tc.Function.Name, truncate(redactedArgs, 120))
	AppendWorkContextEvent(ctx, "tool_start", a.AgentID, "Calling MCP tool "+tc.Function.Name, "", map[string]interface{}{
		"tool": tc.Function.Name,
		"args": redactedArgs,
	})
	EmitProgress(ctx, ProgressEvent{Type: "tool", Agent: a.AgentID, Tool: tc.Function.Name, Phase: "start", Timestamp: time.Now().Unix()})
	toolResult, err := a.MCP.CallTool(tc.Function.Name, args)
	EmitLatencySpan(ctx, "mcp_tool_"+tc.Function.Name, start)
	dur := time.Since(start).Round(time.Millisecond)
	if err != nil {
		log.Printf("    [%s] tool failed %s (%s) err=%v", a.AgentID, tc.Function.Name, dur, err)
		EmitProgress(ctx, ProgressEvent{Type: "tool", Agent: a.AgentID, Tool: tc.Function.Name, Phase: "end", DurationMS: dur.Milliseconds(), Error: err.Error(), Timestamp: time.Now().Unix()})
		PublishError(ctx, ErrorEvent{Kind: "tool", Agent: a.AgentID, Tool: tc.Function.Name, Error: err.Error(), DurationMS: dur.Milliseconds()})
		a.toolErrors = append(a.toolErrors, ToolCallLog{Agent: a.AgentID, Tool: tc.Function.Name, DurationMS: dur.Milliseconds(), Error: err.Error()})
		AppendWorkContextEvent(ctx, "tool_end", a.AgentID, "MCP tool failed "+tc.Function.Name, err.Error(), map[string]interface{}{
			"tool":        tc.Function.Name,
			"duration_ms": dur.Milliseconds(),
		})
		toolResult = fmt.Sprintf("error: %v", err)
	} else {
		log.Printf("    [%s] tool ok %s (%s) %dB", a.AgentID, tc.Function.Name, dur, len(toolResult))
		EmitProgress(ctx, ProgressEvent{Type: "tool", Agent: a.AgentID, Tool: tc.Function.Name, Phase: "end", DurationMS: dur.Milliseconds(), Timestamp: time.Now().Unix()})
		AppendWorkContextEvent(ctx, "tool_end", a.AgentID, "MCP tool completed "+tc.Function.Name, toolResult, map[string]interface{}{
			"tool":        tc.Function.Name,
			"duration_ms": dur.Milliseconds(),
		})
	}
	return ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: toolResult}
}

func (a *CopilotAgent) injectPeerCallArgs(ctx context.Context, args map[string]interface{}) {
	if args == nil {
		return
	}
	args["caller_agent_id"] = a.AgentID
	args["depth"] = PeerCallDepthFromContext(ctx)
	if reason, _ := args["reason"].(string); strings.TrimSpace(reason) == "" {
		args["reason"] = "peer consultation requested by " + a.AgentID
	}
	if access, ok := WorkContextFromContext(ctx); ok {
		args["work_context_id"] = access.ID
		args["token"] = access.Token
	}
}

func (a *CopilotAgent) injectHandoffArgs(ctx context.Context, args map[string]interface{}) {
	if args == nil {
		return
	}
	args["caller_agent_id"] = a.AgentID
	args["depth"] = PeerCallDepthFromContext(ctx)
	if access, ok := WorkContextFromContext(ctx); ok {
		if current, _ := args["task_id"].(string); strings.TrimSpace(current) == "" {
			args["task_id"] = access.TaskID
		}
		if current, _ := args["work_context_id"].(string); strings.TrimSpace(current) == "" {
			args["work_context_id"] = access.ID
		}
		if current, _ := args["token"].(string); strings.TrimSpace(current) == "" {
			args["token"] = access.Token
		}
	}
}

func (a *CopilotAgent) injectToolArgs(ctx context.Context, toolName string, args map[string]interface{}) {
	if args == nil {
		return
	}
	switch toolName {
	case "call_agent":
		a.injectPeerCallArgs(ctx, args)
	case "handoff_to_agent", "complete_task":
		a.injectHandoffArgs(ctx, args)
	case "agent_context_read", "agent_context_append", "agent_context_search":
		a.injectWorkContextArgs(ctx, args)
	}
}

func (a *CopilotAgent) injectWorkContextArgs(ctx context.Context, args map[string]interface{}) {
	if args == nil {
		return
	}
	access, ok := WorkContextFromContext(ctx)
	if !ok {
		return
	}
	if current, _ := args["work_context_id"].(string); strings.TrimSpace(current) == "" {
		args["work_context_id"] = access.ID
	}
	if current, _ := args["token"].(string); strings.TrimSpace(current) == "" {
		args["token"] = access.Token
	}
}

func isAgentContextTool(name string) bool {
	switch name {
	case "agent_context_read", "agent_context_append", "agent_context_search":
		return true
	default:
		return false
	}
}

func hasWorkContextArgs(args map[string]interface{}) bool {
	if args == nil {
		return false
	}
	id, _ := args["work_context_id"].(string)
	token, _ := args["token"].(string)
	return strings.TrimSpace(id) != "" && strings.TrimSpace(token) != ""
}

// ChatWithLocalTools runs the tool-call loop with the given tool definitions,
// routing tool invocations to the provided exec function instead of MCP.
// Used by the orchestrator to dispatch to in-process worker agents.
// The system prompt (if set) is prepended; toolTrace is reset and recorded.
func (a *CopilotAgent) ChatWithLocalTools(
	ctx context.Context,
	messages []ChatMessage,
	tools []toolDef,
	exec func(name string, args map[string]interface{}) (string, error),
) (string, error) {
	if a.SystemPrompt != "" && (len(messages) == 0 || messages[0].Role != "system") {
		messages = append([]ChatMessage{{Role: "system", Content: a.SystemPrompt}}, messages...)
	}
	a.toolTrace = nil

	pm := ParseProviderModel(a.ActiveModel())
	if pm.Provider == ProviderCodex {
		return a.chatCodexBridgeWithLocalTools(ctx, pm.Model, messages, tools, exec)
	}

	maxRounds := envInt("SAGE_ORCH_MAX_ROUNDS", 12)
	for round := 0; round < maxRounds; round++ {
		result, err := a.callCopilot(ctx, messages, tools)
		if err != nil {
			return "", err
		}
		if len(result.Choices) == 0 {
			return "", fmt.Errorf("no choices in response")
		}
		choice := result.Choices[0]
		if choice.FinishReason != "tool_calls" || len(choice.Message.ToolCalls) == 0 {
			return choice.Message.Content, nil
		}
		messages = append(messages, choice.Message)
		for _, tc := range choice.Message.ToolCalls {
			a.toolTrace = append(a.toolTrace, tc.Function.Name)
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = map[string]interface{}{}
			}
			start := time.Now()
			log.Printf("  [%s] route → %s args=%s", a.AgentID, tc.Function.Name, truncate(tc.Function.Arguments, 120))
			EmitProgress(ctx, ProgressEvent{Type: "route", Agent: a.AgentID, Tool: tc.Function.Name, Phase: "start", Timestamp: time.Now().Unix()})
			toolResult, err := exec(tc.Function.Name, args)
			EmitLatencySpan(ctx, "local_tool_"+tc.Function.Name, start)
			dur := time.Since(start).Round(time.Millisecond)
			if err != nil {
				log.Printf("  [%s] route ✗ %s (%s) err=%v", a.AgentID, tc.Function.Name, dur, err)
				EmitProgress(ctx, ProgressEvent{Type: "route", Agent: a.AgentID, Tool: tc.Function.Name, Phase: "end", DurationMS: dur.Milliseconds(), Error: err.Error(), Timestamp: time.Now().Unix()})
				PublishError(ctx, ErrorEvent{Kind: "route", Agent: a.AgentID, Tool: tc.Function.Name, Error: err.Error(), DurationMS: dur.Milliseconds()})
				toolResult = fmt.Sprintf("error: %v", err)
			} else {
				log.Printf("  [%s] route ✓ %s (%s) %dB", a.AgentID, tc.Function.Name, dur, len(toolResult))
				EmitProgress(ctx, ProgressEvent{Type: "route", Agent: a.AgentID, Tool: tc.Function.Name, Phase: "end", DurationMS: dur.Milliseconds(), Timestamp: time.Now().Unix()})
			}
			messages = append(messages, ChatMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    toolResult,
			})
		}
	}
	// Cap reached. Don't fail outright — return a sentinel the manager
	// recognizes so it can publish input-required and pause for the human.
	// The messages slice carries the full conversation so far so the manager
	// can resume the loop without losing tool-call history.
	return "", &RoundCapReachedError{Rounds: maxRounds, Messages: messages}
}

func (a *CopilotAgent) chatCodexBridgeWithLocalTools(
	ctx context.Context,
	model string,
	messages []ChatMessage,
	tools []toolDef,
	exec func(name string, args map[string]interface{}) (string, error),
) (string, error) {
	if !a.codexAllowed {
		return "", fmt.Errorf("codex provider is not enabled for %s", a.AgentID)
	}
	bridgeURL := strings.TrimSpace(a.codexBridgeURL)
	if bridgeURL == "" {
		return "", fmt.Errorf("CODEX_BRIDGE_URL is not configured")
	}
	system := ""
	filtered := make([]ChatMessage, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "system" && system == "" {
			system = msg.Content
			continue
		}
		filtered = append(filtered, msg)
	}
	client := NewCodexBridgeClient(bridgeURL)
	maxRounds := envInt("SAGE_ORCH_MAX_ROUNDS", 12)
	for round := 0; round < maxRounds; round++ {
		start := time.Now()
		resp, err := client.ChatResponse(ctx, CodexBridgeChatRequest{
			Model:    model,
			System:   system,
			Messages: filtered,
			Tools:    tools,
		})
		EmitLatencySpan(ctx, "codex_bridge_local_tools", start)
		if err != nil {
			return "", err
		}
		if resp.FinishReason != "tool_calls" || len(resp.ToolCalls) == 0 {
			return resp.Text, nil
		}
		filtered = append(filtered, ChatMessage{Role: "assistant", ToolCalls: resp.ToolCalls})
		for _, tc := range resp.ToolCalls {
			a.toolTrace = append(a.toolTrace, tc.Function.Name)
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = map[string]interface{}{}
			}
			start := time.Now()
			log.Printf("  [%s] route -> %s args=%s", a.AgentID, tc.Function.Name, truncate(tc.Function.Arguments, 120))
			EmitProgress(ctx, ProgressEvent{Type: "route", Agent: a.AgentID, Tool: tc.Function.Name, Phase: "start", Timestamp: time.Now().Unix()})
			toolResult, err := exec(tc.Function.Name, args)
			EmitLatencySpan(ctx, "local_tool_"+tc.Function.Name, start)
			dur := time.Since(start).Round(time.Millisecond)
			if err != nil {
				log.Printf("  [%s] route failed %s (%s) err=%v", a.AgentID, tc.Function.Name, dur, err)
				EmitProgress(ctx, ProgressEvent{Type: "route", Agent: a.AgentID, Tool: tc.Function.Name, Phase: "end", DurationMS: dur.Milliseconds(), Error: err.Error(), Timestamp: time.Now().Unix()})
				PublishError(ctx, ErrorEvent{Kind: "route", Agent: a.AgentID, Tool: tc.Function.Name, Error: err.Error(), DurationMS: dur.Milliseconds()})
				toolResult = fmt.Sprintf("error: %v", err)
			} else {
				log.Printf("  [%s] route ok %s (%s) %dB", a.AgentID, tc.Function.Name, dur, len(toolResult))
				EmitProgress(ctx, ProgressEvent{Type: "route", Agent: a.AgentID, Tool: tc.Function.Name, Phase: "end", DurationMS: dur.Milliseconds(), Timestamp: time.Now().Unix()})
			}
			filtered = append(filtered, ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: toolResult})
		}
	}
	return "", &RoundCapReachedError{Rounds: maxRounds, Messages: filtered}
}

// RoundCapReachedError signals the orchestrator hit its per-task tool-call
// cap. The manager catches this, publishes input-required on the bus (with
// the partial last reply), and waits on ChannelControl for the human's
// decision before resuming or finalizing.
//
// Messages carries the full conversation at the moment of the cap so a
// Resume call can pick up exactly where this one left off.
type RoundCapReachedError struct {
	Rounds   int
	Messages []ChatMessage
}

func (e *RoundCapReachedError) Error() string {
	return fmt.Sprintf("local tool loop reached cap of %d rounds — awaiting confirmation", e.Rounds)
}

// truncate shortens a string for log lines.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// callCopilot makes a single HTTP call to the Copilot completions endpoint.
func (a *CopilotAgent) callCopilot(ctx context.Context, messages []ChatMessage, tools []toolDef) (*chatResponse, error) {
	pm := ParseProviderModel(a.ActiveModel())
	if pm.Provider == ProviderCodex {
		return nil, fmt.Errorf("codex model %s cannot run on the Copilot chat path", a.ActiveModel())
	}
	tokenStart := time.Now()
	token, err := a.getToken()
	EmitLatencySpan(ctx, "provider_token", tokenStart)
	if err != nil {
		return nil, fmt.Errorf("could not obtain Copilot token: %w", err)
	}

	payload := chatRequest{
		Model:    pm.Model,
		Messages: messages,
		Tools:    tools,
	}
	log.Printf("[copilot-chat-call] %s using model: %s", a.AgentID, payload.Model)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	doRequest := func(tok string) (*http.Response, []byte, error) {
		endpoint := normalizeCopilotAPIBaseURL(a.copilotBaseURL) + "/chat/completions"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBuffer(body))
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Editor-Version", "vscode/1.85.0")
		req.Header.Set("Editor-Plugin-Version", "copilot-chat/0.12.0")
		req.Header.Set("Openai-Intent", "conversation-panel")
		req.Header.Set("Copilot-Integration-Id", "vscode-chat")
		resp, err := copilotHTTPClient.Do(req)
		if err != nil {
			return nil, nil, err
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				log.Printf("Copilot chat response body close failed: %v", err)
			}
		}()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return resp, nil, err
		}
		return resp, b, nil
	}

	httpStart := time.Now()
	resp, respBody, err := doRequest(token)
	EmitLatencySpan(ctx, "provider_chat_http", httpStart)
	if err != nil {
		return nil, err
	}

	// Token expired — refresh and retry once.
	if resp.StatusCode == http.StatusUnauthorized {
		a.copilotToken = ""
		a.tokenExpiresAt = time.Time{}
		a.copilotBaseURL = ""
		token, err = a.getToken()
		if err != nil {
			return nil, fmt.Errorf("token refresh after 401 failed: %w", err)
		}
		httpStart = time.Now()
		resp, respBody, err = doRequest(token)
		EmitLatencySpan(ctx, "provider_chat_http_retry", httpStart)
		if err != nil {
			return nil, err
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Copilot API error (%d): %s", resp.StatusCode, respBody)
	}

	var result chatResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("Copilot error [%s]: %s", result.Error.Type, result.Error.Message)
	}
	return &result, nil
}

// getToken returns a valid Copilot API token, refreshing if needed.
func (a *CopilotAgent) getToken() (string, error) {
	// Use in-memory token if still valid.
	if a.copilotToken != "" && time.Until(a.tokenExpiresAt) > 5*time.Minute {
		return a.copilotToken, nil
	}

	// Prefer Sage's cached Copilot token. Keep an env/default fallback here so
	// direct/solo agent paths still use the standalone auth store even if a
	// caller forgets to explicitly wire SetStateDir.
	stateDir := strings.TrimSpace(a.stateDir)
	if stateDir == "" {
		stateDir = GetEnvOr("SAGE_STATE_DIR", "/sage-state")
	}
	if stateDir != "" {
		auth, err := GetCopilotRuntimeAuth(stateDir)
		if err == nil {
			a.copilotToken = auth.Token
			a.tokenExpiresAt = auth.ExpiresAt
			a.copilotBaseURL = auth.BaseURL
			return auth.Token, nil
		}
		if a.OAuthToken == "" {
			return "", err
		}
	}

	// Fall back to any token that was set directly (e.g. via env var).
	if a.OAuthToken != "" {
		auth, err := refreshCopilotToken(a.OAuthToken)
		if err != nil {
			return "", err
		}
		a.copilotToken = auth.Token
		a.tokenExpiresAt = auth.ExpiresAt
		a.copilotBaseURL = auth.BaseURL
		return auth.Token, nil
	}

	return "", fmt.Errorf("no Copilot token source available (set SAGE_STATE_DIR or OAuthToken)")
}
