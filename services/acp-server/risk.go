package main

import "time"

// capabilityWeights maps ACP capability identifiers to their base risk scores.
// Based on ACP-RISK-3.0 capability weight table.
var capabilityWeights = map[string]int{
	"acp:cap:skill.memory-read":    5,
	"acp:cap:skill.web-search":     10,
	"acp:cap:skill.file-read":      15,
	"acp:cap:skill.db-query":       20,
	"acp:cap:skill.api-call":       25,
	"acp:cap:skill.memory-write":   25,
	"acp:cap:skill.code-exec":      35,
	"acp:cap:skill.file-write":     40,
	"acp:cap:skill.shell-exec":     50,
	"acp:cap:skill.db-mutate":      55,
	"acp:cap:skill.agent-delegate": 30,
}

// autonomyMultipliers maps autonomy levels to their risk multipliers.
// L2 is the neutral baseline (multiplier = 1.0).
var autonomyMultipliers = map[AutonomyLevel]float64{
	AutonomyL1: 0.5,
	AutonomyL2: 1.0,
	AutonomyL3: 1.5,
}

const (
	frequencyWindow      = 60 * time.Second
	frequencyPenaltyStep = 5  // calls per window that trigger a penalty increment
	frequencyPenalty     = 10 // points added per step
	crossContextPenalty  = 15 // points added when action crosses agent context boundaries
)

// RiskInput is the data needed to score a single admission request.
type RiskInput struct {
	AgentID    string
	Capability string
	Resource   string
	Autonomy   AutonomyLevel

	// CallCount is the number of matching calls in the frequency window,
	// already recorded and returned by the store.
	CallCount int

	// CrossContext is true when the action crosses agent ownership boundaries
	// (currently unused in this implementation — pass false unless you track
	// cross-context in the orchestrator).
	CrossContext bool
}

// ScoreRisk computes the composite risk score for an admission request.
//
// Formula (ACP-RISK-3.0 §4):
//
//	RS = round(base_weight × autonomy_multiplier)
//	   + frequency_penalty
//	   + cross_context_penalty
//
// Note: the autonomy multiplier is applied multiplicatively against the base
// weight so that L1 agents (human-supervised) get a discount and L3 agents
// (fully autonomous) get a surcharge relative to the L2 baseline.
func ScoreRisk(input RiskInput) int {
	base, ok := capabilityWeights[input.Capability]
	if !ok {
		// Unknown capability — treat as high-risk
		base = 60
	}

	mult, ok := autonomyMultipliers[input.Autonomy]
	if !ok {
		mult = 1.0
	}

	rs := int(float64(base) * mult)

	// Frequency penalty: +10 for every 5 calls above the first in the window
	if input.CallCount > 1 {
		steps := (input.CallCount - 1) / frequencyPenaltyStep
		rs += steps * frequencyPenalty
	}

	if input.CrossContext {
		rs += crossContextPenalty
	}

	return rs
}

// RiskLevel converts a numeric score to a human-readable label.
func RiskLevel(score int) string {
	switch {
	case score < 30:
		return "LOW"
	case score < 70:
		return "MEDIUM"
	default:
		return "HIGH"
	}
}

// ApplyPolicy maps a risk score to an admission decision using the active policy snapshot.
func ApplyPolicy(score int, policy *PolicySnapshot) Decision {
	if policy == nil {
		// Fallback policy when no snapshot is configured
		if score < 30 {
			return DecisionApproved
		}
		if score < 70 {
			return DecisionEscalated
		}
		return DecisionDenied
	}
	if score < policy.ApproveBelow {
		return DecisionApproved
	}
	if score < policy.EscalateBelow {
		return DecisionEscalated
	}
	return DecisionDenied
}
