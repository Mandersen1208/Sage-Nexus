package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Server holds all dependencies for the ACP HTTP handlers.
type Server struct {
	store          *Store
	institutionPub ed25519.PublicKey
	institutionPrv ed25519.PrivateKey
}

func NewServer(store *Store, pub ed25519.PublicKey, prv ed25519.PrivateKey) *Server {
	return &Server{store: store, institutionPub: pub, institutionPrv: prv}
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error_code": code, "message": message})
}

// --- GET /acp/v1/health ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{Status: "ok", Version: "1.0"})
}

// --- GET /acp/v1/challenge ---
// Requires header: X-ACP-Agent-ID

func (s *Server) handleChallenge(w http.ResponseWriter, r *http.Request) {
	agentID := r.Header.Get("X-ACP-Agent-ID")
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_AGENT_ID", "X-ACP-Agent-ID header required")
		return
	}
	agent, ok := s.store.GetAgent(agentID)
	if !ok {
		writeError(w, http.StatusNotFound, "AGENT_NOT_FOUND", fmt.Sprintf("agent %q is not registered", agentID))
		return
	}
	if agent.Status != AgentStatusActive {
		writeError(w, http.StatusForbidden, "AGENT_NOT_ACTIVE", fmt.Sprintf("agent %q status is %s", agentID, agent.Status))
		return
	}

	nonce := newUUID()
	exp := time.Now().Add(60 * time.Second)
	s.store.PutChallenge(&Challenge{
		Nonce:     nonce,
		AgentID:   agentID,
		ExpiresAt: exp,
	})
	writeJSON(w, http.StatusOK, ChallengeResponse{
		Challenge: nonce,
		ExpiresAt: exp.Unix(),
	})
}

// --- POST /acp/v1/verify ---
// Headers: Authorization: Bearer <CT>, X-ACP-Agent-ID, X-ACP-Challenge, X-ACP-Signature

func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
	agentID := r.Header.Get("X-ACP-Agent-ID")
	challenge := r.Header.Get("X-ACP-Challenge")
	sigHeader := r.Header.Get("X-ACP-Signature")
	authHeader := r.Header.Get("Authorization")

	if agentID == "" || challenge == "" || sigHeader == "" || authHeader == "" {
		writeError(w, http.StatusBadRequest, "MISSING_HEADERS",
			"X-ACP-Agent-ID, X-ACP-Challenge, X-ACP-Signature, Authorization all required")
		return
	}

	// 1. Look up registered agent
	agent, ok := s.store.GetAgent(agentID)
	if !ok {
		writeError(w, http.StatusNotFound, "AGENT_NOT_FOUND", "agent not registered")
		return
	}
	if agent.Status != AgentStatusActive {
		writeError(w, http.StatusForbidden, "AGENT_NOT_ACTIVE", "agent is not active")
		return
	}

	// 2. Check denial cooldown
	if s.store.IsInCooldown(agentID) {
		writeJSON(w, http.StatusOK, ACPDecision{
			Decision:  DecisionDenied,
			RiskScore: 100,
			RiskLevel: "HIGH",
			ErrorCode: "COOLDOWN_ACTIVE",
		})
		return
	}

	// 3. Read and hash request body
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB cap
	if err != nil {
		writeError(w, http.StatusBadRequest, "READ_BODY", "failed to read request body")
		return
	}
	sum := sha256.Sum256(body)
	bodyHashHex := fmt.Sprintf("%x", sum)

	// 4. Decode the PoP signature
	sig, err := base64.RawURLEncoding.DecodeString(sigHeader)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_SIGNATURE", "X-ACP-Signature must be base64url")
		return
	}

	// 5. Decode agent public key and verify PoP
	agentPubBytes, err := base64.RawURLEncoding.DecodeString(agent.PublicKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "AGENT_KEY_CORRUPT", "cannot decode agent public key")
		return
	}
	agentPub := ed25519.PublicKey(agentPubBytes)
	if err := VerifyPoP("POST", "/acp/v1/verify", challenge, bodyHashHex, sig, agentPub); err != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_POP", err.Error())
		return
	}

	// 6. Consume challenge (one-time use)
	ch, err := s.store.ConsumeChallenge(challenge)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_CHALLENGE", err.Error())
		return
	}
	if ch.AgentID != agentID {
		writeError(w, http.StatusUnauthorized, "CHALLENGE_AGENT_MISMATCH",
			"challenge was issued to a different agent")
		return
	}

	// 7. Parse and verify Capability Token
	ctRaw, err := ParseBearerToken(authHeader)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_AUTH", err.Error())
		return
	}
	ctPayload, err := VerifyCT(ctRaw, s.institutionPub)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_CT", err.Error())
		return
	}
	if ctPayload.Subject != agentID {
		writeError(w, http.StatusUnauthorized, "CT_SUBJECT_MISMATCH",
			"CT subject does not match X-ACP-Agent-ID")
		return
	}

	// 8. Parse verify request body
	var req VerifyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "cannot parse request body as JSON")
		return
	}
	if req.Capability == "" || req.Resource == "" {
		writeError(w, http.StatusBadRequest, "MISSING_FIELDS", "capability and resource are required")
		return
	}

	// 9. Capability check
	if !CTHasCap(ctPayload, req.Capability) {
		writeJSON(w, http.StatusOK, ACPDecision{
			Decision:  DecisionDenied,
			RiskScore: 100,
			RiskLevel: "HIGH",
			ErrorCode: "CAPABILITY_NOT_GRANTED",
		})
		return
	}

	// 10. Record call and score risk
	callCount := s.store.RecordCall(agentID, req.Capability, req.Resource, frequencyWindow)
	score := ScoreRisk(RiskInput{
		AgentID:    agentID,
		Capability: req.Capability,
		Resource:   req.Resource,
		Autonomy:   agent.AutonomyLevel,
		CallCount:  callCount,
	})
	level := RiskLevel(score)

	// 11. Apply policy
	policy := s.store.GetPolicy()
	decision := ApplyPolicy(score, policy)

	result := ACPDecision{
		Decision:  decision,
		RiskScore: score,
		RiskLevel: level,
	}

	switch decision {
	case DecisionApproved:
		etID := newUUID()
		exp := time.Now().Add(300 * time.Second)
		s.store.PutExecutionToken(&ExecutionToken{
			ETID:       etID,
			AgentID:    agentID,
			Capability: req.Capability,
			Resource:   req.Resource,
			IssuedAt:   time.Now(),
			ExpiresAt:  exp,
		})
		result.ExecutionToken = &ETSummary{
			ETID:      etID,
			ExpiresAt: exp.Unix(),
		}

	case DecisionEscalated:
		result.EscalationID = newUUID()

	case DecisionDenied:
		result.ErrorCode = "RISK_THRESHOLD_EXCEEDED"
		cooldownDur := 300 * time.Second
		cooldownTrigger := 3
		if policy != nil && policy.CooldownDenials > 0 {
			cooldownTrigger = policy.CooldownDenials
			cooldownDur = time.Duration(policy.CooldownSeconds) * time.Second
		}
		s.store.RecordDenial(agentID, cooldownDur, cooldownTrigger)
	}

	writeJSON(w, http.StatusOK, result)
}

// --- POST /acp/v1/agents ---

func (s *Server) handleRegisterAgent(w http.ResponseWriter, r *http.Request) {
	var req RegisterAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error())
		return
	}
	if req.AgentID == "" || req.PublicKey == "" {
		writeError(w, http.StatusBadRequest, "MISSING_FIELDS", "agent_id and public_key required")
		return
	}
	// Validate the public key is a decodable Ed25519 key
	pkBytes, err := base64.RawURLEncoding.DecodeString(req.PublicKey)
	if err != nil || len(pkBytes) != ed25519.PublicKeySize {
		writeError(w, http.StatusBadRequest, "INVALID_PUBLIC_KEY",
			"public_key must be base64url-encoded 32-byte Ed25519 public key")
		return
	}
	if req.AutonomyLevel == "" {
		req.AutonomyLevel = AutonomyL2
	}
	if req.Status == "" {
		req.Status = AgentStatusActive
	}
	agent := &Agent{
		AgentID:       req.AgentID,
		PublicKey:     req.PublicKey,
		AutonomyLevel: req.AutonomyLevel,
		Status:        req.Status,
		RegisteredAt:  time.Now(),
	}
	s.store.PutAgent(agent)
	writeJSON(w, http.StatusCreated, agent)
}

// --- GET /acp/v1/agents ---

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.ListAgents())
}

// --- POST /acp/v1/tokens ---

func (s *Server) handleIssueToken(w http.ResponseWriter, r *http.Request) {
	var req IssueTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error())
		return
	}
	if req.Subject == "" || len(req.Capability) == 0 || req.ExpiresAt == 0 {
		writeError(w, http.StatusBadRequest, "MISSING_FIELDS", "sub, cap, and exp are required")
		return
	}
	// Verify the subject is a registered agent
	if _, ok := s.store.GetAgent(req.Subject); !ok {
		writeError(w, http.StatusNotFound, "AGENT_NOT_FOUND",
			fmt.Sprintf("agent %q is not registered", req.Subject))
		return
	}
	ctID := newUUID()
	payload := CTPayload{
		Subject:    req.Subject,
		Issuer:     "institution",
		IssuedAt:   time.Now().Unix(),
		ExpiresAt:  req.ExpiresAt,
		CTID:       ctID,
		Capability: req.Capability,
		Resource:   req.Resource,
	}
	token, err := SignCT(payload, s.institutionPrv)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SIGN_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, IssueTokenResponse{Token: token, CTID: ctID})
}

// --- POST /acp/v1/policy-snapshots ---

func (s *Server) handleCreatePolicy(w http.ResponseWriter, r *http.Request) {
	var req CreatePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error())
		return
	}
	if req.ApproveBelow == 0 && req.EscalateBelow == 0 && req.DenyAtOrAbove == 0 {
		writeError(w, http.StatusBadRequest, "MISSING_FIELDS",
			"approve_below, escalate_below, deny_at_or_above are required")
		return
	}
	if req.CooldownDenials == 0 {
		req.CooldownDenials = 3
	}
	if req.CooldownSeconds == 0 {
		req.CooldownSeconds = 300
	}
	snap := &PolicySnapshot{
		ID:              newUUID(),
		Status:          "ACTIVE",
		ApproveBelow:    req.ApproveBelow,
		EscalateBelow:   req.EscalateBelow,
		DenyAtOrAbove:   req.DenyAtOrAbove,
		CooldownDenials: req.CooldownDenials,
		CooldownSeconds: req.CooldownSeconds,
		CreatedAt:       time.Now(),
	}
	s.store.SetPolicy(snap)
	writeJSON(w, http.StatusCreated, snap)
}

// --- GET /acp/v1/policy-snapshots/active ---

func (s *Server) handleGetActivePolicy(w http.ResponseWriter, r *http.Request) {
	p := s.store.GetPolicy()
	if p == nil {
		writeError(w, http.StatusNotFound, "NO_POLICY", "no active policy snapshot")
		return
	}
	writeJSON(w, http.StatusOK, p)
}
