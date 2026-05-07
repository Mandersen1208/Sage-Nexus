package main

import (
	"fmt"
	"sync"
	"time"
)

// Store is a thread-safe in-memory store for all ACP server state.
type Store struct {
	mu sync.RWMutex

	agents     map[string]*Agent
	challenges map[string]*Challenge // keyed by nonce
	tokens     map[string]*ExecutionToken
	policy     *PolicySnapshot
	frequency  map[string]*FrequencyRecord // keyed by agentID+cap+resource
	denials    map[string]*DenialRecord    // keyed by agentID
}

func NewStore() *Store {
	return &Store{
		agents:     make(map[string]*Agent),
		challenges: make(map[string]*Challenge),
		tokens:     make(map[string]*ExecutionToken),
		frequency:  make(map[string]*FrequencyRecord),
		denials:    make(map[string]*DenialRecord),
	}
}

// --- Agent operations ---

func (s *Store) PutAgent(a *Agent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents[a.AgentID] = a
}

func (s *Store) GetAgent(id string) (*Agent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.agents[id]
	return a, ok
}

func (s *Store) ListAgents() []*Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Agent, 0, len(s.agents))
	for _, a := range s.agents {
		out = append(out, a)
	}
	return out
}

// --- Challenge operations ---

func (s *Store) PutChallenge(c *Challenge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Evict stale challenges for this agent first
	for nonce, ch := range s.challenges {
		if ch.AgentID == c.AgentID || time.Now().After(ch.ExpiresAt) {
			delete(s.challenges, nonce)
		}
	}
	s.challenges[c.Nonce] = c
}

func (s *Store) ConsumeChallenge(nonce string) (*Challenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.challenges[nonce]
	if !ok {
		return nil, fmt.Errorf("challenge not found")
	}
	if ch.Used {
		return nil, fmt.Errorf("challenge already used")
	}
	if time.Now().After(ch.ExpiresAt) {
		delete(s.challenges, nonce)
		return nil, fmt.Errorf("challenge expired")
	}
	ch.Used = true
	delete(s.challenges, nonce)
	return ch, nil
}

// --- Execution token operations ---

func (s *Store) PutExecutionToken(et *ExecutionToken) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[et.ETID] = et
}

func (s *Store) ConsumeExecutionToken(etID string) (*ExecutionToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	et, ok := s.tokens[etID]
	if !ok {
		return nil, fmt.Errorf("execution token not found")
	}
	if et.Consumed {
		return nil, fmt.Errorf("execution token already consumed")
	}
	if time.Now().After(et.ExpiresAt) {
		return nil, fmt.Errorf("execution token expired")
	}
	now := time.Now()
	et.Consumed = true
	et.ConsumedAt = &now
	return et, nil
}

// --- Policy operations ---

func (s *Store) SetPolicy(p *PolicySnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.policy != nil {
		s.policy.Status = "INACTIVE"
	}
	s.policy = p
}

func (s *Store) GetPolicy() *PolicySnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.policy
}

// --- Frequency tracking (for risk scoring) ---

func frequencyKey(agentID, capability, resource string) string {
	return agentID + "|" + capability + "|" + resource
}

// RecordCall records a capability invocation and returns the count within the window.
func (s *Store) RecordCall(agentID, capability, resource string, window time.Duration) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := frequencyKey(agentID, capability, resource)
	rec, ok := s.frequency[key]
	if !ok {
		rec = &FrequencyRecord{
			AgentID:    agentID,
			Capability: capability,
			Resource:   resource,
		}
		s.frequency[key] = rec
	}

	now := time.Now()
	cutoff := now.Add(-window)

	// Evict timestamps outside the window
	fresh := rec.Timestamps[:0]
	for _, t := range rec.Timestamps {
		if t.After(cutoff) {
			fresh = append(fresh, t)
		}
	}
	fresh = append(fresh, now)
	rec.Timestamps = fresh

	return len(rec.Timestamps)
}

// --- Denial tracking (for cooldown enforcement) ---

func (s *Store) RecordDenial(agentID string, cooldownDuration time.Duration, triggerAfter int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.denials[agentID]
	if !ok {
		rec = &DenialRecord{AgentID: agentID}
		s.denials[agentID] = rec
	}
	rec.Count++
	if rec.Count >= triggerAfter && rec.CooldownEnd == nil {
		end := time.Now().Add(cooldownDuration)
		rec.CooldownEnd = &end
	}
}

// IsInCooldown returns true if the agent is currently in a denial cooldown.
func (s *Store) IsInCooldown(agentID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.denials[agentID]
	if !ok {
		return false
	}
	if rec.CooldownEnd == nil {
		return false
	}
	if time.Now().After(*rec.CooldownEnd) {
		// Cooldown expired — reset in a write lock
		return false
	}
	return true
}

func (s *Store) ClearCooldown(agentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec, ok := s.denials[agentID]; ok {
		rec.CooldownEnd = nil
		rec.Count = 0
	}
}
