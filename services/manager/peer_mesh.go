package sageagents

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

const (
	DefaultPeerMeshMaxDepth = 3
)

type peerCallDepthKey struct{}

type PeerPolicy struct {
	Allowlist        map[string][]string
	MaxDepth         int
	MaxDepthByCaller map[string]int
}

var peerPolicyState = struct {
	sync.RWMutex
	policy PeerPolicy
}{
	policy: PeerPolicy{
		Allowlist:        map[string][]string{},
		MaxDepth:         DefaultPeerMeshMaxDepth,
		MaxDepthByCaller: map[string]int{},
	},
}

func ConfigurePeerPolicy(policy PeerPolicy) {
	peerPolicyState.Lock()
	defer peerPolicyState.Unlock()
	if policy.Allowlist == nil {
		policy.Allowlist = map[string][]string{}
	}
	if policy.MaxDepth <= 0 {
		policy.MaxDepth = DefaultPeerMeshMaxDepth
	}
	if policy.MaxDepthByCaller == nil {
		policy.MaxDepthByCaller = map[string]int{}
	}
	copied := PeerPolicy{
		Allowlist:        map[string][]string{},
		MaxDepth:         policy.MaxDepth,
		MaxDepthByCaller: map[string]int{},
	}
	for caller, targets := range policy.Allowlist {
		copied.Allowlist[strings.TrimSpace(caller)] = uniqueStrings(targets)
	}
	for caller, depth := range policy.MaxDepthByCaller {
		if depth > 0 {
			copied.MaxDepthByCaller[strings.TrimSpace(caller)] = depth
		}
	}
	peerPolicyState.policy = copied
}

func currentPeerPolicy() PeerPolicy {
	peerPolicyState.RLock()
	defer peerPolicyState.RUnlock()
	policy := peerPolicyState.policy
	copied := PeerPolicy{
		Allowlist:        map[string][]string{},
		MaxDepth:         policy.MaxDepth,
		MaxDepthByCaller: map[string]int{},
	}
	for caller, targets := range policy.Allowlist {
		copied.Allowlist[caller] = append([]string{}, targets...)
	}
	for caller, depth := range policy.MaxDepthByCaller {
		copied.MaxDepthByCaller[caller] = depth
	}
	return copied
}

// AgentCanCallPeers reports whether the worker should receive peer-mesh tools.
func AgentCanCallPeers(agentID string) bool {
	return len(AllowedPeerTargets(agentID)) > 0
}

// AllowedPeerTargets returns the configured target agents for a caller.
func AllowedPeerTargets(agentID string) []string {
	policy := currentPeerPolicy()
	targets := append([]string{}, policy.Allowlist[strings.TrimSpace(agentID)]...)
	sort.Strings(targets)
	return targets
}

// IsPeerCallAllowed enforces bounded domain collaboration from registry policy.
func IsPeerCallAllowed(callerAgentID, targetAgentID string) bool {
	callerAgentID = strings.TrimSpace(callerAgentID)
	targetAgentID = strings.TrimSpace(targetAgentID)
	if callerAgentID == "" || targetAgentID == "" || callerAgentID == targetAgentID {
		return false
	}
	policy := currentPeerPolicy()
	for _, target := range policy.Allowlist[callerAgentID] {
		if target == targetAgentID {
			return true
		}
	}
	return false
}

func ValidatePeerCall(callerAgentID, targetAgentID string, depth int) error {
	maxDepth := PeerMeshMaxDepthFor(callerAgentID)
	if depth >= maxDepth {
		return fmt.Errorf("max agent call depth (%d) reached", maxDepth)
	}
	if !IsPeerCallAllowed(callerAgentID, targetAgentID) {
		return fmt.Errorf("peer call not allowed")
	}
	return nil
}

func PeerMeshMaxDepth() int {
	maxDepth := envInt("SAGE_AGENT_MESH_MAX_DEPTH", DefaultPeerMeshMaxDepth)
	if maxDepth <= 0 {
		policy := currentPeerPolicy()
		if policy.MaxDepth > 0 {
			return policy.MaxDepth
		}
		return DefaultPeerMeshMaxDepth
	}
	return maxDepth
}

func PeerMeshMaxDepthFor(agentID string) int {
	envDepth := envInt("SAGE_AGENT_MESH_MAX_DEPTH", 0)
	if envDepth > 0 {
		return envDepth
	}
	policy := currentPeerPolicy()
	if depth := policy.MaxDepthByCaller[strings.TrimSpace(agentID)]; depth > 0 {
		return depth
	}
	if policy.MaxDepth > 0 {
		return policy.MaxDepth
	}
	return DefaultPeerMeshMaxDepth
}

func WithPeerCallDepth(ctx context.Context, depth int) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if depth < 0 {
		depth = 0
	}
	return context.WithValue(ctx, peerCallDepthKey{}, depth)
}

func PeerCallDepthFromContext(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	depth, ok := ctx.Value(peerCallDepthKey{}).(int)
	if !ok || depth < 0 {
		return 0
	}
	return depth
}
