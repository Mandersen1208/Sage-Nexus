package sageagents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type CodexBridgeStatus struct {
	Connected     bool   `json:"connected"`
	BridgeURL     string `json:"bridgeUrl,omitempty"`
	Model         string `json:"model,omitempty"`
	CodexFound    bool   `json:"codexFound"`
	ProbeOK       bool   `json:"probeOk"`
	Error         string `json:"error,omitempty"`
	BridgeVersion string `json:"bridgeVersion,omitempty"`
}

type CodexBridgeChatRequest struct {
	Model    string        `json:"model"`
	System   string        `json:"system,omitempty"`
	Messages []ChatMessage `json:"messages"`
	Tools    []toolDef     `json:"tools,omitempty"`
}

type CodexBridgeChatResponse struct {
	Text         string     `json:"text"`
	Model        string     `json:"model,omitempty"`
	DurationMS   int64      `json:"durationMs,omitempty"`
	FinishReason string     `json:"finishReason,omitempty"`
	ToolCalls    []ToolCall `json:"toolCalls,omitempty"`
	Error        string     `json:"error,omitempty"`
}

type CodexBridgeClient struct {
	BaseURL string
	Client  *http.Client
}

func NewCodexBridgeClient(baseURL string) *CodexBridgeClient {
	return &CodexBridgeClient{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Client:  &http.Client{Timeout: 180 * time.Second},
	}
}

func (c *CodexBridgeClient) Status(ctx context.Context, model string) CodexBridgeStatus {
	status := CodexBridgeStatus{BridgeURL: strings.TrimSpace(c.baseURL()), Model: strings.TrimSpace(model)}
	if status.BridgeURL == "" {
		status.Error = "CODEX_BRIDGE_URL is not configured"
		return status
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, status.BridgeURL+"/health?model="+urlQueryEscape(model), nil)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		status.Error = fmt.Sprintf("codex bridge returned %d: %s", resp.StatusCode, trimForError(body))
		return status
	}
	if err := json.Unmarshal(body, &status); err != nil {
		status.Error = "decode bridge status: " + err.Error()
		return status
	}
	status.BridgeURL = c.baseURL()
	status.Connected = status.CodexFound && status.ProbeOK && status.Error == ""
	return status
}

func (c *CodexBridgeClient) Chat(ctx context.Context, reqBody CodexBridgeChatRequest) (string, error) {
	out, err := c.ChatResponse(ctx, reqBody)
	if err != nil {
		return "", err
	}
	return out.Text, nil
}

func (c *CodexBridgeClient) ChatResponse(ctx context.Context, reqBody CodexBridgeChatRequest) (CodexBridgeChatResponse, error) {
	baseURL := c.baseURL()
	if baseURL == "" {
		return CodexBridgeChatResponse{}, fmt.Errorf("CODEX_BRIDGE_URL is not configured")
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return CodexBridgeChatResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat", bytes.NewReader(payload))
	if err != nil {
		return CodexBridgeChatResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return CodexBridgeChatResponse{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return CodexBridgeChatResponse{}, fmt.Errorf("codex bridge returned %d: %s", resp.StatusCode, trimForError(body))
	}
	var out CodexBridgeChatResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return CodexBridgeChatResponse{}, err
	}
	if strings.TrimSpace(out.Error) != "" {
		return CodexBridgeChatResponse{}, errors.New(out.Error)
	}
	return out, nil
}

func (c *CodexBridgeClient) baseURL() string {
	if c == nil {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
}

func (c *CodexBridgeClient) httpClient() *http.Client {
	if c != nil && c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: 180 * time.Second}
}

func urlQueryEscape(value string) string {
	return url.QueryEscape(strings.TrimSpace(value))
}
