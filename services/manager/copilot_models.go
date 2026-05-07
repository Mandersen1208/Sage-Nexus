package sageagents

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// ListCopilotModels returns the model IDs currently available to the caller's
// Copilot entitlement. It uses the same short-lived Copilot bearer token flow
// as chat completions.
func ListCopilotModels(stateDir string) ([]string, error) {
	auth, err := GetCopilotRuntimeAuth(stateDir)
	if err != nil {
		return nil, fmt.Errorf("copilot token unavailable: %w", err)
	}

	req, err := http.NewRequest(http.MethodGet, normalizeCopilotAPIBaseURL(auth.BaseURL)+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+auth.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Editor-Version", "vscode/1.85.0")
	req.Header.Set("Editor-Plugin-Version", "copilot-chat/0.12.0")
	req.Header.Set("Openai-Intent", "conversation-panel")
	req.Header.Set("Copilot-Integration-Id", "vscode-chat")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	ids := parseCopilotModelIDs(body)
	if len(ids) == 0 {
		return nil, fmt.Errorf("copilot models response contained no model IDs")
	}
	return ids, nil
}

func parseCopilotModelIDs(body []byte) []string {
	ids := map[string]struct{}{}

	var list []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &list); err == nil {
		for _, item := range list {
			id := strings.TrimSpace(item.ID)
			if id != "" {
				ids[id] = struct{}{}
			}
		}
	}

	var wrapped struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil {
		for _, item := range wrapped.Data {
			id := strings.TrimSpace(item.ID)
			if id != "" {
				ids[id] = struct{}{}
			}
		}
	}

	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
