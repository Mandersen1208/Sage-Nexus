package main

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	sageagents "github.com/matta/sage-nexus/services/manager"
	"github.com/matta/sage-nexus/services/manager/a2a"
)

var discoveryRuntime *skillDiscoveryRuntime

//go:embed static
var staticFiles embed.FS

// SageResponse is the legacy synchronous reply body used by the HTTP /dispatch
// endpoint for CLI/curl testing. Production traffic goes over Redis A2A events.
type SageResponse struct {
	RequestID string `json:"request_id"`
	Content   string `json:"content"`
	Agent     string `json:"agent"`
	Status    string `json:"status"` // "ok" | "error" | "escalate"
	Error     string `json:"error,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// inboundTask is what the manager extracts from an A2A Message arriving on
// sage:tasks. The full Message struct is in a2a/types.go; here we just need
// the fields the orchestrator cares about.
type inboundTask struct {
	TaskID        string
	ActiveTaskID  string
	ContextID     string
	Content       string
	Capability    string
	Resource      string
	Source        string
	AgentMode     string
	TargetAgentID string
	ModeLabel     string
}

func parseA2AMessage(payload []byte) (inboundTask, error) {
	var msg a2a.Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		return inboundTask{}, err
	}
	if msg.TaskID == "" {
		return inboundTask{}, fmt.Errorf("task id missing")
	}
	var content strings.Builder
	for _, p := range msg.Parts {
		if p.Kind == "text" {
			content.WriteString(p.Text)
		}
	}
	capability, _ := msg.Metadata["capability"].(string)
	res, _ := msg.Metadata["resource"].(string)
	source, _ := msg.Metadata["source"].(string)
	agentMode, _ := msg.Metadata["agent_mode"].(string)
	targetAgentID, _ := msg.Metadata["target_agent_id"].(string)
	modeLabel, _ := msg.Metadata["mode_label"].(string)
	activeTaskID, _ := msg.Metadata["active_task_id"].(string)
	if capability == "" {
		capability = "acp:cap:skill.agent-delegate"
	}
	if res == "" {
		res = "sage://workspace/*"
	}
	return inboundTask{
		TaskID:        msg.TaskID,
		ActiveTaskID:  activeTaskID,
		ContextID:     msg.ContextID,
		Content:       content.String(),
		Capability:    capability,
		Resource:      res,
		Source:        source,
		AgentMode:     agentMode,
		TargetAgentID: targetAgentID,
		ModeLabel:     modeLabel,
	}, nil
}

// newTaskID generates a synthetic task ID for HTTP-originated requests so the
// dashboard can correlate them on the same bus.
func newTaskID() string {
	b := mustRandomBytes(8)
	return "http-" + hex.EncodeToString(b)
}

func newActiveTaskID() string {
	b := mustRandomBytes(8)
	return "chat-task-" + hex.EncodeToString(b)
}

func newContextID() string {
	b := mustRandomBytes(6)
	return "local-" + time.Now().Format("20060102T150405") + "-" + hex.EncodeToString(b)
}

func newChatMessageID() string {
	b := mustRandomBytes(6)
	return "msg-" + hex.EncodeToString(b)
}

func mustRandomBytes(size int) []byte {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Errorf("generate random identifier: %w", err))
	}
	return b
}

func main() {
	listenAddr := sageagents.GetEnvOr("MANAGER_LISTEN_ADDR", ":8090")
	acpEndpoint := sageagents.GetEnvOr("ACP_ENDPOINT", "http://acp-server:8080")
	mcpEndpoint := sageagents.GetEnvOr("MCP_ENDPOINT", "http://sage-mcp:3030")
	configPath := sageagents.GetEnvOr("AGENTS_CONFIG", "/app/config/agents.json")
	redisAddr := sageagents.GetEnvOr("REDIS_ADDR", "redis:6379")
	ctFile := sageagents.GetEnvOr("ACP_CT_FILE", "")
	ctEnv := os.Getenv("ACP_CAPABILITY_TOKEN")
	codexBridgeURL := strings.TrimSpace(os.Getenv("CODEX_BRIDGE_URL"))
	ctx := context.Background()

	// ── Config + system prompt ───────────────────────────────────────────────
	registryPath := sageagents.GetEnvOr("SAGE_AGENT_REGISTRY_FILE", sageagents.DefaultRegistryPath)
	cfg, err := sageagents.LoadAgentRegistry(configPath, registryPath)
	if err != nil {
		log.Printf("Warning: agent registry not loaded (%v) - using empty defaults", err)
		cfg = &sageagents.AgentsConfig{}
	}
	for _, warning := range cfg.Warnings {
		log.Printf("Warning: agent registry: %s", warning)
	}
	sageagents.ConfigurePeerPolicy(cfg.PeerPolicy())
	agentID := sageagents.OrchestratorAgentID
	baseSystemPrompt, err := cfg.SystemPrompt(agentID)
	if err != nil {
		log.Printf("Warning: system prompt: %v", err)
	}
	systemPrompt := cfg.BuildOrchestratorPrompt(baseSystemPrompt)
	if systemPrompt != "" {
		log.Printf("Loaded system prompt for %s (%d chars)", agentID, len(systemPrompt))
	}

	// ── Build orchestrator + workers ─────────────────────────────────────────
	stateDir := sageagents.GetEnvOr("SAGE_STATE_DIR", "/sage-state")
	mcpToolCatalog, err := (&sageagents.MCPClient{Endpoint: mcpEndpoint + "/mcp"}).ListTools()
	if err != nil {
		log.Printf("Warning: MCP tools/list failed at startup: %v", err)
	} else {
		log.Printf("Discovered %d MCP tools from %s", len(mcpToolCatalog), mcpEndpoint)
	}
	availableMCPTools := toolNameSet(mcpToolCatalog)

	buildWorker := func(id string) *sageagents.CopilotAgent {
		prompt, err := cfg.SystemPrompt(id)
		if err != nil {
			log.Printf("Warning: system prompt for %s: %v", id, err)
		}
		model := cfg.ModelFor(id)
		allowedTools, warnings := cfg.ResolveToolsForAgent(id)
		for _, warning := range warnings {
			log.Printf("Warning: agent tools: %s", warning)
		}
		allowedTools = filterAllowedMCPTools(id, allowedTools, availableMCPTools)
		w := &sageagents.CopilotAgent{
			BaseAgent: sageagents.BaseAgent{
				AgentID:     id,
				ACPEndpoint: acpEndpoint,
				MCPEndpoint: mcpEndpoint,
			},
			Model:        model,
			SystemPrompt: prompt,
			MCP:          &sageagents.MCPClient{Endpoint: mcpEndpoint + "/mcp"},
			AllowedTools: allowedTools,
			ToolCatalog:  mcpToolCatalog,
		}
		w.SetStateDir(stateDir)
		w.SetCodexBridge(codexBridgeURL, true)
		if prompt != "" {
			log.Printf("Loaded system prompt for %s (%d chars, model=%s)", id, len(prompt), w.ActiveModel())
		}
		return w
	}

	workers := make(map[string]*sageagents.CopilotAgent)
	for _, id := range cfg.WorkerAgentIDs() {
		workers[id] = buildWorker(id)
	}
	routes, routeWarnings := cfg.BuildOrchestratorRoutes()
	for _, warning := range routeWarnings {
		log.Printf("Warning: agent routes: %s", warning)
	}

	mgr := sageagents.NewManager()
	orchestrator := &sageagents.SageOrchestratorAgent{
		BaseAgent: sageagents.BaseAgent{
			AgentID:     agentID,
			ACPEndpoint: acpEndpoint,
			MCPEndpoint: mcpEndpoint,
		},
		Manager:      mgr,
		Workers:      workers,
		Registry:     cfg,
		RouteTools:   routes.Tools,
		RouteTargets: routes.Targets,
		Model:        cfg.ModelFor(agentID),
		SystemPrompt: systemPrompt,
	}
	orchestrator.SetStateDir(stateDir)
	if orchestrator.LLM() != nil {
		orchestrator.LLM().SetCodexBridge(codexBridgeURL, true)
	}
	registerInProcessAgents(ctx, mgr, orchestrator, workers)

	keyFile := sageagents.GetEnvOr("AGENT_KEY_FILE", "/data/agent.key")
	keySourceFile := strings.TrimSpace(os.Getenv("AGENT_KEY_SOURCE_FILE"))
	if keySourceFile != "" {
		sourceKey, readErr := os.ReadFile(keySourceFile)
		if readErr != nil {
			log.Printf("Warning: AGENT_KEY_SOURCE_FILE %s could not be read: %v", keySourceFile, readErr)
		} else {
			currentKey, currentErr := os.ReadFile(keyFile)
			if currentErr != nil && !errors.Is(currentErr, os.ErrNotExist) {
				log.Printf("Warning: could not read AGENT_KEY_FILE %s: %v", keyFile, currentErr)
			}
			if currentErr != nil || strings.TrimSpace(string(currentKey)) != strings.TrimSpace(string(sourceKey)) {
				if mkErr := os.MkdirAll(filepath.Dir(keyFile), 0o700); mkErr != nil {
					log.Printf("Warning: could not create key directory %s: %v", filepath.Dir(keyFile), mkErr)
				} else if wrErr := os.WriteFile(keyFile, sourceKey, 0o600); wrErr != nil {
					log.Printf("Warning: could not sync AGENT_KEY_FILE %s from %s: %v", keyFile, keySourceFile, wrErr)
				} else if errors.Is(currentErr, os.ErrNotExist) {
					log.Printf("Seeded AGENT_KEY_FILE %s from %s", keyFile, keySourceFile)
				} else {
					log.Printf("Updated AGENT_KEY_FILE %s from %s", keyFile, keySourceFile)
				}
			}
		}
	}
	if err := orchestrator.LoadOrGenerateIdentity(keyFile); err != nil {
		log.Fatalf("Identity load/generate failed: %v", err)
	}
	log.Printf("Identity loaded from %s  pubkey=%s...", keyFile, orchestrator.PublicKeyBase64()[:16])

	// ── ACP registration ─────────────────────────────────────────────────────
	if err := orchestrator.RegisterWithACP("L2"); err != nil {
		log.Printf("ACP registration failed (will retry): %v", err)
	} else {
		log.Printf("Registered with ACP  agent=%s  pubkey=%s...",
			agentID, orchestrator.PublicKeyBase64()[:16])
	}

	// ── ACP client (admission control for dispatching tasks) ─────────────────
	ct := ctEnv
	if ct == "" && ctFile != "" {
		b, err := os.ReadFile(ctFile)
		if err != nil {
			log.Printf("Warning: could not read ACP_CT_FILE %s: %v", ctFile, err)
		} else {
			ct = string(b)
			log.Printf("Loaded capability token from %s", ctFile)
		}
	}
	if ct == "" {
		issued, err := issueManagerCapabilityToken(acpEndpoint, agentID)
		if err != nil {
			log.Printf("Warning: ACP capability token auto-issue failed: %v", err)
		} else {
			ct = issued
			log.Printf("ACP capability token auto-issued for %s", agentID)
		}
	}

	var acpClient *sageagents.ACPClient
	if ct != "" {
		acpClient = &sageagents.ACPClient{
			ServerURL:  acpEndpoint,
			AgentID:    agentID,
			PrivateKey: orchestrator.PrivateKey,
			CT:         ct,
		}
		log.Printf("ACP client ready — admission control active")
	} else {
		log.Printf("Warning: no capability token found (ACP_CAPABILITY_TOKEN or ACP_CT_FILE) — admission checks skipped until token is loaded")
	}

	// ── MCP connectivity check ───────────────────────────────────────────────
	if err := orchestrator.ConnectToMCP(); err != nil {
		log.Printf("MCP not reachable at startup — will retry on first use")
	} else {
		log.Printf("Connected to MCP at %s", mcpEndpoint)
	}

	// ── Redis ────────────────────────────────────────────────────────────────
	rc := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: os.Getenv("REDIS_PASSWORD"),
	})
	if err := rc.Ping(ctx).Err(); err != nil {
		log.Fatalf("Redis not reachable at %s: %v", redisAddr, err)
	}
	log.Printf("Connected to Redis at %s", redisAddr)
	sessionTTLHours := 168
	if v := os.Getenv("SAGE_SESSION_TTL_HOURS"); v != "" {
		if n, err := fmt.Sscanf(v, "%d", &sessionTTLHours); err != nil || n != 1 {
			sessionTTLHours = 168
		}
	}
	chatSessions := sageagents.NewRedisChatSessionStore(rc, sessionTTLHours)
	chatActiveTasks := newChatActiveTaskStore(rc, sessionTTLHours)
	dispatchSessions := sageagents.NewRedisSessionStore(rc, sessionTTLHours)
	var workContexts *sageagents.WorkContextStore
	if managerEnvBool("SAGE_WORK_CONTEXT_ENABLED", true) {
		workContextTTLHours := envIntOr("SAGE_WORK_CONTEXT_TTL_HOURS", sessionTTLHours)
		workContexts = sageagents.NewRedisWorkContextStore(
			rc,
			workContextTTLHours,
			envIntOr("SAGE_WORK_CONTEXT_MAX_EVENTS", 500),
			envIntOr("SAGE_WORK_CONTEXT_MAX_EVENT_BYTES", 12000),
		)
		log.Printf("Agent Work Context ENABLED (ttl=%dh)", workContextTTLHours)
	} else {
		log.Printf("Agent Work Context disabled — set SAGE_WORK_CONTEXT_ENABLED=true to enable")
	}

	// ── Error bus ────────────────────────────────────────────────────────────
	pub := &sageagents.ErrorPublisher{Client: rc}
	orchestrator.StartErrorSubscriber(ctx, rc)

	// ── Sage front-of-house (AGT-sage) ───────────────────────────────────────
	// Optional, gated by SAGE_FRONT_OF_HOUSE_ENABLED. When on, inbound tasks
	// with capability acp:cap:skill.sage-front-of-house are routed through
	// AGT-sage (SOUL.md voice + multi-turn memory). She delegates to the
	// orchestrator via the call_orchestrator local tool. When off, non-chat
	// dispatch can still route directly; Sage Auto chat fails loudly rather
	// than returning manager prose as Sage.
	var sageRunner *sageagents.SageRunner
	if os.Getenv("SAGE_FRONT_OF_HOUSE_ENABLED") == "true" {
		sessionStore := sageagents.NewRedisSessionStore(rc, sessionTTLHours)
		sageRunner = sageagents.NewSageRunner(cfg, orchestrator, sessionStore, stateDir)
		if sageRunner.Sage != nil {
			sageRunner.Sage.SetCodexBridge(codexBridgeURL, true)
		}
		log.Printf("Sage front-of-house ENABLED (capability=%s, ttl=%dh)", sageagents.SageFrontOfHouseCapability, sessionTTLHours)
	} else {
		log.Printf("Sage front-of-house disabled — set SAGE_FRONT_OF_HOUSE_ENABLED=true to enable")
	}

	agentModelRuntime := newAgentModelRuntime(
		cfg,
		orchestrator,
		workers,
		sageRunner,
		newAgentModelOverrideStore(rc),
		stateDir,
	)
	if err := agentModelRuntime.LoadOverrides(ctx); err != nil {
		log.Printf("Warning: agent model overrides were not loaded: %v", err)
	}
	toolCatalogRuntime := newToolCatalogRuntime(
		newToolCatalogStore(rc),
		&sageagents.MCPClient{Endpoint: mcpEndpoint + "/mcp"},
	)
	skillCatalogRuntime := newSkillCatalogRuntime(stateDir)
	discoveryRuntime = newSkillDiscoveryRuntime(newSkillDiscoveryStore(rc), mcpEndpoint)
	if err := discoveryRuntime.applyStoredPolicies(ctx); err != nil {
		log.Printf("Warning: skill discovery policy restore failed: %v", err)
	}

	// ── A2A task subscriber: the highway ─────────────────────────────────────
	// Sage publishes a2a.Message on sage:tasks; we run the orchestration and
	// stream every event back on sage:events.
	go func() {
		ps := rc.Subscribe(ctx, a2a.ChannelTasks)
		defer func() {
			if err := ps.Close(); err != nil {
				log.Printf("Redis task subscription close failed: %v", err)
			}
		}()
		log.Printf("Subscribed to %s — waiting for A2A tasks from Sage", a2a.ChannelTasks)

		for {
			msg, err := ps.ReceiveMessage(ctx)
			if err != nil {
				log.Printf("Redis receive error: %v", err)
				time.Sleep(time.Second)
				continue
			}

			task, err := parseA2AMessage([]byte(msg.Payload))
			if err != nil {
				log.Printf("Bad A2A message payload: %v", err)
				continue
			}
			log.Printf("← [%s] cap=%s  content=%.60s...", task.TaskID, task.Capability, task.Content)
			go handleTask(ctx, rc, orchestrator, sageRunner, acpClient, pub, chatSessions, chatActiveTasks, dispatchSessions, workContexts, task)
		}
	}()

	// ── A2A control subscriber: continue/stop for paused tasks ──────────────
	// When the orchestrator hits the round cap and publishes input-required,
	// Sage asks the human and posts their decision via delegate_continue,
	// which lands here.
	go func() {
		ps := rc.Subscribe(ctx, a2a.ChannelControl)
		defer func() {
			if err := ps.Close(); err != nil {
				log.Printf("Redis control subscription close failed: %v", err)
			}
		}()
		log.Printf("Subscribed to %s — waiting for control messages", a2a.ChannelControl)

		for {
			msg, err := ps.ReceiveMessage(ctx)
			if err != nil {
				log.Printf("Redis receive error (control): %v", err)
				time.Sleep(time.Second)
				continue
			}
			var cm a2a.ControlMessage
			if err := json.Unmarshal([]byte(msg.Payload), &cm); err != nil {
				log.Printf("Bad control payload: %v", err)
				continue
			}
			if !continuations.deliver(cm.TaskID, cm) {
				log.Printf("control message for unknown taskId %s — dropped (decision=%s)", cm.TaskID, cm.Decision)
			}
		}
	}()

	// ── HTTP (health + debug + dashboard SSE) ────────────────────────────────
	mux := http.NewServeMux()
	mgr.RegisterRoutes(mux)
	uiFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("static UI embed invalid: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(uiFS)))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":    "ok",
			"agent":     agentID,
			"acp_ready": acpClient != nil,
		})
	})

	// /orchestrator/errors — cross-request error memory.
	registerProviderAuthRoutes(mux, stateDir, codexBridgeURL)
	mux.HandleFunc("/orchestrator/errors", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"started_at": orchestrator.ErrorsStartedAt(),
			"recent":     orchestrator.RecentErrors(50),
			"stats_15m":  orchestrator.ErrorStats(15 * time.Minute),
		})
	})

	// /dispatch — synchronous HTTP wrapper for CLI/curl testing. Internally
	// routes through the same A2A pipeline as Redis tasks: every event lands
	// on sage:events for the dashboard. Returns a JSON SageResponse for
	// backwards compatibility.
	mux.HandleFunc("/dispatch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			RequestID  string `json:"request_id"`
			SessionID  string `json:"session_id"`
			Content    string `json:"content"`
			Capability string `json:"capability"`
			Resource   string `json:"resource"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, fmt.Sprintf("bad request: %v", err), http.StatusBadRequest)
			return
		}
		task := inboundTask{
			TaskID:     body.RequestID,
			ContextID:  body.SessionID,
			Content:    body.Content,
			Capability: body.Capability,
			Resource:   body.Resource,
			Source:     "dispatch",
		}
		if task.TaskID == "" {
			task.TaskID = newTaskID()
		}
		if task.Capability == "" {
			task.Capability = "acp:cap:skill.agent-delegate"
		}
		if task.Resource == "" {
			task.Resource = "sage://workspace/*"
		}
		log.Printf("← [%s] (HTTP) cap=%s  content=%.60s...", task.TaskID, task.Capability, task.Content)

		reply := runTask(r.Context(), rc, orchestrator, sageRunner, acpClient, pub, chatSessions, chatActiveTasks, dispatchSessions, workContexts, task)
		writeJSON(w, http.StatusOK, reply)
	})

	// /agent-dispatch handles bounded peer-to-peer consultations from worker agents.
	// It bypasses the orchestrator LLM router, then enforces the configured mesh policy.
	mux.HandleFunc("/agent-dispatch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			CallerAgentID    string `json:"caller_agent_id"`
			TargetAgentID    string `json:"target_agent_id"`
			Content          string `json:"content"`
			RequestID        string `json:"request_id"`
			Reason           string `json:"reason"`
			Depth            int    `json:"depth"`
			WorkContextID    string `json:"work_context_id"`
			WorkContextToken string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.TargetAgentID == "" || req.Content == "" {
			http.Error(w, "target_agent_id and content required", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.CallerAgentID) == "" {
			http.Error(w, "caller_agent_id required", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Reason) == "" {
			http.Error(w, "reason required", http.StatusBadRequest)
			return
		}
		if !managerEnvBool("SAGE_AGENT_MESH_ENABLED", true) {
			writeJSONError(w, http.StatusForbidden, "agent mesh disabled")
			return
		}
		maxDepth := sageagents.PeerMeshMaxDepthFor(req.CallerAgentID)
		if req.Depth >= maxDepth {
			writeJSONError(w, http.StatusForbidden, fmt.Sprintf("max agent call depth (%d) reached", maxDepth))
			return
		}
		if !sageagents.IsPeerCallAllowed(req.CallerAgentID, req.TargetAgentID) {
			writeJSONError(w, http.StatusForbidden, "peer call not allowed")
			return
		}
		worker, ok := workers[req.TargetAgentID]
		if !ok || worker == nil {
			http.Error(w, "unknown agent: "+req.TargetAgentID, http.StatusNotFound)
			return
		}
		if err := worker.ConnectToMCP(); err != nil {
			http.Error(w, "MCP connect failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		ctx := r.Context()
		ctx = sageagents.WithPeerCallDepth(ctx, req.Depth+1)
		var workAccess sageagents.WorkContextAccess
		if workContexts != nil && strings.TrimSpace(req.WorkContextID) != "" && strings.TrimSpace(req.WorkContextToken) != "" {
			access, err := workContexts.AccessForToken(ctx, req.WorkContextID, req.WorkContextToken)
			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, "work context unauthorized: "+err.Error())
				return
			}
			workAccess = access
			ctx = sageagents.WithWorkContext(ctx, workContexts, access)
		}
		sageagents.AppendWorkContextEvent(ctx, "peer_request", req.CallerAgentID, "Peer consultation requested", req.Content, map[string]interface{}{
			"target_agent_id": req.TargetAgentID,
			"reason":          req.Reason,
			"depth":           req.Depth,
		})
		result, err := worker.Chat(ctx, peerDispatchMessages(worker, req.CallerAgentID, req.Reason, req.Content, workAccess))
		if err != nil {
			sageagents.AppendWorkContextEvent(ctx, "peer_response", req.TargetAgentID, "Peer consultation failed", err.Error(), map[string]interface{}{
				"caller_agent_id": req.CallerAgentID,
				"reason":          req.Reason,
				"depth":           req.Depth + 1,
			})
			writeJSON(w, http.StatusOK, map[string]string{"error": err.Error()})
			return
		}
		sageagents.AppendWorkContextEvent(ctx, "peer_response", req.TargetAgentID, "Peer consultation completed", result, map[string]interface{}{
			"caller_agent_id": req.CallerAgentID,
			"reason":          req.Reason,
			"depth":           req.Depth + 1,
			"tool_calls":      worker.LastToolTrace(),
		})
		writeJSON(w, http.StatusOK, map[string]string{"reply": result, "agent": req.TargetAgentID})
	})

	// /agents/list — returns IDs of all registered worker agents.
	mux.HandleFunc("/agents/list", func(w http.ResponseWriter, r *http.Request) {
		ids := make([]string, 0, len(workers))
		for id := range workers {
			ids = append(ids, id)
		}
		writeJSON(w, http.StatusOK, ids)
	})

	mux.HandleFunc("/agents/catalog", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, cfg.BuildChatCatalog())
	})

	registerAgentModelRoutes(mux, agentModelRuntime)
	registerToolCatalogRoutes(mux, toolCatalogRuntime)
	registerSkillCatalogRoutes(mux, skillCatalogRuntime)
	registerSkillDiscoveryRoutes(mux, discoveryRuntime)

	registerChatSessionRoutes(mux, chatSessions, cfg)
	registerWorkContextRoutes(mux, workContexts)
	registerWorkspaceRoutes(mux)

	// /chat is the browser bridge for local or same-host dashboard requests.
	// It publishes the user message to sage:tasks so the normal A2A pipeline
	// handles execution and event streaming.
	mux.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		if !isAllowedChatRequest(r) {
			http.Error(w, "chat requests must originate from this host", http.StatusForbidden)
			return
		}

		var body struct {
			ContextID     string `json:"contextId"`
			Content       string `json:"content"`
			AgentMode     string `json:"agentMode"`
			TargetAgentID string `json:"targetAgentId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.Content) == "" {
			http.Error(w, "content required", http.StatusBadRequest)
			return
		}
		if body.ContextID == "" {
			body.ContextID = newContextID()
		}

		effectiveMode := strings.TrimSpace(body.AgentMode)
		effectiveTarget := strings.TrimSpace(body.TargetAgentID)
		if effectiveMode == "" && chatSessions != nil {
			if session, err := chatSessions.Ensure(r.Context(), body.ContextID, ""); err == nil {
				effectiveMode = session.AgentMode
				effectiveTarget = session.TargetAgentID
			}
		}
		selection, err := cfg.ResolveChatSelection(effectiveMode, effectiveTarget)
		if err != nil {
			http.Error(w, "invalid chat mode: "+err.Error(), http.StatusBadRequest)
			return
		}

		if active, ok := chatActiveTasks.get(r.Context(), body.ContextID); ok && active.ActiveRunID != "" && continuations.exists(active.ActiveRunID) {
			userMessageID := newChatMessageID()
			if chatSessions != nil {
				if err := chatSessions.AppendMessage(r.Context(), body.ContextID, sageagents.ChatTranscriptMessage{
					ID:        userMessageID,
					Role:      "user",
					Text:      body.Content,
					CreatedAt: time.Now().UnixMilli(),
					TaskID:    active.ActiveRunID,
				}); err != nil {
					log.Printf("[chat-sessions] append continuation user %s failed: %v", body.ContextID, err)
				}
			}
			chatActiveTasks.markRunRunning(r.Context(), body.ContextID, active.ActiveTaskID, active.ActiveRunID)
			cm := a2a.ControlMessage{TaskID: active.ActiveRunID, Decision: "continue", Note: body.Content}
			payload, err := json.Marshal(cm)
			if err != nil {
				http.Error(w, "marshal failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
			if err := rc.Publish(r.Context(), a2a.ChannelControl, payload).Err(); err != nil {
				http.Error(w, "publish failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
			log.Printf("<- [%s] (chat continuation) contextId=%s activeTaskId=%s content=%.60s...", active.ActiveRunID, body.ContextID, active.ActiveTaskID, body.Content)
			writeJSON(w, http.StatusOK, map[string]string{
				"taskId":       active.ActiveRunID,
				"contextId":    body.ContextID,
				"activeTaskId": active.ActiveTaskID,
				"continued":    "true",
			})
			return
		}

		activeTaskID := newActiveTaskID()
		if active, ok := chatActiveTasks.get(r.Context(), body.ContextID); ok && active.ActiveTaskID != "" && active.TaskState != activeTaskStateCompleted {
			activeTaskID = active.ActiveTaskID
		}
		taskID := newTaskID()
		userMessageID := newChatMessageID()
		if chatActiveTasks != nil {
			if _, err := chatActiveTasks.startRun(r.Context(), chatActiveTaskPointer{
				ContextID:    body.ContextID,
				ActiveTaskID: activeTaskID,
			}, taskID, body.Content, userMessageID); err != nil {
				log.Printf("[active-task] start run %s failed: %v", taskID, err)
			}
		}
		if chatSessions != nil {
			if err := chatSessions.AppendMessage(r.Context(), body.ContextID, sageagents.ChatTranscriptMessage{
				ID:        userMessageID,
				Role:      "user",
				Text:      body.Content,
				CreatedAt: time.Now().UnixMilli(),
				TaskID:    taskID,
			}); err != nil {
				log.Printf("[chat-sessions] append user %s failed: %v", body.ContextID, err)
			}
			if err := chatSessions.UpsertTaskMessage(r.Context(), body.ContextID, sageagents.ChatTranscriptMessage{
				ID:        newChatMessageID(),
				Role:      "assistant",
				Text:      "",
				CreatedAt: time.Now().UnixMilli(),
				Status:    "pending",
				TaskID:    taskID,
			}); err != nil {
				log.Printf("[chat-sessions] append pending assistant %s failed: %v", body.ContextID, err)
			}
		}
		msg := a2a.Message{
			Kind:      "message",
			MessageID: newTaskID(),
			TaskID:    taskID,
			ContextID: body.ContextID,
			Role:      "user",
			Parts:     []a2a.Part{{Kind: "text", Text: body.Content}},
			Metadata: map[string]interface{}{
				"capability":      "acp:cap:skill.agent-delegate",
				"resource":        "sage://workspace/*",
				"source":          "local-chat",
				"agent_mode":      selection.AgentMode,
				"target_agent_id": selection.TargetAgentID,
				"mode_label":      selection.Label,
				"active_task_id":  activeTaskID,
			},
		}
		payload, err := json.Marshal(msg)
		if err != nil {
			http.Error(w, "marshal failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := rc.Publish(r.Context(), a2a.ChannelTasks, payload).Err(); err != nil {
			if chatSessions != nil {
				if writeErr := chatSessions.UpsertTaskMessage(r.Context(), body.ContextID, sageagents.ChatTranscriptMessage{
					Role:      "assistant",
					Text:      "publish failed: " + err.Error(),
					Status:    "error",
					TaskID:    taskID,
					CreatedAt: time.Now().UnixMilli(),
				}); writeErr != nil {
					log.Printf("[chat-sessions] write publish failure %s failed: %v", body.ContextID, writeErr)
				}
			}
			http.Error(w, "publish failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("<- [%s] (chat) contextId=%s mode=%s target=%s content=%.60s...", taskID, body.ContextID, selection.AgentMode, selection.TargetAgentID, body.Content)
		writeJSON(w, http.StatusOK, map[string]string{
			"taskId":       taskID,
			"contextId":    body.ContextID,
			"activeTaskId": activeTaskID,
		})
	})

	// /chat/continue routes continue/stop choices for input-required tasks.
	mux.HandleFunc("/chat/continue", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		if !isAllowedChatRequest(r) {
			http.Error(w, "chat requests must originate from this host", http.StatusForbidden)
			return
		}
		var body struct {
			TaskID   string `json:"taskId"`
			Decision string `json:"decision"`
			Note     string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.TaskID == "" || (body.Decision != "continue" && body.Decision != "stop") {
			http.Error(w, "taskId and decision (continue|stop) required", http.StatusBadRequest)
			return
		}
		if active, ok := chatActiveTasks.resolveRun(r.Context(), body.TaskID); ok {
			if body.Decision == "stop" {
				chatActiveTasks.markRunStopped(r.Context(), active.ContextID, active.ActiveTaskID, body.TaskID)
			} else {
				chatActiveTasks.markRunRunning(r.Context(), active.ContextID, active.ActiveTaskID, body.TaskID)
			}
		}
		cm := a2a.ControlMessage{TaskID: body.TaskID, Decision: body.Decision, Note: body.Note}
		payload, err := json.Marshal(cm)
		if err != nil {
			http.Error(w, "marshal failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := rc.Publish(r.Context(), a2a.ChannelControl, payload).Err(); err != nil {
			http.Error(w, "publish failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("<- [%s] (chat/continue) decision=%s", body.TaskID, body.Decision)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// /chat/stop cancels a currently running local chat task.
	mux.HandleFunc("/chat/stop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		if !isAllowedChatRequest(r) {
			http.Error(w, "chat requests must originate from this host", http.StatusForbidden)
			return
		}
		var body struct {
			TaskID string `json:"taskId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.TaskID == "" {
			http.Error(w, "taskId required", http.StatusBadRequest)
			return
		}
		if activeTasks.cancel(body.TaskID) {
			if active, ok := chatActiveTasks.resolveRun(r.Context(), body.TaskID); ok {
				chatActiveTasks.markRunStopped(r.Context(), active.ContextID, active.ActiveTaskID, body.TaskID)
			}
			log.Printf("<- [%s] (chat/stop) cancel requested", body.TaskID)
			writeJSON(w, http.StatusOK, map[string]string{"status": "canceling"})
			return
		}
		http.Error(w, "task is not active", http.StatusNotFound)
	})

	// /stream — SSE endpoint forwarding sage:events + sage:errors verbatim to
	// the browser. The Sage dashboard connects here for real-time activity.
	// Each SSE data line is the raw JSON payload from Redis. The frontend
	// uses the event "kind" (status-update, artifact-update) and the channel
	// (event vs error) to route rendering.
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		sub := rc.Subscribe(r.Context(), a2a.ChannelEvents, sageagents.ChannelErrors)
		defer func() {
			if err := sub.Close(); err != nil {
				log.Printf("Redis stream subscription close failed: %v", err)
			}
		}()

		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case msg, ok := <-sub.Channel():
				if !ok {
					return
				}
				// Wrap the payload with its source channel so the dashboard
				// can route status-update vs artifact-update vs error.
				envelope, err := json.Marshal(map[string]string{
					"channel": msg.Channel,
					"payload": msg.Payload,
				})
				if err != nil {
					log.Printf("SSE envelope marshal failed: %v", err)
					return
				}
				if _, err := fmt.Fprintf(w, "data: %s\n\n", envelope); err != nil {
					log.Printf("SSE write failed: %v", err)
					return
				}
				flusher.Flush()
			case <-ticker.C:
				if _, err := fmt.Fprintf(w, ": keepalive\n\n"); err != nil {
					log.Printf("SSE keepalive write failed: %v", err)
					return
				}
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})

	corsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mux.ServeHTTP(w, r)
	})

	log.Printf("Manager listening on %s", listenAddr)
	if err := http.ListenAndServe(listenAddr, corsHandler); err != nil {
		log.Fatalf("HTTP server error: %v", err)
	}
}

func registerChatSessionRoutes(mux *http.ServeMux, store *sageagents.RedisChatSessionStore, cfg *sageagents.AgentsConfig) {
	mux.HandleFunc("/chat/sessions", func(w http.ResponseWriter, r *http.Request) {
		if !isAllowedChatRequest(r) {
			http.Error(w, "chat requests must originate from this host", http.StatusForbidden)
			return
		}
		if store == nil {
			http.Error(w, "chat session store unavailable", http.StatusServiceUnavailable)
			return
		}
		switch r.Method {
		case http.MethodGet:
			sessions, err := store.List(r.Context(), 100)
			if err != nil {
				http.Error(w, "session list failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"sessions": sessions})
		case http.MethodPost:
			var body struct {
				ContextID string `json:"contextId"`
				Title     string `json:"title"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			if strings.TrimSpace(body.ContextID) == "" {
				body.ContextID = newContextID()
			}
			if strings.TrimSpace(body.Title) == "" {
				body.Title = "New chat"
			}
			session, err := store.Ensure(r.Context(), body.ContextID, body.Title)
			if err != nil {
				http.Error(w, "session create failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, session)
		default:
			http.Error(w, "GET or POST only", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/chat/sessions/", func(w http.ResponseWriter, r *http.Request) {
		if !isAllowedChatRequest(r) {
			http.Error(w, "chat requests must originate from this host", http.StatusForbidden)
			return
		}
		if store == nil {
			http.Error(w, "chat session store unavailable", http.StatusServiceUnavailable)
			return
		}
		contextID := strings.TrimPrefix(r.URL.Path, "/chat/sessions/")
		contextID = strings.Trim(contextID, "/")
		if contextID == "" {
			http.Error(w, "contextId required", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodGet:
			detail, err := store.Get(r.Context(), contextID)
			if err != nil {
				status := http.StatusInternalServerError
				if errors.Is(err, redis.Nil) {
					status = http.StatusNotFound
				}
				http.Error(w, "session load failed: "+err.Error(), status)
				return
			}
			writeJSON(w, http.StatusOK, detail)
		case http.MethodPatch:
			var body struct {
				Title         *string `json:"title"`
				AgentMode     string  `json:"agentMode"`
				TargetAgentID string  `json:"targetAgentId"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			var (
				session sageagents.ChatSessionSummary
				err     error
			)
			if body.Title != nil {
				session, err = store.UpdateTitle(r.Context(), contextID, *body.Title)
				if err != nil {
					http.Error(w, "session update failed: "+err.Error(), http.StatusInternalServerError)
					return
				}
			}
			if strings.TrimSpace(body.AgentMode) != "" {
				selection, err := cfg.ResolveChatSelection(body.AgentMode, body.TargetAgentID)
				if err != nil {
					http.Error(w, "invalid chat mode: "+err.Error(), http.StatusBadRequest)
					return
				}
				session, err = store.UpdateMode(r.Context(), contextID, selection)
				if err != nil {
					http.Error(w, "session mode update failed: "+err.Error(), http.StatusInternalServerError)
					return
				}
			}
			if session.ID == "" {
				session, err = store.Ensure(r.Context(), contextID, "")
				if err != nil {
					http.Error(w, "session update failed: "+err.Error(), http.StatusInternalServerError)
					return
				}
			}
			writeJSON(w, http.StatusOK, session)
		case http.MethodDelete:
			if err := store.Delete(r.Context(), contextID); err != nil {
				http.Error(w, "session delete failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		default:
			http.Error(w, "GET, PATCH or DELETE only", http.StatusMethodNotAllowed)
		}
	})
}

func registerWorkContextRoutes(mux *http.ServeMux, store *sageagents.WorkContextStore) {
	mux.HandleFunc("/work-context/by-task/", func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			http.Error(w, "work context store unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}
		taskID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/work-context/by-task/"), "/")
		if taskID == "" {
			http.Error(w, "taskId required", http.StatusBadRequest)
			return
		}
		access, err := store.ResolveByTaskWithToken(r.Context(), taskID, bearerToken(r))
		if err != nil {
			http.Error(w, "unauthorized: "+err.Error(), http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"workContextId": access.ID,
			"taskId":        access.TaskID,
			"contextId":     access.ContextID,
		})
	})

	mux.HandleFunc("/work-context/", func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			http.Error(w, "work context store unavailable", http.StatusServiceUnavailable)
			return
		}
		rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/work-context/"), "/")
		if rest == "" {
			http.Error(w, "workContextId required", http.StatusBadRequest)
			return
		}
		parts := strings.Split(rest, "/")
		workContextID := parts[0]
		if err := store.Authorize(r.Context(), workContextID, bearerToken(r)); err != nil {
			http.Error(w, "unauthorized: "+err.Error(), http.StatusUnauthorized)
			return
		}

		if len(parts) == 2 && parts[1] == "events" {
			if r.Method != http.MethodPost {
				http.Error(w, "POST only", http.StatusMethodNotAllowed)
				return
			}
			var body struct {
				Kind     string                 `json:"kind"`
				Actor    string                 `json:"actor"`
				Summary  string                 `json:"summary"`
				Content  string                 `json:"content"`
				Metadata map[string]interface{} `json:"metadata"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			if strings.TrimSpace(body.Kind) == "" || strings.TrimSpace(body.Summary) == "" {
				http.Error(w, "kind and summary required", http.StatusBadRequest)
				return
			}
			if strings.TrimSpace(body.Actor) == "" {
				body.Actor = "mcp"
			}
			event, err := store.AppendEvent(r.Context(), workContextID, sageagents.WorkContextEvent{
				Kind:     body.Kind,
				Actor:    body.Actor,
				Summary:  body.Summary,
				Content:  body.Content,
				Metadata: body.Metadata,
			})
			if err != nil {
				http.Error(w, "append failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, event)
			return
		}
		if len(parts) > 1 {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}
		filter := sageagents.WorkContextFilter{
			Limit: parseInt64Query(r, "limit", 100),
			Kind:  r.URL.Query().Get("kind"),
			Actor: r.URL.Query().Get("actor"),
		}
		detail, err := store.Read(r.Context(), workContextID, filter)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, redis.Nil) {
				status = http.StatusNotFound
			}
			http.Error(w, "work context read failed: "+err.Error(), status)
			return
		}
		writeJSON(w, http.StatusOK, detail)
	})
}

// registerWorkspaceRoutes registers endpoints for read-only browsing and
// downloading files from a configured local root. The default Docker compose
// setup points this at the host code directory, while SOUL loading keeps using
// the narrower workspace mount.
func registerWorkspaceRoutes(mux *http.ServeMux) {
	fileBrowserRoot := sageagents.GetEnvOr("SAGE_FILE_BROWSER_ROOT", sageagents.GetEnvOr("SAGE_HOST_WORKSPACE", "/home/node/.openclaw/workspace"))
	absRoot, err := filepath.Abs(fileBrowserRoot)
	if err != nil {
		log.Printf("file browser root %q invalid: %v", fileBrowserRoot, err)
		absRoot = fileBrowserRoot
	}

	// /workspace/files/list - List files below the configured browser root.
	mux.HandleFunc("/workspace/files/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}

		dir := strings.TrimSpace(r.URL.Query().Get("dir"))
		if dir == "" {
			dir = "."
		}

		dir = cleanBrowserPath(dir)
		if dir == "" {
			http.Error(w, "invalid directory", http.StatusBadRequest)
			return
		}

		fullPath, ok := resolveBrowserPath(absRoot, dir)
		if !ok {
			http.Error(w, "access denied", http.StatusForbidden)
			return
		}

		entries, err := os.ReadDir(fullPath)
		if err != nil {
			http.Error(w, "read directory failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		files := make([]map[string]interface{}, 0, len(entries))
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				continue
			}

			isDir := entry.IsDir()
			relPath := filepath.Join(dir, entry.Name())
			relPath = filepath.ToSlash(relPath)

			files = append(files, map[string]interface{}{
				"name":    entry.Name(),
				"path":    relPath,
				"isDir":   isDir,
				"size":    info.Size(),
				"modTime": info.ModTime().Unix(),
			})
		}

		// Sort by name
		sort.Slice(files, func(i, j int) bool {
			return files[i]["name"].(string) < files[j]["name"].(string)
		})

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"files":     files,
			"path":      dir,
			"rootLabel": filepath.Base(absRoot),
		})
	})

	// /workspace/files/download - Download a file or directory as zip
	mux.HandleFunc("/workspace/files/download", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}

		path := strings.TrimSpace(r.URL.Query().Get("path"))
		if path == "" {
			http.Error(w, "path required", http.StatusBadRequest)
			return
		}

		path = cleanBrowserPath(path)
		if path == "" || path == "." {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		fullPath, ok := resolveBrowserPath(absRoot, path)
		if !ok {
			http.Error(w, "access denied", http.StatusForbidden)
			return
		}

		info, err := os.Stat(fullPath)
		if err != nil {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}

		if !info.IsDir() {
			// Single file download
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", info.Name()))
			http.ServeFile(w, r, fullPath)
			return
		}

		// Directory: create a zip archive
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fmt.Sprintf("%s.zip", info.Name())))

		zipWriter := zip.NewWriter(w)

		err = filepath.Walk(fullPath, func(filePath string, fileInfo os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			relPath, err := filepath.Rel(fullPath, filePath)
			if err != nil {
				return err
			}
			relPath = filepath.ToSlash(relPath)

			if fileInfo.IsDir() {
				return nil
			}

			file, err := os.Open(filePath)
			if err != nil {
				return err
			}

			zipFileInfo, err := zip.FileInfoHeader(fileInfo)
			if err != nil {
				if closeErr := file.Close(); closeErr != nil {
					log.Printf("workspace download file close failed: %v", closeErr)
				}
				return err
			}
			zipFileInfo.Name = relPath

			zipFile, err := zipWriter.CreateHeader(zipFileInfo)
			if err != nil {
				if closeErr := file.Close(); closeErr != nil {
					log.Printf("workspace download file close failed: %v", closeErr)
				}
				return err
			}

			_, copyErr := io.Copy(zipFile, file)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			return closeErr
		})

		if err != nil {
			log.Printf("Error creating zip: %v", err)
			return
		}
		if err := zipWriter.Close(); err != nil {
			log.Printf("Error finalizing zip: %v", err)
		}
	})
}

func cleanBrowserPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "."
	}
	if strings.HasPrefix(path, "/") || strings.Contains(path, "\\") {
		return ""
	}
	path = filepath.Clean(path)
	if path == string(filepath.Separator) || filepath.IsAbs(path) {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, part := range parts {
		if part == ".." {
			return ""
		}
	}
	return filepath.ToSlash(path)
}

func resolveBrowserPath(absRoot, relPath string) (string, bool) {
	fullPath := filepath.Join(absRoot, filepath.FromSlash(relPath))
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", false
	}
	if absPath == absRoot {
		return absPath, true
	}
	withSep := strings.TrimRight(absRoot, string(filepath.Separator)) + string(filepath.Separator)
	return absPath, strings.HasPrefix(absPath, withSep)
}

func bearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[len("Bearer "):])
	}
	return ""
}

func issueManagerCapabilityToken(acpEndpoint, agentID string) (string, error) {
	body := map[string]interface{}{
		"sub": agentID,
		"cap": []string{
			"acp:cap:skill.agent-delegate",
			sageagents.SageFrontOfHouseCapability,
		},
		"resource": "sage://workspace/*",
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(acpEndpoint, "/")+"/acp/v1/tokens", strings.NewReader(string(payload)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("ACP token response body close failed: %v", err)
		}
	}()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read ACP token response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("ACP token issue returned %d: %s", resp.StatusCode, respBody)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.Token) == "" {
		return "", fmt.Errorf("ACP token issue returned empty token")
	}
	return strings.TrimSpace(out.Token), nil
}

func parseInt64Query(r *http.Request, key string, def int64) int64 {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return def
	}
	var out int64
	if _, err := fmt.Sscanf(raw, "%d", &out); err != nil || out <= 0 {
		return def
	}
	return out
}

func isAllowedChatRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}

	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}

	originURL, err := http.NewRequest(http.MethodGet, origin, nil)
	if err != nil {
		return false
	}

	reqHost := r.Host
	if h, _, err := net.SplitHostPort(r.Host); err == nil {
		reqHost = h
	}
	originHost := originURL.URL.Hostname()
	return strings.EqualFold(originHost, reqHost)
}

func appendAssistantChatTranscript(
	ctx context.Context,
	store *sageagents.RedisChatSessionStore,
	task inboundTask,
	reply SageResponse,
) {
	if store == nil || task.Source != "local-chat" || strings.TrimSpace(task.ContextID) == "" {
		return
	}
	status := "done"
	text := reply.Content
	if reply.Status != "ok" {
		status = "error"
		text = reply.Error
	}
	if strings.TrimSpace(text) == "" {
		return
	}
	if err := store.UpsertTaskMessage(ctx, task.ContextID, sageagents.ChatTranscriptMessage{
		ID:        newChatMessageID(),
		Role:      "assistant",
		Text:      text,
		CreatedAt: time.Now().UnixMilli(),
		Status:    status,
		TaskID:    task.TaskID,
	}); err != nil {
		log.Printf("[chat-sessions] append assistant %s failed: %v", task.ContextID, err)
	}
}

func registerInProcessAgents(ctx context.Context, mgr *sageagents.Manager, orchestrator *sageagents.SageOrchestratorAgent, workers map[string]*sageagents.CopilotAgent) {
	refresh := func() {
		if orchestrator != nil {
			mgr.RegisterAgent(inProcessOrchestratorMetadata(orchestrator.AgentID))
		}
		for _, worker := range workers {
			if worker != nil {
				mgr.RegisterAgent(inProcessWorkerMetadata(worker))
			}
		}
	}

	refresh()
	log.Printf("Registered %d in-process workers with manager health registry", len(workers))

	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refresh()
			}
		}
	}()
}

func inProcessOrchestratorMetadata(agentID string) sageagents.AgentMetadata {
	return sageagents.AgentMetadata{
		AgentID:      agentID,
		Endpoint:     "in-process",
		Capabilities: []string{"orchestrator", "sage-manager"},
		LastSeen:     time.Now().Unix(),
	}
}

func inProcessWorkerMetadata(worker *sageagents.CopilotAgent) sageagents.AgentMetadata {
	meta := worker.Metadata()
	meta.Endpoint = "in-process"
	meta.Capabilities = append(append([]string{}, meta.Capabilities...), worker.AllowedTools...)
	meta.LastSeen = time.Now().Unix()
	return meta
}

func peerDispatchMessages(worker *sageagents.CopilotAgent, callerAgentID, reason, query string, access sageagents.WorkContextAccess) []sageagents.ChatMessage {
	prompt := "You are answering a bounded peer consultation from " + strings.TrimSpace(callerAgentID) + ".\n" +
		"Reason: " + strings.TrimSpace(reason) + "\n" +
		"Stay inside your domain authority, provide concrete facts or pushback, and do not take ownership of the whole task."
	if access.ID != "" && access.Token != "" {
		prompt += "\n\n" + sageagents.BuildWorkContextWorkerPrompt(access)
	}
	if worker != nil && strings.TrimSpace(worker.SystemPrompt) != "" {
		prompt = strings.TrimSpace(worker.SystemPrompt) + "\n\n" + prompt
	}
	return []sageagents.ChatMessage{
		{Role: "system", Content: prompt},
		{Role: "user", Content: query},
	}
}

func toolNameSet(catalog []sageagents.ToolDefinition) map[string]struct{} {
	set := make(map[string]struct{}, len(catalog))
	for _, tool := range catalog {
		name := strings.TrimSpace(tool.Function.Name)
		if name != "" {
			set[name] = struct{}{}
		}
	}
	return set
}

func filterAllowedMCPTools(agentID string, allowed []string, available map[string]struct{}) []string {
	filtered := make([]string, 0, len(allowed))
	for _, name := range allowed {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if (name == "call_agent" || name == "list_agents") && (!managerEnvBool("SAGE_AGENT_MESH_ENABLED", true) || !sageagents.AgentCanCallPeers(agentID)) {
			continue
		}
		if len(available) > 0 {
			if _, ok := available[name]; !ok {
				log.Printf("Warning: agent %s references unavailable MCP tool %s", agentID, name)
				continue
			}
		}
		filtered = append(filtered, name)
	}
	return filtered
}

func managerEnvBool(key string, def bool) bool {
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

func shouldUseDispatchRollingContext(task inboundTask, useSage bool) bool {
	if useSage || strings.TrimSpace(task.ContextID) == "" {
		return false
	}
	source := strings.TrimSpace(strings.ToLower(task.Source))
	return source == "" || source == "dispatch"
}

func truncateDispatchText(s string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	trimmed := strings.TrimSpace(s)
	if len(trimmed) <= maxChars {
		return trimmed
	}
	return trimmed[:maxChars] + "..."
}

func buildDispatchRollingInput(
	ctx context.Context,
	store sageagents.SessionStore,
	contextID string,
	currentInput string,
) string {
	if store == nil || strings.TrimSpace(contextID) == "" {
		return currentInput
	}
	prior := store.Load(ctx, contextID)
	if len(prior) == 0 {
		return currentInput
	}
	maxMessages := envIntOr("MANAGER_DISPATCH_ROLLING_MAX_MESSAGES", 12)
	if maxMessages <= 0 {
		return currentInput
	}
	start := len(prior) - maxMessages
	if start < 0 {
		start = 0
	}
	maxChars := envIntOr("MANAGER_DISPATCH_ROLLING_MAX_CHARS", 700)
	var b strings.Builder
	b.WriteString("Recent rolling context for continuity:\n")
	added := 0
	for _, msg := range prior[start:] {
		role := strings.TrimSpace(strings.ToLower(msg.Role))
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		switch role {
		case "assistant":
			b.WriteString("Manager: ")
		case "user":
			b.WriteString("User: ")
		default:
			continue
		}
		b.WriteString(truncateDispatchText(content, maxChars))
		b.WriteString("\n")
		added++
	}
	if added == 0 {
		return currentInput
	}
	b.WriteString("\nCurrent request:\n")
	b.WriteString(strings.TrimSpace(currentInput))
	return b.String()
}

func persistDispatchRollingTurn(
	ctx context.Context,
	store sageagents.SessionStore,
	task inboundTask,
	userInput string,
	reply SageResponse,
	useSage bool,
) {
	if store == nil || useSage || strings.TrimSpace(task.ContextID) == "" {
		return
	}
	source := strings.TrimSpace(strings.ToLower(task.Source))
	if source != "" && source != "dispatch" {
		return
	}
	userText := strings.TrimSpace(userInput)
	if userText != "" {
		store.Append(ctx, task.ContextID, sageagents.ChatMessage{Role: "user", Content: userText})
	}
	assistantText := strings.TrimSpace(reply.Content)
	if assistantText == "" {
		assistantText = strings.TrimSpace(reply.Error)
	}
	if assistantText != "" {
		store.Append(ctx, task.ContextID, sageagents.ChatMessage{Role: "assistant", Content: assistantText})
	}
	store.Trim(ctx, task.ContextID, envIntOr("MANAGER_DISPATCH_ROLLING_MAX_MESSAGES", 12))
}

func runSageAutoManagerExecution(
	ctx context.Context,
	sage *sageagents.SageRunner,
	orch *sageagents.SageOrchestratorAgent,
	task inboundTask,
	orchestrationInput string,
	tracker *sageagents.HandoffTracker,
) (string, error) {
	log.Printf("  Manager executing Sage Auto [%s] via orchestrator input=%.120q", task.TaskID, orchestrationInput)
	sageagents.EmitProgress(ctx, sageagents.ProgressEvent{
		Type:        "route",
		Agent:       sageagents.SageAgentID,
		Tool:        "call_orchestrator",
		Phase:       "start",
		Mode:        "delegate",
		RouteReason: "sage_auto_manager_execution",
		Timestamp:   time.Now().Unix(),
	})
	start := time.Now()
	rawResult, execErr := orch.Orchestrate(ctx, orchestrationInput, tracker)
	sageagents.EmitLatencySpan(ctx, "orchestrator_total", start)
	emitSageAutoManagerExecutionEnd(ctx, rawResult, execErr, start)
	return finalizeSageAutoManagerExecution(ctx, sage, orch, task, rawResult, execErr, tracker)
}

func resumeSageAutoManagerExecution(
	ctx context.Context,
	sage *sageagents.SageRunner,
	orch *sageagents.SageOrchestratorAgent,
	task inboundTask,
	prevMessages []sageagents.ChatMessage,
	originalInput, note string,
	tracker *sageagents.HandoffTracker,
) (string, error) {
	start := time.Now()
	rawResult, execErr := orch.Resume(ctx, prevMessages, tracker, originalInput, note)
	sageagents.EmitLatencySpan(ctx, "orchestrator_resume", start)
	emitSageAutoManagerExecutionEnd(ctx, rawResult, execErr, start)
	return finalizeSageAutoManagerExecution(ctx, sage, orch, task, rawResult, execErr, tracker)
}

func emitSageAutoManagerExecutionEnd(ctx context.Context, rawResult string, execErr error, start time.Time) {
	duration := time.Since(start).Round(time.Millisecond)
	event := sageagents.ProgressEvent{
		Type:        "route",
		Agent:       sageagents.SageAgentID,
		Tool:        "call_orchestrator",
		Phase:       "end",
		Mode:        "delegate",
		RouteReason: "sage_auto_manager_execution",
		DurationMS:  duration.Milliseconds(),
		Timestamp:   time.Now().Unix(),
	}
	if execErr != nil {
		event.Error = execErr.Error()
	}
	sageagents.EmitProgress(ctx, event)
	log.Printf("  Manager Sage Auto execution finished (%s) rawLen=%d err=%v", duration, len(rawResult), execErr)
}

func finalizeSageAutoManagerExecution(
	ctx context.Context,
	sage *sageagents.SageRunner,
	orch *sageagents.SageOrchestratorAgent,
	task inboundTask,
	rawResult string,
	execErr error,
	tracker *sageagents.HandoffTracker,
) (string, error) {
	var capErr *sageagents.RoundCapReachedError
	if errors.As(execErr, &capErr) {
		return rawResult, execErr
	}

	sourceResult := rawResult
	if managerEnvBool("SAGE_DELEGATED_PREFER_WORKER_REPLY", true) {
		if workerReply := strings.TrimSpace(orch.LastReply()); workerReply != "" {
			sourceResult = workerReply
		}
	}
	executionResult := sage.BuildManagerExecutionResult(task.ContextID, task.TaskID, sourceResult, execErr, tracker)
	final, finalErr := sage.CompleteAutoFromManagerResult(ctx, task.ContextID, task.Content, executionResult, tracker)
	if finalErr != nil {
		return final, finalErr
	}
	if execErr != nil {
		return final, execErr
	}
	return final, nil
}

// handleTask is the Redis-driven entry point. It runs the same pipeline as
// /dispatch via runTask, then logs and discards the response (the response
// is delivered to Sage entirely through sage:events).
func handleTask(
	ctx context.Context,
	rc *redis.Client,
	orch *sageagents.SageOrchestratorAgent,
	sage *sageagents.SageRunner,
	acp *sageagents.ACPClient,
	pub *sageagents.ErrorPublisher,
	chatSessions *sageagents.RedisChatSessionStore,
	chatActiveTasks *chatActiveTaskStore,
	dispatchSessions sageagents.SessionStore,
	workContexts *sageagents.WorkContextStore,
	task inboundTask,
) {
	runTask(ctx, rc, orch, sage, acp, pub, chatSessions, chatActiveTasks, dispatchSessions, workContexts, task)
}

// runTask is the unified orchestration pipeline used by both the Redis
// subscriber and the HTTP /dispatch endpoint. Every progress event is
// published to sage:events; the final artifact is published as a
// TaskArtifactUpdateEvent followed by a terminal TaskStatusUpdateEvent.
func runTask(
	ctx context.Context,
	rc *redis.Client,
	orch *sageagents.SageOrchestratorAgent,
	sage *sageagents.SageRunner,
	acp *sageagents.ACPClient,
	pub *sageagents.ErrorPublisher,
	chatSessions *sageagents.RedisChatSessionStore,
	chatActiveTasks *chatActiveTaskStore,
	dispatchSessions sageagents.SessionStore,
	workContexts *sageagents.WorkContextStore,
	task inboundTask,
) SageResponse {
	ctx, unregisterActiveTask := activeTasks.register(ctx, task.TaskID)
	defer unregisterActiveTask()

	reply := SageResponse{
		RequestID: task.TaskID,
		Agent:     orch.AgentID,
		Timestamp: time.Now().Unix(),
	}

	ctx = sageagents.WithErrorPublisher(ctx, pub)

	var workContext sageagents.WorkContextAccess
	if workContexts != nil {
		access, err := workContexts.Create(ctx, task.TaskID, task.ContextID, "manager")
		if err != nil {
			log.Printf("[work-context] create failed for %s: %v", task.TaskID, err)
		} else {
			workContext = access
			ctx = sageagents.WithWorkContext(ctx, workContexts, access)
			sageagents.AppendWorkContextEvent(ctx, "task_input", "manager", "Task accepted by manager", task.Content, map[string]interface{}{
				"task_id":    task.TaskID,
				"context_id": task.ContextID,
				"capability": task.Capability,
				"resource":   task.Resource,
				"source":     task.Source,
				"agent_mode": task.AgentMode,
				"target":     task.TargetAgentID,
			})
		}
	}
	workContextID := workContext.ID
	if chatActiveTasks != nil && task.Source == "local-chat" && task.ActiveTaskID != "" {
		chatActiveTasks.updatePointer(ctx, task.ContextID, task.ActiveTaskID, task.TaskID, activeTaskStateActive, activeRunStateRunning, "", workContextID)
	}

	// Publish initial state transitions immediately so the dashboard sees
	// the task before ACP and orchestration run.
	publishA2AEventWithWorkContext(rc, a2a.NewSubmittedStatus(task.TaskID, task.ContextID), workContextID)
	publishA2AEventWithWorkContext(rc, a2a.NewWorkingStatus(task.TaskID, task.ContextID, "orchestrating",
		map[string]interface{}{"activity": "start"}), workContextID)

	sink := sageagents.A2AProgressSink(rc, task.TaskID, task.ContextID, nil, workContextID)
	ctx = sageagents.WithProgressSink(ctx, sink)
	orchestrationInput := task.Content
	if discoveryRuntime != nil {
		skills, err := discoveryRuntime.searchSkills(task.Content, task.TargetAgentID, 5)
		if err != nil {
			log.Printf("  skill prefetch failed [%s]: %v", task.TaskID, err)
		} else if len(skills) > 0 {
			orchestrationInput = injectCanonicalSkills(task.Content, skills)
			sageagents.AppendWorkContextEvent(ctx, "skill_prefetch", "manager", "Canonical skills prefetched for dispatch", "", map[string]interface{}{
				"count":        len(skills),
				"target_agent": task.TargetAgentID,
			})
		}
	}

	// Heartbeat ticker — publishes a working-state event every 5s while the
	// orchestration runs. Sage's MCP delegate resets its inactivity timer on
	// every event for this task ID, so heartbeats keep the request alive
	// indefinitely as long as the manager is actually still processing.
	hbDone := make(chan struct{})
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		started := time.Now()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				elapsed := time.Since(started).Milliseconds()
				publishA2AEventWithWorkContext(rc, a2a.NewWorkingStatus(
					task.TaskID, task.ContextID,
					fmt.Sprintf("still working (%ds)", elapsed/1000),
					map[string]interface{}{
						"activity":   "heartbeat",
						"elapsed_ms": elapsed,
					},
				), workContextID)
			case <-hbDone:
				return
			}
		}
	}()
	defer close(hbDone)

	// ── ACP admission ────────────────────────────────────────────────────────
	var etID string
	if acp != nil {
		acpStart := time.Now()
		id, err := acp.RequestAdmission(task.Capability, task.Resource, map[string]interface{}{
			"content": task.Content,
		})
		sageagents.EmitLatencySpan(ctx, "acp_admission", acpStart)
		if err != nil {
			log.Printf("  ACP admission denied [%s]: %v", task.TaskID, err)
			sageagents.PublishError(ctx, sageagents.ErrorEvent{RequestID: task.TaskID, Kind: "acp_deny", Error: err.Error()})
			sageagents.AppendWorkContextEvent(ctx, "acp_admission", "manager", "ACP admission denied", err.Error(), map[string]interface{}{"status": "denied"})
			publishA2AEventWithWorkContext(rc, a2a.NewFailedStatus(task.TaskID, task.ContextID, "admission denied: "+err.Error()), workContextID)
			if workContexts != nil && workContextID != "" {
				workContexts.SetStatus(ctx, workContextID, "failed")
			}
			reply.Status = "error"
			reply.Error = "admission denied: " + err.Error()
			appendAssistantChatTranscript(ctx, chatSessions, task, reply)
			return reply
		}
		etID = id
		log.Printf("  ACP approved [%s] et=%s", task.TaskID, etID)
		sageagents.AppendWorkContextEvent(ctx, "acp_admission", "manager", "ACP admission approved", "", map[string]interface{}{"status": "approved"})
	} else {
		log.Printf("  ACP skipped (no CT) [%s]", task.TaskID)
		sageagents.AppendWorkContextEvent(ctx, "acp_admission", "manager", "ACP admission skipped", "", map[string]interface{}{"status": "skipped"})
	}

	// ── Orchestrate (with input-required pause/resume support) ──────────────
	// Dispatch path:
	//   - Sage front-of-house ON + capability matches → AGT-sage drives the
	//     entry/session context. The manager still calls the orchestrator, then
	//     hands the structured execution result back to Sage for finalization.
	//   - Otherwise → orchestrator directly (existing behavior).
	tracker := sageagents.NewHandoffTracker()
	oStart := time.Now()
	orch.ResetLastWorker()
	agentMode := strings.TrimSpace(task.AgentMode)
	if agentMode == "" {
		agentMode = sageagents.ChatModeAuto
	}
	useSage := sage != nil && agentMode == sageagents.ChatModeAuto && (task.Capability == sageagents.SageFrontOfHouseCapability || task.Source == "local-chat")
	if shouldUseDispatchRollingContext(task, useSage) {
		orchestrationInput = buildDispatchRollingInput(ctx, dispatchSessions, task.ContextID, orchestrationInput)
	}
	var (
		result   string
		err      error
		canceled bool
	)
	switch {
	case agentMode == sageagents.ChatModeSolo && task.TargetAgentID == sageagents.SageAgentID:
		if sage == nil {
			err = fmt.Errorf("Sage solo requested but Sage front-of-house is not initialized")
			break
		}
		log.Printf("  Routing [%s] to AGT-sage solo (contextID=%s)", task.TaskID, task.ContextID)
		sageagents.AppendWorkContextEvent(ctx, "route_decision", "manager", "Task routed to Sage solo", "", map[string]interface{}{"agent": sageagents.SageAgentID, "mode": agentMode})
		result, err = sage.RunSolo(ctx, task.ContextID, orchestrationInput, tracker)
	case agentMode == sageagents.ChatModeSolo:
		log.Printf("  Routing [%s] to %s solo", task.TaskID, task.TargetAgentID)
		sageagents.AppendWorkContextEvent(ctx, "route_decision", "manager", "Task routed to targeted agent solo", "", map[string]interface{}{"agent": task.TargetAgentID, "mode": agentMode})
		result, err = orch.DispatchWorker(ctx, task.TargetAgentID, orchestrationInput, tracker, sageagents.WorkerDispatchOptions{
			Mode:              sageagents.ChatModeSolo,
			SuppressPeerTools: true,
			EnforceSeniorGate: false,
		})
	case agentMode == sageagents.ChatModeLaunch:
		log.Printf("  Routing [%s] to %s launch", task.TaskID, task.TargetAgentID)
		sageagents.AppendWorkContextEvent(ctx, "route_decision", "manager", "Task launched from targeted agent", "", map[string]interface{}{"agent": task.TargetAgentID, "mode": agentMode})
		result, err = orch.DispatchWorker(ctx, task.TargetAgentID, orchestrationInput, tracker, sageagents.WorkerDispatchOptions{
			Mode:              sageagents.ChatModeLaunch,
			SuppressPeerTools: false,
			EnforceSeniorGate: true,
		})
	case useSage:
		log.Printf("  Routing [%s] through AGT-sage (contextID=%s)", task.TaskID, task.ContextID)
		sageagents.AppendWorkContextEvent(ctx, "route_decision", "manager", "Task routed through Sage front-of-house", "", map[string]interface{}{"agent": sageagents.SageAgentID})
		orchestrationInput, err = sage.BuildAutoOrchestrationInput(ctx, task.ContextID, orchestrationInput, tracker)
		if err != nil {
			break
		}
		result, err = runSageAutoManagerExecution(ctx, sage, orch, task, orchestrationInput, tracker)
	case agentMode == sageagents.ChatModeAuto && task.Source == "local-chat":
		err = fmt.Errorf("Sage Auto requires Sage front-of-house to be initialized")
	default:
		if agentMode != sageagents.ChatModeAuto {
			err = fmt.Errorf("unsupported chat mode %q", agentMode)
			break
		}
		sageagents.AppendWorkContextEvent(ctx, "route_decision", "manager", "Task routed directly to orchestrator", "", map[string]interface{}{"agent": orch.AgentID})
		result, err = orch.Orchestrate(ctx, orchestrationInput, tracker)
	}

	// Loop while the orchestrator hits its round cap and the human says continue.
	// On the first iteration this just runs Orchestrate; on subsequent iterations
	// we call Resume with the messages saved in the cap error.
	cumulativeRounds := 0
	for {
		var capErr *sageagents.RoundCapReachedError
		if !errors.As(err, &capErr) {
			break // either success or hard failure — exit the resumption loop
		}
		cumulativeRounds += capErr.Rounds
		log.Printf("  Orchestrate ⏸ [%s] cap reached at %d rounds (cumulative=%d) — awaiting human", task.TaskID, capErr.Rounds, cumulativeRounds)

		if partial := orch.LastReply(); partial != "" {
			publishA2AEventWithWorkContext(rc, a2a.NewArtifactUpdate(task.TaskID, task.ContextID, partial), workContextID)
		}
		prompt := fmt.Sprintf(
			"Manager paused after %d rounds. Want me to push another %d, or sit with what we have?",
			cumulativeRounds, capErr.Rounds,
		)
		meta := map[string]interface{}{
			"reason":           "round_cap_reached",
			"rounds_completed": cumulativeRounds,
			"prompt":           prompt,
		}
		if lw := orch.LastWorker(); lw != "" {
			meta["last_agent"] = lw
		}
		if ids := tracker.AgentIDs(); len(ids) > 0 {
			meta["trace"] = ids
		}
		if chatActiveTasks != nil && task.Source == "local-chat" && task.ActiveTaskID != "" {
			chatActiveTasks.markInputRequired(ctx, task.ContextID, task.ActiveTaskID, task.TaskID, prompt, workContextID)
		}
		sageagents.AppendWorkContextEvent(ctx, "continuation_required", "manager", "Manager paused for human continuation", prompt, meta)
		publishA2AEventWithWorkContext(rc, a2a.NewInputRequiredStatus(task.TaskID, task.ContextID, prompt, meta), workContextID)

		// Wait for the human's decision via sage:control. Default 15min cap;
		// configurable for headless / batch-runs.
		ch := continuations.register(task.TaskID)
		timeoutMs := envIntOr("SAGE_CONTINUATION_TIMEOUT_MS", 15*60*1000)
		var cm a2a.ControlMessage
		select {
		case cm = <-ch:
			continuations.unregister(task.TaskID)
		case <-ctx.Done():
			continuations.unregister(task.TaskID)
			log.Printf("  Orchestrate ⏹ [%s] canceled while awaiting human", task.TaskID)
			if chatActiveTasks != nil && task.Source == "local-chat" && task.ActiveTaskID != "" {
				chatActiveTasks.markRunCanceled(ctx, task.ContextID, task.ActiveTaskID, task.TaskID)
			}
			reply.Status = "error"
			reply.Error = "task canceled by user"
			canceled = true
			err = nil
			result = orch.LastReply()
			goto finalize
		case <-time.After(time.Duration(timeoutMs) * time.Millisecond):
			continuations.unregister(task.TaskID)
			log.Printf("  Orchestrate ⏸ [%s] no continuation decision in %dms — canceling", task.TaskID, timeoutMs)
			if chatActiveTasks != nil && task.Source == "local-chat" && task.ActiveTaskID != "" {
				chatActiveTasks.markRunCanceled(ctx, task.ContextID, task.ActiveTaskID, task.TaskID)
			}
			sageagents.AppendWorkContextEvent(ctx, "continuation_timeout", "manager", "No continuation decision received", "", map[string]interface{}{
				"rounds_completed": cumulativeRounds,
				"timeout_ms":       timeoutMs,
			})
			publishA2AEventWithWorkContext(rc, a2a.NewFailedStatusWithMeta(
				task.TaskID, task.ContextID, "user did not respond — task canceled",
				map[string]interface{}{
					"reason":           "user_did_not_respond",
					"rounds_completed": cumulativeRounds,
					"timeout_ms":       timeoutMs,
				},
			), workContextID)
			reply.Status = "error"
			reply.Error = "user did not respond"
			err = nil // suppress fall-through to hard-failure handler
			result = orch.LastReply()
			goto finalize
		}

		if cm.Decision == "stop" {
			if chatActiveTasks != nil && task.Source == "local-chat" && task.ActiveTaskID != "" {
				chatActiveTasks.markRunStopped(ctx, task.ContextID, task.ActiveTaskID, task.TaskID)
			}
			sageagents.AppendWorkContextEvent(ctx, "continuation_stop", "manager", "Human stopped task", cm.Note, nil)
			// Finalize with whatever partial work we've gathered.
			result = orch.LastReply()
			err = nil
			if useSage {
				result, err = finalizeSageAutoManagerExecution(ctx, sage, orch, task, result, nil, tracker)
			}
			log.Printf("  Orchestrate ⏹ [%s] stopped by user; finalizing with partial reply (%d chars)", task.TaskID, len(result))
			break
		}

		// Decision == "continue" — resume the loop that hit the cap. If the
		// task was driven by Sage, resume her conversation; otherwise resume
		// the orchestrator's router loop.
		log.Printf("  Orchestrate ▶ [%s] resuming on user confirmation; note=%q  (sage=%v)", task.TaskID, truncateNote(cm.Note, 80), useSage)
		if chatActiveTasks != nil && task.Source == "local-chat" && task.ActiveTaskID != "" {
			chatActiveTasks.markRunRunning(ctx, task.ContextID, task.ActiveTaskID, task.TaskID)
		}
		sageagents.AppendWorkContextEvent(ctx, "continuation_continue", "manager", "Human continued task", cm.Note, nil)
		if useSage {
			result, err = resumeSageAutoManagerExecution(ctx, sage, orch, task, capErr.Messages, orchestrationInput, cm.Note, tracker)
		} else {
			result, err = orch.Resume(ctx, capErr.Messages, tracker, orchestrationInput, cm.Note)
		}
	}

finalize:
	oDur := time.Since(oStart).Round(time.Millisecond)
	if canceled || errors.Is(err, context.Canceled) {
		log.Printf("  Orchestrate ⏹ [%s] (%s): canceled by user", task.TaskID, oDur)
		if chatActiveTasks != nil && task.Source == "local-chat" && task.ActiveTaskID != "" {
			if active, ok := chatActiveTasks.resolveRun(ctx, task.TaskID); !ok || active.RunState != activeRunStateStopped {
				chatActiveTasks.markRunCanceled(ctx, task.ContextID, task.ActiveTaskID, task.TaskID)
			}
		}
		if partial := orch.LastReply(); partial != "" {
			publishA2AEventWithWorkContext(rc, a2a.NewArtifactUpdate(task.TaskID, task.ContextID, partial), workContextID)
		}
		rawErr := "context canceled"
		if err != nil {
			rawErr = err.Error()
		}
		sageagents.AppendWorkContextEvent(ctx, "final_status", "manager", "Task canceled by user", rawErr, map[string]interface{}{"duration_ms": oDur.Milliseconds()})
		publishA2AEventWithWorkContext(rc, a2a.NewCanceledStatus(task.TaskID, task.ContextID, "task canceled by user",
			map[string]interface{}{
				"reason":      "user_canceled",
				"raw_error":   rawErr,
				"duration_ms": oDur.Milliseconds(),
			}), workContextID)
		if workContexts != nil && workContextID != "" {
			workContexts.SetStatus(ctx, workContextID, "canceled")
		}
		reply.Status = "error"
		reply.Error = "task canceled by user"
	} else if err != nil {
		log.Printf("  Orchestrate ✗ [%s] (%s): %v", task.TaskID, oDur, err)
		if chatActiveTasks != nil && task.Source == "local-chat" && task.ActiveTaskID != "" {
			chatActiveTasks.markRunFailed(ctx, task.ContextID, task.ActiveTaskID, task.TaskID)
		}
		sageagents.PublishError(ctx, sageagents.ErrorEvent{RequestID: task.TaskID, Kind: "orchestrate", Agent: orch.AgentID, Error: err.Error(), DurationMS: oDur.Milliseconds()})
		// Partial work lands BEFORE the failed status so the dashboard /
		// MCP delegate capture both.
		if partial := orch.LastReply(); partial != "" {
			publishA2AEventWithWorkContext(rc, a2a.NewArtifactUpdate(task.TaskID, task.ContextID, partial), workContextID)
		}
		failureMeta := classifyFailure(err, orch, tracker, oDur)
		sageagents.AppendWorkContextEvent(ctx, "final_status", "manager", "Task failed", err.Error(), failureMeta)
		publishA2AEventWithWorkContext(rc, a2a.NewFailedStatusWithMeta(
			task.TaskID, task.ContextID, err.Error(),
			failureMeta,
		), workContextID)
		if workContexts != nil && workContextID != "" {
			workContexts.SetStatus(ctx, workContextID, "failed")
		}
		reply.Status = "error"
		reply.Error = err.Error()
		if useSage && strings.TrimSpace(result) != "" {
			reply.Error = result
		}
	} else {
		log.Printf("  Orchestrate ✓ [%s] (%s) worker=%s replyLen=%d", task.TaskID, oDur, orch.LastWorker(), len(result))
		if chatActiveTasks != nil && task.Source == "local-chat" && task.ActiveTaskID != "" {
			chatActiveTasks.markRunCompleted(ctx, task.ContextID, task.ActiveTaskID, task.TaskID)
			chatActiveTasks.clearIfTask(ctx, task.ContextID, task.ActiveTaskID)
		}
		if result != "" {
			publishA2AEventWithWorkContext(rc, a2a.NewArtifactUpdate(task.TaskID, task.ContextID, result), workContextID)
		}
		sageagents.AppendWorkContextEvent(ctx, "final_status", "manager", "Task completed", result, map[string]interface{}{"duration_ms": oDur.Milliseconds(), "worker": orch.LastWorker()})
		publishA2AEventWithWorkContext(rc, a2a.NewCompletedStatus(task.TaskID, task.ContextID), workContextID)
		if workContexts != nil && workContextID != "" {
			workContexts.SetStatus(ctx, workContextID, "completed")
		}
		reply.Status = "ok"
		reply.Content = result
	}

	persistDispatchRollingTurn(ctx, dispatchSessions, task, task.Content, reply, useSage)

	// Consume ACP execution token after the response is published.
	if acp != nil && etID != "" {
		consumeStart := time.Now()
		if err := acp.ConsumeExecutionToken(etID); err != nil {
			log.Printf("  Warning: ET consume failed [%s]: %v", task.TaskID, err)
		}
		sageagents.EmitLatencySpan(ctx, "acp_et_consume", consumeStart)
	}

	handoffs := tracker.Handoffs()
	log.Printf("  handoff [%s]: %v", task.TaskID, tracker.AgentIDs())
	log.Printf("→ [%s] status=%s", task.TaskID, reply.Status)
	appendAssistantChatTranscript(ctx, chatSessions, task, reply)

	workerModel := orch.ActiveModel()
	if lw := orch.LastWorker(); lw != "" {
		if wa, ok := orch.Workers[lw]; ok && wa != nil {
			workerModel = wa.ActiveModel()
		}
	}
	orch.Manager.AddLog(sageagents.AgentResponseLog{
		RequestID:         task.TaskID,
		Timestamp:         reply.Timestamp,
		Agents:            tracker.AgentIDs(),
		Model:             workerModel,
		Input:             task.Content,
		Output:            reply.Content,
		Error:             reply.Error,
		ToolCalls:         orch.LastWorkerToolTrace(),
		ToolErrors:        orch.LastWorkerToolErrors(),
		Handoffs:          handoffs,
		OrchestrationPath: []string{"a2a-bus", "acp", orch.AgentID},
	})

	return reply
}

func injectCanonicalSkills(original string, skills []skillDiscoverySkill) string {
	if len(skills) == 0 {
		return original
	}
	var b strings.Builder
	b.WriteString("Canonical Skill Context (prefetched by manager):\n")
	limit := len(skills)
	if limit > 5 {
		limit = 5
	}
	for i := 0; i < limit; i++ {
		s := skills[i]
		b.WriteString(fmt.Sprintf("\n[%d] %s (%s)\n", i+1, s.CanonicalName, s.ID))
		if s.Description != "" {
			b.WriteString("- description: " + s.Description + "\n")
		}
		if len(s.Tags) > 0 {
			b.WriteString("- tags: " + strings.Join(s.Tags, ", ") + "\n")
		}
		if s.RiskLevel != "" {
			b.WriteString("- risk: " + s.RiskLevel + "\n")
		}
		if s.ExecutionType != "" {
			b.WriteString("- execution: " + s.ExecutionType + "\n")
		}
		if s.RequiresSession {
			b.WriteString("- requires_session: true\n")
		}
	}
	b.WriteString("\nUser Request:\n")
	b.WriteString(original)
	return b.String()
}

func publishA2AEventWithWorkContext(rc *redis.Client, evt interface{}, workContextID string) {
	if workContextID == "" {
		a2a.PublishEvent(rc, evt)
		return
	}
	switch typed := evt.(type) {
	case a2a.TaskStatusUpdateEvent:
		typed.Metadata = withWorkContextMetadata(typed.Metadata, workContextID)
		a2a.PublishEvent(rc, typed)
	case a2a.TaskArtifactUpdateEvent:
		typed.Metadata = withWorkContextMetadata(typed.Metadata, workContextID)
		a2a.PublishEvent(rc, typed)
	default:
		a2a.PublishEvent(rc, evt)
	}
}

func withWorkContextMetadata(meta map[string]interface{}, workContextID string) map[string]interface{} {
	if meta == nil {
		meta = map[string]interface{}{}
	}
	meta["work_context_id"] = workContextID
	return meta
}

// envIntOr is the manager-side mirror of sageagents.envInt. Reads an int
// env var with a default; invalid values fall back to def silently.
func envIntOr(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		// strconv lives in sageagents.common; use it directly here.
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// truncateNote shortens a control-message note for log lines.
func truncateNote(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// classifyFailure inspects the orchestration error and orchestrator state to
// produce structured metadata for the failed-status event. The dashboard
// renders these fields directly; Sage's MCP delegate forwards them so she
// can speak to the failure mode rather than relaying a raw error string.
func classifyFailure(
	err error,
	orch *sageagents.SageOrchestratorAgent,
	tracker *sageagents.HandoffTracker,
	dur time.Duration,
) map[string]interface{} {
	reason := "unknown"
	msg := err.Error()
	switch {
	case strings.Contains(msg, "tool loop exceeded"):
		reason = "round_cap_exceeded"
	case strings.Contains(msg, "ACP") || strings.Contains(msg, "admission"):
		reason = "acp_denied"
	case strings.Contains(msg, "context canceled") || strings.Contains(msg, "context deadline"):
		reason = "client_aborted"
	case strings.Contains(msg, "MCP") || strings.Contains(msg, "tool ") || strings.Contains(msg, "worker "):
		reason = "tool_error"
	}
	meta := map[string]interface{}{
		"reason":      reason,
		"raw_error":   msg,
		"duration_ms": dur.Milliseconds(),
	}
	if lw := orch.LastWorker(); lw != "" {
		meta["last_agent"] = lw
	}
	if tracker != nil {
		ids := tracker.AgentIDs()
		if len(ids) > 0 {
			meta["trace"] = ids
			meta["rounds_completed"] = len(ids) - 1 // exclude orchestrator itself
		}
	}
	return meta
}
