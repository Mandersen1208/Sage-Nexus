// acp_client.go — Admission Control Protocol (ACP) client.
//
// ACP governs which capabilities an agent is permitted to exercise. Before the
// manager dispatches work it must obtain an execution token (ET) by:
//  1. Fetching a one-time challenge nonce from the ACP server.
//  2. Signing a Proof-of-Possession (PoP) message with its Ed25519 private key.
//  3. Sending the capability request along with the signed PoP.
//
// If the server returns APPROVED, the manager proceeds and consumes the ET
// afterwards to close the audit trail. ESCALATE and DENY are treated as errors.
package sageagents

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ACPClient holds the identity and capability token (CT) needed to request
// admission from the ACP server before executing any controlled capability.
type ACPClient struct {
	// ServerURL is the base URL of the ACP server, e.g. "http://acp-server:8080".
	ServerURL string

	// AgentID is sent in X-ACP-Agent-ID headers so the server can look up the
	// agent's registered public key.
	AgentID string

	// PrivateKey is the agent's Ed25519 signing key, used to produce the PoP
	// signature on every admission request.
	PrivateKey ed25519.PrivateKey

	// CT is the capability token issued by the institution. It is passed as a
	// Bearer token and authorises the agent to request the declared capability.
	CT string
}

// challengeResponse is the body returned by GET /acp/v1/challenge.
type challengeResponse struct {
	// Challenge is a one-time nonce that must be included in the PoP signature.
	Challenge string `json:"challenge"`
	// ExpiresAt is the Unix timestamp after which the nonce is no longer valid.
	ExpiresAt int64 `json:"expires_at"`
}

// verifyRequest is the body posted to POST /acp/v1/verify.
type verifyRequest struct {
	// Capability is the ACP capability identifier, e.g. "acp:cap:skill.agent-delegate".
	Capability string `json:"capability"`
	// Resource is the target resource URI, e.g. "sage://workspace/*".
	Resource string `json:"resource"`
	// ActionParams carries arbitrary context the ACP policy engine may inspect.
	ActionParams map[string]interface{} `json:"action_params,omitempty"`
}

// etSummary holds the execution token ID returned on APPROVED decisions.
type etSummary struct {
	ETID string `json:"et_id"`
}

// verifyResponse is the body returned by POST /acp/v1/verify.
type verifyResponse struct {
	// Decision is one of "APPROVED", "ESCALATED", or "DENIED".
	Decision string `json:"decision"`
	// ExecutionToken is present when Decision == "APPROVED".
	ExecutionToken *etSummary `json:"execution_token,omitempty"`
	// Reason explains ESCALATED or DENIED decisions.
	Reason string `json:"reason,omitempty"`
	// ErrorCode is returned by ACP for structured denial causes.
	ErrorCode string `json:"error_code,omitempty"`
	// RiskScore and RiskLevel describe the policy decision when available.
	RiskScore int    `json:"risk_score,omitempty"`
	RiskLevel string `json:"risk_level,omitempty"`
}

// RequestAdmission runs the full ACP admission flow for the given capability
// and resource, returning the execution token ID on success.
//
// Flow:
//  1. GET /acp/v1/challenge — fetch a one-time nonce.
//  2. Compute PoP: METHOD|PATH|CHALLENGE|hex(SHA256(body)).
//  3. Sign PoP with the agent's Ed25519 private key (base64url-encoded).
//  4. POST /acp/v1/verify with the PoP in X-ACP-Signature and the nonce in
//     X-ACP-Challenge.
//
// Returns an error for ESCALATED, DENIED, or any transport/parse failure.
func (c *ACPClient) RequestAdmission(capability, resource string, actionParams map[string]interface{}) (string, error) {
	challenge, err := c.fetchChallenge()
	if err != nil {
		return "", fmt.Errorf("ACP challenge: %w", err)
	}

	body := verifyRequest{Capability: capability, Resource: resource, ActionParams: actionParams}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	// PoP message: METHOD|PATH|CHALLENGE|hex(SHA256(body))
	h := sha256.Sum256(bodyBytes)
	popMsg := strings.Join([]string{"POST", "/acp/v1/verify", challenge, fmt.Sprintf("%x", h)}, "|")
	sig := ed25519.Sign(c.PrivateKey, []byte(popMsg))
	popB64 := base64.RawURLEncoding.EncodeToString(sig)

	req, err := http.NewRequest(http.MethodPost, c.ServerURL+"/acp/v1/verify", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.CT)
	req.Header.Set("X-ACP-Agent-ID", c.AgentID)
	req.Header.Set("X-ACP-Signature", popB64)
	req.Header.Set("X-ACP-Challenge", challenge)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ACP verify: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ACP verify (%d): %s", resp.StatusCode, respBody)
	}

	var vr verifyResponse
	if err := json.Unmarshal(respBody, &vr); err != nil {
		return "", fmt.Errorf("ACP verify parse: %w", err)
	}

	switch vr.Decision {
	case "APPROVED":
		if vr.ExecutionToken != nil {
			return vr.ExecutionToken.ETID, nil
		}
		return "", nil
	case "ESCALATED":
		return "", fmt.Errorf("ACP escalation required: %s", vr.decisionMessage())
	case "DENIED":
		return "", fmt.Errorf("ACP denied: %s", vr.decisionMessage())
	default:
		return "", fmt.Errorf("unknown ACP decision: %s", vr.Decision)
	}
}

func (r verifyResponse) decisionMessage() string {
	parts := make([]string, 0, 3)
	if r.ErrorCode != "" {
		parts = append(parts, r.ErrorCode)
	}
	if r.Reason != "" {
		parts = append(parts, r.Reason)
	}
	if r.RiskLevel != "" || r.RiskScore != 0 {
		parts = append(parts, fmt.Sprintf("risk=%s/%d", r.RiskLevel, r.RiskScore))
	}
	if len(parts) == 0 {
		return "no reason returned"
	}
	return strings.Join(parts, "; ")
}

// ConsumeExecutionToken marks the execution token as used after the action
// completes. This closes the ACP audit trail for the request. Calling this is
// best-effort — a failure is logged but does not roll back the completed action.
func (c *ACPClient) ConsumeExecutionToken(etID string) error {
	url := fmt.Sprintf("%s/acp/v1/exec-tokens/%s/consume", c.ServerURL, etID)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-ACP-Agent-ID", c.AgentID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("consume ET (%d): %s", resp.StatusCode, b)
	}
	return nil
}

// fetchChallenge requests a one-time nonce from the ACP server.
// The nonce must be embedded in the PoP signature and included in the verify
// request header so the server can detect replay attacks.
func (c *ACPClient) fetchChallenge() (string, error) {
	req, err := http.NewRequest(http.MethodGet, c.ServerURL+"/acp/v1/challenge", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-ACP-Agent-ID", c.AgentID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("challenge (%d): %s", resp.StatusCode, b)
	}
	var cr challengeResponse
	if err := json.Unmarshal(b, &cr); err != nil {
		return "", err
	}
	return cr.Challenge, nil
}
