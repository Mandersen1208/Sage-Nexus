// mcp_client.go — JSON-RPC 2.0 client for the sage-mcp HTTP server.
//
// The Model Context Protocol (MCP) server exposes a set of tools (skill_list,
// skill_search, skill_get, web_search, delegate_to_manager) over a stateless
// HTTP endpoint. MCPClient wraps the JSON-RPC call/response cycle so that
// CopilotAgent can invoke these tools without dealing with wire-format details.
package sageagents

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// mcpHTTPClient is a dedicated HTTP client with a timeout so MCP tool calls
// cannot hang forever (e.g. if SearXNG stalls). The underlying SearXNG search
// itself has its own 15s timeout inside the MCP server, so 60s here leaves
// headroom for skill reads and the model-driven tool loop.
var mcpHTTPClient = &http.Client{Timeout: 60 * time.Second}

// MCPClient calls the sage-mcp HTTP endpoint via JSON-RPC 2.0.
// Create one per component (not per call) — the client maintains a monotonically
// increasing request ID counter for correlation.
type MCPClient struct {
	// Endpoint is the full URL of the MCP tool endpoint,
	// e.g. "http://sage-mcp:3030/mcp".
	Endpoint string

	// nextID is an auto-incrementing JSON-RPC request identifier.
	// Not thread-safe; each agent should own its own MCPClient instance.
	nextID int
}

// mcpRequest is the JSON-RPC 2.0 request envelope sent to the MCP server.
type mcpRequest struct {
	JSONRPC string      `json:"jsonrpc"` // always "2.0"
	ID      int         `json:"id"`
	Method  string      `json:"method"` // e.g. "tools/call"
	Params  interface{} `json:"params"`
}

// mcpResponse is the JSON-RPC 2.0 response envelope from the MCP server.
type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"` // present on success
	Error   *mcpError       `json:"error,omitempty"`  // present on failure
}

// mcpError is the JSON-RPC error object returned by the MCP server on failure.
type mcpError struct {
	Code    int    `json:"code"`    // JSON-RPC error code
	Message string `json:"message"` // human-readable error description
}

// ListTools discovers MCP tool schemas from the server at runtime. The manager
// uses this as the source of truth for model tool definitions so adding an MCP
// tool does not require editing Go schema maps.
func (c *MCPClient) ListTools() ([]ToolDefinition, error) {
	c.nextID++
	req := mcpRequest{
		JSONRPC: "2.0",
		ID:      c.nextID,
		Method:  "tools/list",
		Params:  map[string]interface{}{},
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequest(http.MethodPost, c.Endpoint, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := mcpHTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("MCP HTTP error: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var mcpResp mcpResponse
	if err := json.Unmarshal(raw, &mcpResp); err != nil {
		return nil, fmt.Errorf("MCP parse error: %w", err)
	}
	if mcpResp.Error != nil {
		return nil, fmt.Errorf("MCP tools/list error: %s", mcpResp.Error.Message)
	}

	var result struct {
		Tools []struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			InputSchema map[string]interface{} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(mcpResp.Result, &result); err != nil {
		return nil, fmt.Errorf("MCP tools/list result parse error: %w", err)
	}
	tools := make([]ToolDefinition, 0, len(result.Tools))
	for _, tool := range result.Tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		schema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		if len(tool.InputSchema) > 0 {
			schema = tool.InputSchema
		}
		tools = append(tools, toolDef{
			Type: "function",
			Function: toolFuncDef{
				Name:        name,
				Description: strings.TrimSpace(tool.Description),
				Parameters:  schema,
			},
		})
	}
	return tools, nil
}

// CallTool invokes a named MCP tool with the given arguments map and returns
// the first text content block from the response as a plain string.
//
// The MCP tools/call method wraps results in a content array:
//
//	{ "content": [{ "type": "text", "text": "..." }], "isError": false }
//
// CallTool extracts the first text block. If the tool returns isError=true the
// error text is returned as a Go error. If the result cannot be parsed as a
// content array, the raw JSON is returned as a string (graceful degradation).
func (c *MCPClient) CallTool(name string, args map[string]interface{}) (string, error) {
	c.nextID++
	req := mcpRequest{
		JSONRPC: "2.0",
		ID:      c.nextID,
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      name,
			"arguments": args,
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequest(http.MethodPost, c.Endpoint, bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// Accept both JSON and SSE — the MCP server may respond with either.
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := mcpHTTPClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("MCP HTTP error: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var mcpResp mcpResponse
	if err := json.Unmarshal(raw, &mcpResp); err != nil {
		return "", fmt.Errorf("MCP parse error: %w", err)
	}
	if mcpResp.Error != nil {
		return "", fmt.Errorf("MCP tool error: %s", mcpResp.Error.Message)
	}

	// Decode the MCP content array and return the first text block.
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(mcpResp.Result, &result); err != nil {
		// Not a content-array response — return raw JSON as string.
		return string(mcpResp.Result), nil
	}
	if result.IsError && len(result.Content) > 0 {
		return "", fmt.Errorf("MCP tool returned error: %s", result.Content[0].Text)
	}
	for _, block := range result.Content {
		if block.Type == "text" && block.Text != "" {
			return block.Text, nil
		}
	}
	// No text block found — fall back to raw JSON.
	return string(mcpResp.Result), nil
}
