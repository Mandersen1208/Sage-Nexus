package sageagents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCodexWorkerUsesManagerMediatedMCPTool(t *testing.T) {
	bridgeCalls := 0
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bridgeCalls++
		var req CodexBridgeChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode bridge request: %v", err)
		}
		if req.Model != DefaultCodexModel {
			t.Fatalf("bridge model = %q", req.Model)
		}
		if len(req.Tools) != 1 || req.Tools[0].Function.Name != "demo_tool" {
			t.Fatalf("bridge tools = %+v", req.Tools)
		}
		w.Header().Set("Content-Type", "application/json")
		if bridgeCalls == 1 {
			_ = json.NewEncoder(w).Encode(CodexBridgeChatResponse{
				FinishReason: "tool_calls",
				ToolCalls: []ToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "demo_tool", Arguments: `{"query":"hello"}`},
				}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(CodexBridgeChatResponse{FinishReason: "final", Text: "final from codex"})
	}))
	defer bridge.Close()

	mcpCalls := 0
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mcpCalls++
		var req mcpRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode mcp request: %v", err)
		}
		if req.Method != "tools/call" {
			t.Fatalf("mcp method = %q", req.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": map[string]interface{}{
				"content": []map[string]string{{"type": "text", "text": "tool result"}},
			},
		})
	}))
	defer mcp.Close()

	agent := &CopilotAgent{
		BaseAgent:    BaseAgent{AgentID: "AGT-test-agent"},
		Model:        DefaultCodexModelRef,
		MCP:          &MCPClient{Endpoint: mcp.URL},
		AllowedTools: []string{"demo_tool"},
		ToolCatalog:  []ToolDefinition{toolFixture("demo_tool")},
	}
	agent.SetCodexBridge(bridge.URL, true)

	got, err := agent.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "use the tool"}})
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if got != "final from codex" {
		t.Fatalf("reply = %q", got)
	}
	if bridgeCalls != 2 || mcpCalls != 1 {
		t.Fatalf("calls bridge=%d mcp=%d", bridgeCalls, mcpCalls)
	}
	if trace := strings.Join(agent.LastToolTrace(), ","); trace != "demo_tool" {
		t.Fatalf("tool trace = %q", trace)
	}
}

func TestCodexLocalToolLoopRoutesThroughManagerExec(t *testing.T) {
	bridgeCalls := 0
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bridgeCalls++
		var req CodexBridgeChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode bridge request: %v", err)
		}
		if len(req.Tools) != 1 || req.Tools[0].Function.Name != "call_backend_dev_agent" {
			t.Fatalf("bridge tools = %+v", req.Tools)
		}
		w.Header().Set("Content-Type", "application/json")
		if bridgeCalls == 1 {
			_ = json.NewEncoder(w).Encode(CodexBridgeChatResponse{
				FinishReason: "tool_calls",
				ToolCalls: []ToolCall{{
					ID:   "route_1",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "call_backend_dev_agent", Arguments: `{"query":"fix api"}`},
				}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(CodexBridgeChatResponse{FinishReason: "final", Text: "routed final"})
	}))
	defer bridge.Close()

	agent := &CopilotAgent{BaseAgent: BaseAgent{AgentID: OrchestratorAgentID}, Model: DefaultCodexModelRef}
	agent.SetCodexBridge(bridge.URL, true)

	execCalls := 0
	got, err := agent.ChatWithLocalTools(
		context.Background(),
		[]ChatMessage{{Role: "user", Content: "route this"}},
		[]toolDef{toolFixture("call_backend_dev_agent")},
		func(name string, args map[string]interface{}) (string, error) {
			execCalls++
			if name != "call_backend_dev_agent" {
				t.Fatalf("exec name = %q", name)
			}
			return "worker result", nil
		},
	)
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if got != "routed final" || execCalls != 1 || bridgeCalls != 2 {
		t.Fatalf("reply=%q execCalls=%d bridgeCalls=%d", got, execCalls, bridgeCalls)
	}
}
