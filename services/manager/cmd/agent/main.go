package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-redis/redis/v8"
	sageagents "github.com/matta/sage-nexus/services/manager"
)

func main() {
	listenAddr := sageagents.GetEnvOr("AGENT_LISTEN_ADDR", ":8081")
	managerURL := sageagents.GetEnvOr("MANAGER_URL", "http://manager:8090")
	acpEndpoint := sageagents.GetEnvOr("ACP_ENDPOINT", "http://acp-server:8080")
	mcpEndpoint := sageagents.GetEnvOr("MCP_ENDPOINT", "http://sage-mcp:3030")
	stateDir := sageagents.GetEnvOr("SAGE_STATE_DIR", "/sage-state")
	configPath := sageagents.GetEnvOr("AGENTS_CONFIG", "/app/config/agents.json")

	// ── Load agent config ────────────────────────────────────────────────────
	cfg, err := sageagents.LoadAgentsConfig(configPath)
	if err != nil {
		log.Printf("Warning: could not load agents config (%v) — using defaults", err)
		cfg = &sageagents.AgentsConfig{}
	}

	const agentID = "AGT-research-agent"
	agentCfg := cfg.Get(agentID)

	systemPrompt, err := cfg.SystemPrompt(agentID)
	if err != nil {
		log.Printf("Warning: system prompt load failed: %v", err)
	} else if systemPrompt != "" {
		log.Printf("Loaded system prompt for %s (%d chars)", agentID, len(systemPrompt))
	}

	// ── Build agent ──────────────────────────────────────────────────────────
	agent := &sageagents.CopilotAgent{
		BaseAgent: sageagents.BaseAgent{
			AgentID:     agentID,
			Endpoint:    fmt.Sprintf("http://localhost%s/invoke-tool", listenAddr),
			ACPEndpoint: acpEndpoint,
			MCPEndpoint: mcpEndpoint,
		},
		Model:        agentCfg.Model,
		SystemPrompt: systemPrompt,
	}

	agent.SetStateDir(stateDir)
	for _, v := range []string{"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if t := os.Getenv(v); t != "" {
			agent.OAuthToken = t
			log.Printf("Copilot OAuth fallback loaded from %s", v)
			break
		}
	}

	if err := agent.GenerateIdentity(); err != nil {
		log.Fatalf("Failed to generate agent identity: %v", err)
	}
	if err := agent.RegisterWithACP("L2"); err != nil {
		log.Printf("ACP registration failed (non-fatal): %v", err)
	} else {
		log.Printf("Registered with ACP as %s  model=%s", agentID, agent.ActiveModel())
	}

	// ── Redis pub/sub task listener ──────────────────────────────────────────
	redisAddr := sageagents.GetEnvOr("REDIS_ADDR", "redis:6379")
	ctx := context.Background()
	rc := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: os.Getenv("REDIS_PASSWORD"),
	})

	taskCh := fmt.Sprintf("agent:%s:tasks", agentID)
	respPrefix := fmt.Sprintf("agent:%s:responses:", agentID)

	go func() {
		ps := rc.Subscribe(ctx, taskCh)
		defer ps.Close()
		log.Printf("Subscribed to Redis channel: %s", taskCh)
		for {
			msg, err := ps.ReceiveMessage(ctx)
			if err != nil {
				log.Printf("Redis pubsub error: %v", err)
				continue
			}
			var req sageagents.ToolInvokeRequest
			if err := json.Unmarshal([]byte(msg.Payload), &req); err != nil {
				log.Printf("Invalid task payload: %v", err)
				continue
			}
			resp := dispatchTool(agent, req)
			b, _ := json.Marshal(resp)
			rc.Publish(ctx, respPrefix+req.Tool, b)
		}
	}()

	// ── Heartbeat ────────────────────────────────────────────────────────────
	go func() {
		for {
			if err := agent.SendHeartbeat(managerURL); err != nil {
				log.Printf("Heartbeat error: %v", err)
			}
			time.Sleep(30 * time.Minute)
		}
	}()

	// ── HTTP ─────────────────────────────────────────────────────────────────
	http.HandleFunc("/invoke-tool", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req sageagents.ToolInvokeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(sageagents.ToolInvokeResponse{Error: "invalid body"})
			return
		}
		json.NewEncoder(w).Encode(dispatchTool(agent, req))
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	log.Printf("Agent server listening on %s", listenAddr)
	if err := http.ListenAndServe(listenAddr, nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func dispatchTool(agent *sageagents.CopilotAgent, req sageagents.ToolInvokeRequest) sageagents.ToolInvokeResponse {
	switch req.Tool {
	case "copilot":
		result, err := agent.UseCopilotModel(req.Prompt)
		if err != nil {
			return sageagents.ToolInvokeResponse{Error: err.Error()}
		}
		return sageagents.ToolInvokeResponse{Result: result}
	default:
		return sageagents.ToolInvokeResponse{Error: "unknown tool: " + req.Tool}
	}
}
