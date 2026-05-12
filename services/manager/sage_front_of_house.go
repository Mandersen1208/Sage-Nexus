// Package sageagents sage_front_of_house.go - AGT-sage as the user-facing voice in front of
// the manager's worker mesh.
package sageagents

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

const defaultSageSoulPath = "/home/node/.openclaw/workspace/SOUL.md"

// SageFrontOfHouseCapability is the ACP capability inbound tasks must declare
// to be routed through Sage. Local chat can also force this route by source.
const SageFrontOfHouseCapability = "acp:cap:skill.sage-front-of-house"

// SageRunner holds the things needed to drive a Sage turn end-to-end.
type SageRunner struct {
	Sage         *CopilotAgent
	Orchestrator *SageOrchestratorAgent
	Sessions     SessionStore
}

type sageRouteDecision struct {
	Direct bool
	Mode   string
	Reason string
}

type sageRevoicePolicy string
type sageRevoiceMode string

const (
	revoicePolicySelective sageRevoicePolicy = "selective"
	revoicePolicyAlways    sageRevoicePolicy = "always"
	revoicePolicyWrapper   sageRevoicePolicy = "wrapper"
	revoicePolicyOff       sageRevoicePolicy = "off"

	revoiceModeFull    sageRevoiceMode = "full"
	revoiceModeWrapper sageRevoiceMode = "wrapper"
	revoiceModeSkip    sageRevoiceMode = "skip"
)

type sageRevoiceDecision struct {
	Policy     sageRevoicePolicy
	Mode       sageRevoiceMode
	Reason     string
	SkipReason string
}

// NewSageRunner builds Sage's CopilotAgent with SOUL.md as her system prompt
// and bundles it with the orchestrator plus session store.
func NewSageRunner(
	cfg *AgentsConfig,
	orch *SageOrchestratorAgent,
	sessions SessionStore,
	stateDir string,
) *SageRunner {
	prompt, promptSource := loadSageSystemPrompt(cfg)
	model := cfg.ModelFor(SageAgentID)
	sage := &CopilotAgent{
		BaseAgent: BaseAgent{
			AgentID:     SageAgentID,
			ACPEndpoint: orch.ACPEndpoint,
			MCPEndpoint: orch.MCPEndpoint,
		},
		Model:        model,
		SystemPrompt: prompt,
		MCP:          nil,
		AllowedTools: nil,
	}
	sage.SetStateDir(stateDir)
	if prompt != "" {
		log.Printf("Loaded system prompt for %s from %s (%d chars, model=%s)", SageAgentID, promptSource, len(prompt), sage.ActiveModel())
	} else {
		log.Printf("Warning: no system prompt for %s - Sage will run with default voice", SageAgentID)
	}
	return &SageRunner{
		Sage:         sage,
		Orchestrator: orch,
		Sessions:     sessions,
	}
}

func loadSageSystemPrompt(cfg *AgentsConfig) (string, string) {
	soulPath := strings.TrimSpace(os.Getenv("SAGE_SOUL_PATH"))
	if soulPath == "" {
		soulPath = defaultSageSoulPath
	}
	b, err := os.ReadFile(soulPath)
	if err != nil {
		log.Printf("Warning: could not read Sage SOUL.md at %s: %v; falling back to bundled prompt", soulPath, err)
	} else if strings.TrimSpace(string(b)) == "" {
		log.Printf("Warning: Sage SOUL.md at %s is empty; falling back to bundled prompt", soulPath)
	} else {
		return string(b), soulPath
	}

	prompt, err := cfg.SystemPrompt(SageAgentID)
	if err != nil {
		log.Printf("Warning: bundled system prompt for %s could not be loaded: %v", SageAgentID, err)
		return "", "none"
	}
	return prompt, "bundled"
}

// Run executes Sage Auto. Sage is the user-facing chat layer; the manager /
// orchestrator owns the work routing for this mode.
func (r *SageRunner) Run(
	ctx context.Context,
	contextID, userInput string,
	tracker *HandoffTracker,
) (string, error) {
	if r == nil || r.Sage == nil || r.Orchestrator == nil {
		return "", fmt.Errorf("SageRunner not initialized")
	}
	if tracker != nil {
		tracker.Add(AgentHandoff{AgentID: SageAgentID, Role: "front-of-house"})
	}

	decision := sageRouteDecision{
		Direct: false,
		Mode:   "delegate",
		Reason: "sage_auto_routes_to_orchestrator",
	}
	AppendWorkContextEvent(ctx, "sage_route", SageAgentID, "Sage Auto routed to manager/orchestrator", "", map[string]interface{}{
		"mode":   decision.Mode,
		"reason": decision.Reason,
		"direct": decision.Direct,
	})
	EmitProgress(ctx, ProgressEvent{
		Type:        "route",
		Agent:       SageAgentID,
		Tool:        "sage_auto_route",
		Phase:       "end",
		Mode:        decision.Mode,
		RouteReason: decision.Reason,
		Timestamp:   time.Now().Unix(),
	})

	var prior []ChatMessage
	if r.Sessions != nil {
		prior = r.Sessions.Load(ctx, contextID)
		if len(prior) > 0 {
			log.Printf("  [%s] session loaded for %s: %s", SageAgentID, contextID, formatSessionForLog(prior))
		}
	}

	userMsg := ChatMessage{Role: "user", Content: userInput}
	reply, err := r.runDelegated(ctx, prior, userInput, decision, tracker)
	if err != nil {
		return reply, err
	}

	r.persistTurn(ctx, contextID, userMsg, reply)

	if tracker != nil {
		tracker.Add(AgentHandoff{
			AgentID: SageAgentID,
			Role:    "front-of-house",
			Model:   r.Sage.ActiveModel(),
			Reply:   reply,
		})
	}
	return reply, nil
}

func (r *SageRunner) RunSolo(
	ctx context.Context,
	contextID, userInput string,
	tracker *HandoffTracker,
) (string, error) {
	if r == nil || r.Sage == nil {
		return "", fmt.Errorf("SageRunner not initialized")
	}
	if tracker != nil {
		tracker.Add(AgentHandoff{AgentID: SageAgentID, Role: "solo"})
	}

	var prior []ChatMessage
	if r.Sessions != nil {
		prior = r.Sessions.Load(ctx, contextID)
		if len(prior) > 0 {
			log.Printf("  [%s] solo session loaded for %s: %s", SageAgentID, contextID, formatSessionForLog(prior))
		}
	}

	messages := make([]ChatMessage, 0, 2+len(prior))
	if r.Sage.SystemPrompt != "" {
		messages = append(messages, ChatMessage{Role: "system", Content: r.Sage.SystemPrompt})
	}
	messages = append(messages, prior...)
	userMsg := ChatMessage{Role: "user", Content: userInput}
	messages = append(messages, userMsg)
	decision := sageRouteDecision{Direct: true, Mode: "solo", Reason: "targeted_sage_solo"}
	reply, err := r.runDirect(ctx, messages, decision)
	if err != nil {
		return reply, err
	}
	r.persistTurn(ctx, contextID, userMsg, reply)
	if tracker != nil {
		tracker.Add(AgentHandoff{
			AgentID: SageAgentID,
			Role:    "solo",
			Model:   r.Sage.ActiveModel(),
			Reply:   reply,
		})
	}
	return reply, nil
}

func (r *SageRunner) runDirect(ctx context.Context, messages []ChatMessage, decision sageRouteDecision) (string, error) {
	start := time.Now()
	log.Printf("  [%s] fast path direct reason=%s", SageAgentID, decision.Reason)
	reply, err := r.Sage.Chat(ctx, messages)
	EmitLatencySpan(ctx, "sage_model_direct", start)
	dur := time.Since(start).Round(time.Millisecond)
	if err != nil {
		log.Printf("  [%s] direct failed (%s): %v", SageAgentID, dur, err)
		return reply, err
	}
	log.Printf("  [%s] direct ok (%s) replyLen=%d", SageAgentID, dur, len(reply))
	return reply, nil
}

func (r *SageRunner) runDelegated(
	ctx context.Context,
	prior []ChatMessage,
	userInput string,
	decision sageRouteDecision,
	tracker *HandoffTracker,
) (string, error) {
	query := buildDelegatedQuery(prior, userInput)
	log.Printf("  [%s] delegate -> orchestrator reason=%s query=%q", SageAgentID, decision.Reason, truncate(query, 120))
	EmitProgress(ctx, ProgressEvent{Type: "route", Agent: SageAgentID, Tool: "call_orchestrator", Phase: "start", Mode: "delegate", RouteReason: decision.Reason, Timestamp: time.Now().Unix()})
	start := time.Now()
	out, err := r.Orchestrator.Orchestrate(ctx, query, tracker)
	EmitLatencySpan(ctx, "orchestrator_total", start)
	dur := time.Since(start).Round(time.Millisecond)
	if err != nil {
		log.Printf("  [%s] orchestrator failed (%s): %v", SageAgentID, dur, err)
		EmitProgress(ctx, ProgressEvent{Type: "route", Agent: SageAgentID, Tool: "call_orchestrator", Phase: "end", Mode: "delegate", RouteReason: decision.Reason, DurationMS: dur.Milliseconds(), Error: err.Error(), Timestamp: time.Now().Unix()})
		return out, err
	}
	log.Printf("  [%s] orchestrator ok (%s) replyLen=%d", SageAgentID, dur, len(out))
	EmitProgress(ctx, ProgressEvent{Type: "route", Agent: SageAgentID, Tool: "call_orchestrator", Phase: "end", Mode: "delegate", RouteReason: decision.Reason, DurationMS: dur.Milliseconds(), Timestamp: time.Now().Unix()})
	managerReply := out
	if envBool("SAGE_DELEGATED_PREFER_WORKER_REPLY", true) {
		if workerReply := strings.TrimSpace(r.Orchestrator.LastReply()); workerReply != "" {
			managerReply = workerReply
		}
	}
	return r.applyDelegatedRevoice(ctx, userInput, managerReply)
}

func shouldRevoiceDelegated() bool {
	return resolveSageRevoicePolicy() != revoicePolicyOff
}

func resolveSageRevoicePolicy() sageRevoicePolicy {
	if policy, ok := parseSageRevoicePolicy(os.Getenv("SAGE_REVOICE_POLICY")); ok {
		return policy
	}

	if raw, ok := os.LookupEnv("SAGE_REVOICE_DELEGATED"); ok {
		switch strings.TrimSpace(strings.ToLower(raw)) {
		case "1", "true", "yes", "on":
			return revoicePolicyAlways
		case "0", "false", "no", "off":
			return revoicePolicyOff
		}
	}

	return revoicePolicySelective
}

func parseSageRevoicePolicy(raw string) (sageRevoicePolicy, bool) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case string(revoicePolicySelective):
		return revoicePolicySelective, true
	case string(revoicePolicyAlways):
		return revoicePolicyAlways, true
	case string(revoicePolicyWrapper):
		return revoicePolicyWrapper, true
	case string(revoicePolicyOff):
		return revoicePolicyOff, true
	default:
		return "", false
	}
}

func (r *SageRunner) applyDelegatedRevoice(ctx context.Context, userInput, managerReply string) (string, error) {
	decision := classifyDelegatedRevoice(userInput, managerReply)
	emitSageRevoiceDecision(ctx, decision)
	log.Printf("  [%s] revoice policy=%s mode=%s reason=%s", SageAgentID, decision.Policy, decision.Mode, decision.Reason)

	switch decision.Mode {
	case revoiceModeFull:
		return r.revoiceDelegatedReply(ctx, userInput, managerReply)
	case revoiceModeWrapper:
		return managerReply, nil
	default:
		return managerReply, nil
	}
}

func classifyDelegatedRevoice(userInput, managerReply string) sageRevoiceDecision {
	policy := resolveSageRevoicePolicy()
	trimmed := strings.TrimSpace(managerReply)
	if trimmed == "" {
		return sageRevoiceDecision{Policy: policy, Mode: revoiceModeSkip, Reason: "empty_output", SkipReason: "empty_output"}
	}

	switch policy {
	case revoicePolicyOff:
		return sageRevoiceDecision{Policy: policy, Mode: revoiceModeSkip, Reason: "policy_off", SkipReason: "policy_off"}
	case revoicePolicyAlways:
		return sageRevoiceDecision{Policy: policy, Mode: revoiceModeFull, Reason: "policy_always"}
	case revoicePolicyWrapper:
		return sageRevoiceDecision{Policy: policy, Mode: revoiceModeWrapper, Reason: "policy_wrapper"}
	}

	if looksMachineReadableJSON(trimmed) {
		return sageRevoiceDecision{Policy: policy, Mode: revoiceModeSkip, Reason: "machine_readable", SkipReason: "machine_readable"}
	}
	if looksRawLogOrCommandOutput(trimmed) {
		return sageRevoiceDecision{Policy: policy, Mode: revoiceModeSkip, Reason: "raw_output", SkipReason: "raw_output"}
	}
	if looksToolError(trimmed) {
		return sageRevoiceDecision{Policy: policy, Mode: revoiceModeSkip, Reason: "tool_error", SkipReason: "tool_error"}
	}
	if shouldWrapDelegatedReply(userInput, trimmed) {
		return sageRevoiceDecision{Policy: policy, Mode: revoiceModeSkip, Reason: "technical_artifact", SkipReason: "technical_artifact"}
	}

	return sageRevoiceDecision{Policy: policy, Mode: revoiceModeSkip, Reason: "pass_through_default", SkipReason: "pass_through_default"}
}

func emitSageRevoiceDecision(ctx context.Context, decision sageRevoiceDecision) {
	meta := map[string]interface{}{
		"revoice_policy": string(decision.Policy),
		"revoice_mode":   string(decision.Mode),
		"reason":         decision.Reason,
	}
	if decision.SkipReason != "" {
		meta["skip_reason"] = decision.SkipReason
	}
	AppendWorkContextEvent(ctx, "sage_revoice", SageAgentID, "Sage delegated revoice policy resolved", "", meta)
	EmitProgress(ctx, ProgressEvent{
		Type:          "route",
		Agent:         SageAgentID,
		Tool:          "sage_revoice",
		Phase:         "end",
		Mode:          string(decision.Mode),
		RouteReason:   decision.Reason,
		RevoicePolicy: string(decision.Policy),
		RevoiceMode:   string(decision.Mode),
		SkipReason:    decision.SkipReason,
		Timestamp:     time.Now().Unix(),
	})
}

func looksMachineReadableJSON(text string) bool {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return false
	}
	var decoded interface{}
	return json.Unmarshal([]byte(trimmed), &decoded) == nil
}

func looksToolError(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "mcp tool returned error") ||
		strings.Contains(lower, "tool returned error") ||
		strings.Contains(lower, "already connected to a transport") ||
		strings.Contains(lower, "bad request")
}

func looksRawLogOrCommandOutput(text string) bool {
	lines := meaningfulLines(text)
	if len(lines) < 3 {
		return false
	}

	rawLines := 0
	for _, line := range lines {
		lower := strings.ToLower(strings.TrimSpace(line))
		switch {
		case looksTimestampedLogLine(line):
			rawLines++
		case strings.HasPrefix(lower, "$ "), strings.HasPrefix(lower, "> "):
			rawLines++
		case strings.Contains(lower, "exit code:"), strings.Contains(lower, "wall time:"):
			rawLines++
		case strings.Contains(lower, "traceback "), strings.HasPrefix(lower, "panic:"):
			rawLines++
		case strings.Contains(lower, "level=error"), strings.Contains(lower, "level=warn"):
			rawLines++
		}
	}

	return rawLines >= 3 || (rawLines >= 2 && rawLines*2 >= len(lines))
}

func looksTimestampedLogLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 10 {
		return false
	}
	hasDatePrefix := len(trimmed) >= 10 &&
		trimmed[0] >= '0' && trimmed[0] <= '9' &&
		trimmed[1] >= '0' && trimmed[1] <= '9' &&
		trimmed[2] >= '0' && trimmed[2] <= '9' &&
		trimmed[3] >= '0' && trimmed[3] <= '9' &&
		(trimmed[4] == '-' || trimmed[4] == '/') &&
		trimmed[5] >= '0' && trimmed[5] <= '9' &&
		trimmed[6] >= '0' && trimmed[6] <= '9' &&
		(trimmed[7] == '-' || trimmed[7] == '/')
	return hasDatePrefix || strings.HasPrefix(trimmed, "time=")
}

func shouldWrapDelegatedReply(userInput, managerReply string) bool {
	lowerInput := strings.ToLower(userInput)
	lowerReply := strings.ToLower(managerReply)
	if containsAny(lowerInput, []string{
		"technical documentation", "documentation", "docs", "readme", "runbook", "architecture document",
		"generate a document", "create a document", "template", "email template", "copyable", "artifact",
		"code block", "write code", "config", "yaml", "json", "toml", "dockerfile", "terraform", "sql",
	}) {
		return true
	}
	if strings.Contains(managerReply, "```") {
		return true
	}
	if containsMarkdownTable(managerReply) {
		return true
	}
	if countMarkdownHeadings(managerReply) >= 2 {
		return true
	}
	if looksChecklist(managerReply) {
		return true
	}
	if isSourceHeavy(lowerReply) {
		return true
	}
	return false
}

func containsMarkdownTable(text string) bool {
	return strings.Contains(text, "\n|") && (strings.Contains(text, "| ---") || strings.Contains(text, "|---"))
}

func countMarkdownHeadings(text string) int {
	count := 0
	for _, line := range meaningfulLines(text) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "### ") {
			count++
		}
	}
	return count
}

func looksChecklist(text string) bool {
	count := 0
	for _, line := range meaningfulLines(text) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- [ ]") ||
			strings.HasPrefix(trimmed, "- [x]") ||
			strings.HasPrefix(trimmed, "1. ") ||
			strings.HasPrefix(trimmed, "2. ") ||
			strings.HasPrefix(trimmed, "3. ") {
			count++
		}
	}
	return count >= 4
}

func isSourceHeavy(lowerText string) bool {
	urls := strings.Count(lowerText, "https://") + strings.Count(lowerText, "http://")
	return urls >= 3 ||
		strings.Contains(lowerText, "research_brief") ||
		strings.Contains(lowerText, "sources:") ||
		strings.Contains(lowerText, "source:")
}

func meaningfulLines(text string) []string {
	raw := strings.Split(text, "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func wrapDelegatedReply(managerReply string) string {
	body := strings.TrimSpace(managerReply)
	return "Got it. I'm keeping the artifact itself clean and copyable, because this is where style can quietly break useful.\n\n" +
		body +
		"\n\nThat's the source-of-truth version. Sage voice stays around it, not inside it."
}

func (r *SageRunner) revoiceDelegatedReply(ctx context.Context, userInput, managerReply string) (string, error) {
	messages := []ChatMessage{}
	if r.Sage.SystemPrompt != "" {
		messages = append(messages, ChatMessage{Role: "system", Content: r.Sage.SystemPrompt})
	}
	messages = append(messages, ChatMessage{
		Role: "user",
		Content: "Re-voice the manager result for Matt in your Sage voice. Preserve all facts, file paths, warnings, errors, and next steps. Do not add new claims.\n\n" +
			"Matt asked:\n" + userInput + "\n\nManager result:\n" + managerReply,
	})
	start := time.Now()
	reply, err := r.Sage.Chat(ctx, messages)
	EmitLatencySpan(ctx, "sage_revoice", start)
	dur := time.Since(start).Round(time.Millisecond)
	if err != nil {
		log.Printf("  [%s] revoice failed (%s): %v; returning manager output", SageAgentID, dur, err)
		return managerReply, nil
	}
	log.Printf("  [%s] revoice ok (%s) replyLen=%d", SageAgentID, dur, len(reply))
	return reply, nil
}

func (r *SageRunner) persistTurn(ctx context.Context, contextID string, userMsg ChatMessage, reply string) {
	if r.Sessions == nil || reply == "" {
		return
	}
	start := time.Now()
	r.Sessions.Append(ctx, contextID, userMsg)
	r.Sessions.Append(ctx, contextID, ChatMessage{Role: "assistant", Content: reply})
	r.Sessions.Trim(ctx, contextID, envInt("SAGE_SESSION_MAX_TURNS", 40))
	EmitLatencySpan(ctx, "session_persist", start)
}

// Resume continues an orchestrator run that paused at the round cap. The saved
// messages are orchestrator messages because delegated Sage now bypasses the
// front-of-house tool loop.
func (r *SageRunner) Resume(
	ctx context.Context,
	prevMessages []ChatMessage,
	tracker *HandoffTracker,
	originalInput, note string,
) (string, error) {
	if r == nil || r.Sage == nil || r.Orchestrator == nil {
		return "", fmt.Errorf("SageRunner not initialized")
	}
	start := time.Now()
	reply, err := r.Orchestrator.Resume(ctx, prevMessages, tracker, originalInput, note)
	EmitLatencySpan(ctx, "orchestrator_resume", start)
	if err != nil {
		return reply, err
	}
	return r.applyDelegatedRevoice(ctx, originalInput, reply)
}

func classifySageRoute(input string) sageRouteDecision {
	if !envBool("SAGE_FAST_PATH_ENABLED", true) {
		return sageRouteDecision{Direct: false, Mode: "delegate", Reason: "fast_path_disabled"}
	}

	normalized := normalizeRouteText(input)
	if normalized == "" {
		return sageRouteDecision{Direct: true, Mode: "direct", Reason: "empty_clarification"}
	}
	if isAcknowledgementOnly(normalized) {
		return sageRouteDecision{Direct: true, Mode: "direct", Reason: "acknowledgement"}
	}
	if isVoiceFeedback(normalized) {
		return sageRouteDecision{Direct: true, Mode: "direct", Reason: "voice_feedback"}
	}
	if isCasualCreativeRequest(normalized) {
		return sageRouteDecision{Direct: true, Mode: "direct", Reason: "casual_creative"}
	}
	if reason := delegationReason(normalized); reason != "" {
		return sageRouteDecision{Direct: false, Mode: "delegate", Reason: reason}
	}
	if isDirectConversation(normalized) {
		return sageRouteDecision{Direct: true, Mode: "direct", Reason: "conversation"}
	}
	return sageRouteDecision{Direct: false, Mode: "delegate", Reason: "default_delegate"}
}

func delegationReason(normalized string) string {
	if containsAny(normalized, []string{
		"attached", "attachment", "screenshot", "image", "photo", "picture",
	}) {
		return "attachment_or_vision"
	}
	if containsAny(normalized, []string{
		"budget", "spend", "spending", "money", "income", "groceries", "transaction", "bank", "debt", "savings",
	}) {
		return "finance"
	}
	if containsAny(normalized, []string{
		"research", "look up", "latest", "current", "today", "recent", "web", "internet", "docs", "documentation", "verify", "validate",
	}) {
		return "research_or_current_fact"
	}
	if containsAny(normalized, []string{
		"fix", "repair", "build", "create", "implement", "update", "change", "edit", "delete", "remove", "refactor", "write", "generate", "install", "rebuild", "restart", "deploy", "migrate", "configure", "commit", "push", "run test", "test ", "try again", "retry", "look into",
	}) {
		return "work_request"
	}
	if containsAny(normalized, []string{
		"docker", "compose", "container", "mcp", "openclaw", "hermes", "manager", "orchestrator", "redis", "route", "endpoint", "service", "mount", "env var", "config", "prompt", "soul", "agent", "skill", "dashboard", "nexus", "logs",
	}) {
		return "system_or_runtime"
	}
	if containsAny(normalized, []string{
		"plan", "architecture", "architect", "design", "proposal", "workorder", "handoff",
	}) {
		return "planning"
	}
	return ""
}

func isAcknowledgementOnly(normalized string) bool {
	words := strings.Fields(normalized)
	if len(words) > 8 {
		return false
	}
	if strings.Contains(normalized, "?") ||
		strings.Contains(normalized, "can you") ||
		strings.Contains(normalized, "could you") ||
		strings.Contains(normalized, "please ") {
		return false
	}
	return normalized == "yes" ||
		normalized == "no" ||
		normalized == "ok" ||
		normalized == "okay" ||
		normalized == "bet" ||
		normalized == "sounds good" ||
		normalized == "that tracks" ||
		normalized == "nice" ||
		normalized == "thanks" ||
		normalized == "thank you" ||
		strings.HasPrefix(normalized, "thanks ") ||
		strings.HasPrefix(normalized, "thank you ") ||
		strings.HasPrefix(normalized, "lol") ||
		strings.HasPrefix(normalized, "lmao")
}

func isVoiceFeedback(normalized string) bool {
	return containsAny(normalized, []string{
		"did not sound like sage",
		"does not sound like sage",
		"doesn't sound like sage",
		"lost your voice",
		"not responding as herself",
		"not responding like yourself",
		"sage voice",
		"your voice back",
		"sound like yourself",
		"sound more like sage",
	})
}

func isCasualCreativeRequest(normalized string) bool {
	return containsAny(normalized, []string{
		"tell me a story",
		"tell a story",
		"make up a story",
		"bedtime story",
		"write me a story",
		"tell me something funny",
		"make me laugh",
		"chat with me",
	})
}

func isDirectConversation(normalized string) bool {
	if containsExactRouteWord(normalized, []string{"hey", "hi", "hello", "yo"}) {
		return true
	}
	if containsAny(normalized, []string{
		"how are you", "who are you", "what are you", "i like", "i love",
		"you can", "you should sound", "your vibe", "your voice",
	}) {
		return true
	}
	words := strings.Fields(normalized)
	return len(words) <= 6 && !strings.Contains(normalized, "?")
}

func containsExactRouteWord(s string, terms []string) bool {
	padded := " " + s + " "
	for _, term := range terms {
		if strings.Contains(padded, " "+term+" ") {
			return true
		}
	}
	return false
}

func buildDelegatedQuery(prior []ChatMessage, userInput string) string {
	if len(prior) == 0 {
		return userInput
	}
	start := len(prior) - 6
	if start < 0 {
		start = 0
	}
	var b strings.Builder
	b.WriteString("Recent chat context for continuity:\n")
	for _, msg := range prior[start:] {
		if msg.Role == "" || strings.TrimSpace(msg.Content) == "" {
			continue
		}
		b.WriteString(msg.Role)
		b.WriteString(": ")
		b.WriteString(truncate(strings.TrimSpace(msg.Content), 800))
		b.WriteString("\n")
	}
	b.WriteString("\nCurrent user request:\n")
	b.WriteString(userInput)
	return b.String()
}

func normalizeRouteText(input string) string {
	return strings.ToLower(strings.TrimSpace(input))
}
