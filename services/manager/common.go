// common.go — shared types and utilities used across all agent packages.
package sageagents

import (
	"os"
	"strconv"
	"strings"
)

// ToolInvokeRequest is the payload sent over HTTP or Redis when asking an agent
// to execute a named tool. The Tool field selects which handler runs; Prompt
// carries the raw input text; RequestID is an optional correlation identifier
// used to match responses in async pub/sub flows.
type ToolInvokeRequest struct {
	Tool      string `json:"tool"`
	Prompt    string `json:"prompt"`
	RequestID string `json:"request_id,omitempty"`
}

// ToolInvokeResponse is the standard reply for a tool invocation.
// Exactly one of Result or Error will be non-empty on success/failure.
type ToolInvokeResponse struct {
	Result string `json:"result"`
	Error  string `json:"error,omitempty"`
}

// GetEnvOr returns the value of the named environment variable, or def if the
// variable is unset or empty. Used throughout the codebase to apply sensible
// defaults while remaining configurable via the environment.
func GetEnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envInt reads an integer env var with a default. Invalid values fall back
// to def with no error — these are operational tuning knobs, not contracts.
func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// envBool reads a boolean env var with a default.
func envBool(key string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}
