// base_agent.go — shared identity, ACP registration, and MCP connectivity
// for all Sage agent types.
//
// Every agent in the system embeds BaseAgent to get:
//   - A stable Ed25519 keypair (persisted to disk across container restarts).
//   - Registration with the ACP admission server.
//   - A liveness check against the sage-mcp tool server.
package sageagents

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// AgentMetadata holds registration and heartbeat info for an agent.
// It is sent to the Manager on each heartbeat so the Manager can track which
// agents are alive and what capabilities they offer.
type AgentMetadata struct {
	// AgentID is the unique identifier for this agent, e.g. "AGT-research-agent".
	AgentID string `json:"agent_id"`
	// Endpoint is the URL the Manager can POST tool invocations to.
	Endpoint string `json:"endpoint"`
	// Capabilities lists the tool/skill names this agent supports,
	// used by Manager.SelectAgent to pick the best agent for a task.
	Capabilities []string `json:"capabilities"`
	// LastSeen is the Unix timestamp of the most recent heartbeat.
	LastSeen int64 `json:"last_seen"`
}

// BaseAgent provides shared identity, ACP registration, and MCP connectivity
// for all Sage agent types. Embed this struct in any agent implementation.
type BaseAgent struct {
	// AgentID is the globally unique agent identifier, e.g. "AGT-sage-orchestrator".
	AgentID string

	// OAuthToken is the long-lived GitHub OAuth token used as a fallback to
	// refresh the short-lived Copilot API token when Sage's auth cache is stale.
	OAuthToken string

	// RefreshToken is reserved for future OAuth refresh flows.
	RefreshToken string

	// ACPEndpoint is the base URL of the ACP admission server,
	// e.g. "http://acp-server:8080".
	ACPEndpoint string

	// MCPEndpoint is the base URL of the sage-mcp tool server,
	// e.g. "http://sage-mcp:3030". ConnectToMCP() probes /health on this URL.
	MCPEndpoint string

	// CallbackURL is reserved for future async callback flows.
	CallbackURL string

	// Endpoint is this agent's own HTTP invoke URL, registered with the Manager
	// so the Manager can POST tasks directly to this agent.
	Endpoint string

	// Ed25519 identity — populated by GenerateIdentity or LoadOrGenerateIdentity.
	// The private key is used for ACP Proof-of-Possession signatures.
	// The public key is registered with the ACP server on startup.
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

// GenerateIdentity creates a fresh Ed25519 keypair and stores it in the agent.
// Use LoadOrGenerateIdentity instead when persistence across restarts matters.
func (a *BaseAgent) GenerateIdentity() error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("keygen failed: %w", err)
	}
	a.PublicKey = pub
	a.PrivateKey = priv
	return nil
}

// LoadOrGenerateIdentity loads a persisted Ed25519 keypair from keyFile, or
// generates a new one and saves it if the file does not exist yet.
//
// Persisting the keypair is important because the ACP server associates
// capability grants with a specific public key. A new key on every restart
// would require re-registration and could invalidate in-flight capability tokens.
func (a *BaseAgent) LoadOrGenerateIdentity(keyFile string) error {
	if data, err := os.ReadFile(keyFile); err == nil {
		if keyBytes, ok := parsePrivateKeyFile(data); ok {
			a.PrivateKey = ed25519.PrivateKey(keyBytes)
			a.PublicKey = a.PrivateKey.Public().(ed25519.PublicKey)
			return nil
		}
		return fmt.Errorf("identity file %s is not a valid Ed25519 private key", keyFile)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("could not read identity file %s: %w", keyFile, err)
	}

	// Key file missing or invalid — generate a new keypair.
	if err := a.GenerateIdentity(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(keyFile), 0o700); err != nil {
		return fmt.Errorf("could not create key dir: %w", err)
	}
	return os.WriteFile(keyFile, []byte(a.PrivateKey), 0o600)
}

func parsePrivateKeyFile(data []byte) ([]byte, bool) {
	if len(data) == ed25519.PrivateKeySize {
		return data, true
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, false
	}

	decoded, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil || len(decoded) != ed25519.PrivateKeySize {
		return nil, false
	}

	return decoded, true
}

// PublicKeyBase64 returns the agent's Ed25519 public key as a base64url string
// (32 raw bytes, no padding). This is the format the ACP server expects in
// the public_key field of POST /acp/v1/agents.
func (a *BaseAgent) PublicKeyBase64() string {
	return base64.RawURLEncoding.EncodeToString(a.PublicKey)
}

// RegisterWithACP registers this agent with the ACP admission server.
// The agent's public key and autonomy level are stored server-side so that
// subsequent RequestAdmission calls can verify the PoP signature.
//
// autonomyLevel controls the policy tier applied to this agent's capability
// requests (e.g. "L2" for standard delegated execution).
//
// HTTP 409 Conflict is treated as success — the agent was already registered.
func (a *BaseAgent) RegisterWithACP(autonomyLevel string) error {
	if a.ACPEndpoint == "" {
		return fmt.Errorf("ACPEndpoint is not set")
	}
	if len(a.PublicKey) == 0 {
		return fmt.Errorf("no identity key — call GenerateIdentity first")
	}

	payload := map[string]interface{}{
		"agent_id":       a.AgentID,
		"public_key":     a.PublicKeyBase64(),
		"autonomy_level": autonomyLevel,
		"status":         "ACTIVE",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, a.ACPEndpoint+"/acp/v1/agents", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("ACP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		return nil // already registered — idempotent
	}
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ACP registration failed (%d): %s", resp.StatusCode, b)
	}
	return nil
}

// ConnectToMCP verifies that the sage-mcp server is reachable by
// probing its /health endpoint. This is called at startup and before the first
// tool call so failures surface early with a clear error message.
func (a *BaseAgent) ConnectToMCP() error {
	if a.MCPEndpoint == "" {
		return fmt.Errorf("MCPEndpoint is not set")
	}

	resp, err := http.Get(a.MCPEndpoint + "/health")
	if err != nil {
		return fmt.Errorf("MCP server unreachable at %s: %w", a.MCPEndpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("MCP health check returned %d", resp.StatusCode)
	}
	return nil
}

// RefreshOAuthToken refreshes the OAuth token when expired.
// TODO: implement token refresh against the GitHub OAuth endpoint.
func (a *BaseAgent) RefreshOAuthToken() error {
	return nil
}
