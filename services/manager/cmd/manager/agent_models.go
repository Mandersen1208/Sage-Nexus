package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/go-redis/redis/v8"
	sageagents "github.com/matta/sage-nexus/services/manager"
)

const agentModelOverridesKey = "sage:agent:model:overrides"

type agentModelOverrideStore struct {
	rc  *redis.Client
	key string
}

func newAgentModelOverrideStore(rc *redis.Client) *agentModelOverrideStore {
	if rc == nil {
		return nil
	}
	return &agentModelOverrideStore{rc: rc, key: agentModelOverridesKey}
}

func (s *agentModelOverrideStore) List(ctx context.Context) (map[string]string, error) {
	if s == nil || s.rc == nil {
		return map[string]string{}, nil
	}
	values, err := s.rc.HGetAll(ctx, s.key).Result()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(values))
	for id, model := range values {
		id = strings.TrimSpace(id)
		model = strings.TrimSpace(model)
		if id == "" || model == "" {
			continue
		}
		out[id] = model
	}
	return out, nil
}

func (s *agentModelOverrideStore) Set(ctx context.Context, agentID, model string) error {
	if s == nil || s.rc == nil {
		return nil
	}
	agentID = strings.TrimSpace(agentID)
	model = strings.TrimSpace(model)
	if agentID == "" {
		return fmt.Errorf("agent id required")
	}
	if model == "" {
		return s.rc.HDel(ctx, s.key, agentID).Err()
	}
	return s.rc.HSet(ctx, s.key, agentID, model).Err()
}

type agentModelItem struct {
	AgentID         string `json:"agentId"`
	DisplayName     string `json:"displayName"`
	CurrentModel    string `json:"currentModel"`
	ConfiguredModel string `json:"configuredModel"`
	Source          string `json:"source"`
}

type agentModelCatalog struct {
	Agents       []agentModelItem `json:"agents"`
	ModelOptions []string         `json:"modelOptions"`
}

type agentModelRuntime struct {
	mu sync.RWMutex

	cfg          *sageagents.AgentsConfig
	orchestrator *sageagents.SageOrchestratorAgent
	workers      map[string]*sageagents.CopilotAgent
	sageRunner   *sageagents.SageRunner

	overrides      *agentModelOverrideStore
	baseConfigured map[string]string
	activeOverride map[string]string
	stateDir       string
	listModels     func(stateDir string) ([]string, error)
}

func newAgentModelRuntime(
	cfg *sageagents.AgentsConfig,
	orchestrator *sageagents.SageOrchestratorAgent,
	workers map[string]*sageagents.CopilotAgent,
	sageRunner *sageagents.SageRunner,
	overrides *agentModelOverrideStore,
	stateDir string,
) *agentModelRuntime {
	baseConfigured := map[string]string{}
	if cfg != nil {
		for id, agent := range cfg.Agents {
			baseConfigured[id] = strings.TrimSpace(agent.Model)
		}
	}
	return &agentModelRuntime{
		cfg:            cfg,
		orchestrator:   orchestrator,
		workers:        workers,
		sageRunner:     sageRunner,
		overrides:      overrides,
		baseConfigured: baseConfigured,
		activeOverride: map[string]string{},
		stateDir:       strings.TrimSpace(stateDir),
		listModels:     sageagents.ListCopilotModels,
	}
}

func (r *agentModelRuntime) LoadOverrides(ctx context.Context) error {
	if r == nil || r.overrides == nil {
		return nil
	}
	overrides, err := r.overrides.List(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, model := range overrides {
		if !r.isKnownAgentLocked(id) {
			continue
		}
		r.activeOverride[id] = model
		r.applyModelLocked(id, model)
	}
	return nil
}

func (r *agentModelRuntime) Catalog() agentModelCatalog {
	r.mu.RLock()
	defer r.mu.RUnlock()

	items := make([]agentModelItem, 0)
	modelOptions := map[string]struct{}{}
	for _, id := range r.catalogAgentIDsLocked() {
		agent := r.cfg.Get(id)
		configured := strings.TrimSpace(r.baseConfigured[id])
		current := strings.TrimSpace(r.currentModelLocked(id))
		source := "default"
		if _, ok := r.activeOverride[id]; ok {
			source = "override"
		} else if configured != "" {
			source = "registry"
		}
		items = append(items, agentModelItem{
			AgentID:         id,
			DisplayName:     agent.DisplayNameValue(),
			CurrentModel:    current,
			ConfiguredModel: configured,
			Source:          source,
		})
	}
	modelOptions[sageagents.DefaultCodexModelRef] = struct{}{}

	if r.stateDir != "" && r.listModels != nil {
		if discovered, err := r.listModels(r.stateDir); err == nil {
			for _, model := range discovered {
				model = strings.TrimSpace(model)
				if model == "" {
					continue
				}
				modelOptions[model] = struct{}{}
			}
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].AgentID == sageagents.SageAgentID {
			return true
		}
		if items[j].AgentID == sageagents.SageAgentID {
			return false
		}
		return strings.ToLower(items[i].DisplayName) < strings.ToLower(items[j].DisplayName)
	})

	options := make([]string, 0, len(modelOptions))
	for model := range modelOptions {
		options = append(options, model)
	}
	sort.Strings(options)
	return agentModelCatalog{Agents: items, ModelOptions: options}
}

func (r *agentModelRuntime) Update(ctx context.Context, agentID, model string) (agentModelItem, error) {
	agentID = strings.TrimSpace(agentID)
	model = strings.TrimSpace(model)
	if agentID == "" {
		return agentModelItem{}, fmt.Errorf("agentId required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.isKnownAgentLocked(agentID) {
		return agentModelItem{}, fmt.Errorf("unknown agent %s", agentID)
	}
	if err := sageagents.ValidateAgentProviderModel(agentID, model); err != nil {
		return agentModelItem{}, err
	}

	target := model
	if target == "" {
		target = strings.TrimSpace(r.baseConfigured[agentID])
	}

	// Log what we're about to do
	current := strings.TrimSpace(r.currentModelLocked(agentID))
	log.Printf("[agent-model] Updating %s: %s → %s", agentID, current, target)

	// Check which runtime components are available
	hasOrchestrator := agentID == sageagents.OrchestratorAgentID && r.orchestrator != nil
	hasSageRunner := agentID == sageagents.SageAgentID && r.sageRunner != nil && r.sageRunner.Sage != nil
	hasWorker := agentID != sageagents.OrchestratorAgentID && agentID != sageagents.SageAgentID && r.workers[agentID] != nil

	log.Printf("[agent-model] %s runtime: orchestrator=%v sageRunner=%v worker=%v", agentID, hasOrchestrator, hasSageRunner, hasWorker)

	r.applyModelLocked(agentID, target)

	if model == "" {
		delete(r.activeOverride, agentID)
	} else {
		r.activeOverride[agentID] = model
	}

	if r.overrides != nil {
		if err := r.overrides.Set(ctx, agentID, model); err != nil {
			return agentModelItem{}, err
		}
	}

	agent := r.cfg.Get(agentID)
	source := "default"
	if _, ok := r.activeOverride[agentID]; ok {
		source = "override"
	} else if strings.TrimSpace(r.baseConfigured[agentID]) != "" {
		source = "registry"
	}

	result := agentModelItem{
		AgentID:         agentID,
		DisplayName:     agent.DisplayNameValue(),
		CurrentModel:    strings.TrimSpace(r.currentModelLocked(agentID)),
		ConfiguredModel: strings.TrimSpace(r.baseConfigured[agentID]),
		Source:          source,
	}

	log.Printf("[agent-model] Update complete: %s now using %s (source=%s)", agentID, result.CurrentModel, result.Source)

	return result, nil
}

func (r *agentModelRuntime) catalogAgentIDsLocked() []string {
	if r.cfg == nil {
		return nil
	}
	ids := make([]string, 0, len(r.cfg.Agents))
	for id := range r.cfg.Agents {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (r *agentModelRuntime) isKnownAgentLocked(agentID string) bool {
	if r.cfg == nil {
		return false
	}
	agent := r.cfg.Get(agentID)
	return strings.TrimSpace(agent.ID) != ""
}

func (r *agentModelRuntime) currentModelLocked(agentID string) string {
	switch agentID {
	case sageagents.OrchestratorAgentID:
		if r.orchestrator != nil {
			return r.orchestrator.ActiveModel()
		}
	case sageagents.SageAgentID:
		if r.sageRunner != nil && r.sageRunner.Sage != nil {
			return r.sageRunner.Sage.ActiveModel()
		}
	default:
		if worker := r.workers[agentID]; worker != nil {
			return worker.ActiveModel()
		}
	}
	agent := r.cfg.Get(agentID)
	if strings.TrimSpace(agent.Model) != "" {
		return strings.TrimSpace(agent.Model)
	}
	return ""
}

func (r *agentModelRuntime) applyModelLocked(agentID, model string) {
	if r.cfg != nil {
		agent := r.cfg.Get(agentID)
		if strings.TrimSpace(agent.ID) != "" {
			agent.Model = model
			r.cfg.Agents[agentID] = agent
		}
	}

	switch agentID {
	case sageagents.OrchestratorAgentID:
		if r.orchestrator != nil {
			r.orchestrator.Model = model
			// Also update the inner LLM agent that ActiveModel() reads from,
			// if it has already been lazily initialized.
			if r.orchestrator.LLM() != nil {
				r.orchestrator.LLM().Model = model
			}
		}
	case sageagents.SageAgentID:
		if r.sageRunner != nil && r.sageRunner.Sage != nil {
			r.sageRunner.Sage.Model = model
		}
	default:
		if worker := r.workers[agentID]; worker != nil {
			worker.Model = model
		}
	}
}

func registerAgentModelRoutes(mux *http.ServeMux, runtime *agentModelRuntime) {
	mux.HandleFunc("/agents/models", func(w http.ResponseWriter, r *http.Request) {
		if runtime == nil {
			http.Error(w, "model runtime unavailable", http.StatusServiceUnavailable)
			return
		}
		if !isAllowedChatRequest(r) {
			http.Error(w, "chat requests must originate from this host", http.StatusForbidden)
			return
		}
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(runtime.Catalog())
		case http.MethodPatch:
			var body struct {
				AgentID string `json:"agentId"`
				Model   string `json:"model"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			item, err := runtime.Update(r.Context(), body.AgentID, body.Model)
			if err != nil {
				http.Error(w, "model update failed: "+err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(item)
		default:
			http.Error(w, "GET or PATCH only", http.StatusMethodNotAllowed)
		}
	})
}
