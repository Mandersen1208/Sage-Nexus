package main

import "time"

// AutonomyLevel represents the agent's supervision requirement.
type AutonomyLevel string

const (
	AutonomyL1 AutonomyLevel = "L1" // human must approve every execution
	AutonomyL2 AutonomyLevel = "L2" // autonomous within policy, human notified on escalation
	AutonomyL3 AutonomyLevel = "L3" // fully autonomous within capability scope
)

// AgentStatus represents whether an agent is operational.
type AgentStatus string

const (
	AgentStatusActive    AgentStatus = "ACTIVE"
	AgentStatusSuspended AgentStatus = "SUSPENDED"
	AgentStatusRevoked   AgentStatus = "REVOKED"
)

// Decision is the outcome of an admission check.
type Decision string

const (
	DecisionApproved  Decision = "APPROVED"
	DecisionEscalated Decision = "ESCALATED"
	DecisionDenied    Decision = "DENIED"
)

// Agent is the registered identity of a skill-agent in the system.
// Corresponds to ACP-AGENT-1.0: A = (ID, C, P, D, L, S)
type Agent struct {
	AgentID       string        `json:"agent_id"`
	PublicKey     string        `json:"public_key"` // base64url Ed25519 public key
	AutonomyLevel AutonomyLevel `json:"autonomy_level"`
	Status        AgentStatus   `json:"status"`
	RegisteredAt  time.Time     `json:"registered_at"`
}

// CTHeader is the header portion of a Capability Token.
type CTHeader struct {
	Alg string `json:"alg"` // "Ed25519"
	Typ string `json:"typ"` // "ACP-CT-1.0"
}

// CTPayload is the claims portion of a Capability Token.
type CTPayload struct {
	Subject    string   `json:"sub"`      // agent ID this CT is issued to
	Issuer     string   `json:"iss"`      // "institution"
	IssuedAt   int64    `json:"iat"`      // Unix epoch
	ExpiresAt  int64    `json:"exp"`      // Unix epoch
	CTID       string   `json:"jti"`      // unique CT identifier
	Capability []string `json:"cap"`      // list of acp:cap:skill.* grants
	Resource   string   `json:"resource"` // resource scope pattern
}

// PolicySnapshot holds the active admission thresholds.
type PolicySnapshot struct {
	ID              string    `json:"id"`
	Status          string    `json:"status"` // "ACTIVE" or "INACTIVE"
	ApproveBelow    int       `json:"approve_below"`
	EscalateBelow   int       `json:"escalate_below"`
	DenyAtOrAbove   int       `json:"deny_at_or_above"`
	CooldownDenials int       `json:"cooldown_trigger_after_denials"`
	CooldownSeconds int       `json:"cooldown_duration_seconds"`
	CreatedAt       time.Time `json:"created_at"`
}

// Challenge is a one-time nonce issued to an agent before a PoP submission.
type Challenge struct {
	Nonce     string    `json:"challenge"`
	AgentID   string    `json:"agent_id"`
	ExpiresAt time.Time `json:"expires_at"`
	Used      bool      `json:"-"`
}

// ExecutionToken is a single-use token issued after an APPROVED decision.
type ExecutionToken struct {
	ETID       string     `json:"et_id"`
	AgentID    string     `json:"agent_id"`
	Capability string     `json:"capability"`
	Resource   string     `json:"resource"`
	IssuedAt   time.Time  `json:"issued_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	Consumed   bool       `json:"consumed"`
	ConsumedAt *time.Time `json:"consumed_at,omitempty"`
}

// FrequencyRecord tracks call frequency for rate-based risk scoring.
type FrequencyRecord struct {
	AgentID    string
	Capability string
	Resource   string
	Timestamps []time.Time
}

// DenialRecord tracks consecutive denials per agent for cooldown enforcement.
type DenialRecord struct {
	AgentID     string
	Count       int
	CooldownEnd *time.Time
}

// --- HTTP request/response types ---

type RegisterAgentRequest struct {
	AgentID       string        `json:"agent_id"`
	PublicKey     string        `json:"public_key"`
	AutonomyLevel AutonomyLevel `json:"autonomy_level"`
	Status        AgentStatus   `json:"status"`
}

type IssueTokenRequest struct {
	Subject    string   `json:"sub"`
	Capability []string `json:"cap"`
	ExpiresAt  int64    `json:"exp"`
	Resource   string   `json:"resource"`
}

type IssueTokenResponse struct {
	Token string `json:"token"`
	CTID  string `json:"ct_id"`
}

type CreatePolicyRequest struct {
	ApproveBelow    int `json:"approve_below"`
	EscalateBelow   int `json:"escalate_below"`
	DenyAtOrAbove   int `json:"deny_at_or_above"`
	CooldownDenials int `json:"cooldown_trigger_after_denials"`
	CooldownSeconds int `json:"cooldown_duration_seconds"`
}

type ChallengeResponse struct {
	Challenge string `json:"challenge"`
	ExpiresAt int64  `json:"expires_at"`
}

type VerifyRequest struct {
	Capability       string                 `json:"capability"`
	Resource         string                 `json:"resource"`
	ActionParameters map[string]interface{} `json:"action_parameters"`
}

type ACPDecision struct {
	Decision       Decision   `json:"decision"`
	RiskScore      int        `json:"risk_score"`
	RiskLevel      string     `json:"risk_level"`
	ExecutionToken *ETSummary `json:"execution_token,omitempty"`
	ErrorCode      string     `json:"error_code,omitempty"`
	EscalationID   string     `json:"escalation_id,omitempty"`
}

type ETSummary struct {
	ETID      string `json:"et_id"`
	ExpiresAt int64  `json:"expires_at"`
}

type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}
