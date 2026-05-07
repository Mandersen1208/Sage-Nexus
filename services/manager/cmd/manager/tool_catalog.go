package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	sageagents "github.com/matta/sage-nexus/services/manager"
)

const toolCatalogOverridesKey = "sage:tool:catalog:overrides"
const toolCatalogCustomKey = "sage:tool:catalog:custom"

type toolCatalogEntry struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Source           string   `json:"source"`
	Enabled          bool     `json:"enabled"`
	AssignedAgentIDs []string `json:"assignedAgentIds,omitempty"`
	Area             string   `json:"area,omitempty"`
	Command          string   `json:"command,omitempty"`
	Args             string   `json:"args,omitempty"`
	CreatedAt        int64    `json:"createdAt,omitempty"`
	UpdatedAt        int64    `json:"updatedAt,omitempty"`
}

type toolCatalog struct {
	Tools    []toolCatalogEntry `json:"tools"`
	SyncedAt int64              `json:"syncedAt"`
}

type toolCatalogStore struct {
	rc *redis.Client
}

func newToolCatalogStore(rc *redis.Client) *toolCatalogStore {
	if rc == nil {
		return nil
	}
	return &toolCatalogStore{rc: rc}
}

func (s *toolCatalogStore) list(ctx context.Context, key string) (map[string]toolCatalogEntry, error) {
	if s == nil || s.rc == nil {
		return map[string]toolCatalogEntry{}, nil
	}
	raw, err := s.rc.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	out := make(map[string]toolCatalogEntry, len(raw))
	for id, payload := range raw {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		var item toolCatalogEntry
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			continue
		}
		item.ID = id
		out[id] = normalizeToolCatalogEntry(item)
	}
	return out, nil
}

func (s *toolCatalogStore) set(ctx context.Context, key string, item toolCatalogEntry) error {
	if s == nil || s.rc == nil {
		return nil
	}
	item = normalizeToolCatalogEntry(item)
	if item.ID == "" {
		return fmt.Errorf("tool id required")
	}
	payload, err := json.Marshal(item)
	if err != nil {
		return err
	}
	return s.rc.HSet(ctx, key, item.ID, payload).Err()
}

func (s *toolCatalogStore) del(ctx context.Context, key, id string) error {
	if s == nil || s.rc == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	return s.rc.HDel(ctx, key, id).Err()
}

type toolCatalogRuntime struct {
	mu sync.RWMutex

	store     *toolCatalogStore
	listTools func() ([]sageagents.ToolDefinition, error)
}

func newToolCatalogRuntime(store *toolCatalogStore, mcp *sageagents.MCPClient) *toolCatalogRuntime {
	runtime := &toolCatalogRuntime{store: store}
	if mcp != nil {
		runtime.listTools = mcp.ListTools
	}
	return runtime
}

func (r *toolCatalogRuntime) Catalog(ctx context.Context) (toolCatalog, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	overrides, err := r.store.list(ctx, toolCatalogOverridesKey)
	if err != nil {
		return toolCatalog{}, err
	}
	custom, err := r.store.list(ctx, toolCatalogCustomKey)
	if err != nil {
		return toolCatalog{}, err
	}

	items := make(map[string]toolCatalogEntry)
	if r.listTools != nil {
		tools, err := r.listTools()
		if err != nil {
			return toolCatalog{}, err
		}
		for _, tool := range tools {
			id := strings.TrimSpace(tool.Function.Name)
			if id == "" {
				continue
			}
			items[id] = toolCatalogEntry{
				ID:          id,
				Name:        id,
				Description: strings.TrimSpace(tool.Function.Description),
				Source:      "mcp",
				Enabled:     true,
			}
		}
	}

	for id, override := range overrides {
		base, ok := items[id]
		if !ok {
			continue
		}
		if override.Name != "" {
			base.Name = override.Name
		}
		if override.Description != "" {
			base.Description = override.Description
		}
		base.Enabled = override.Enabled
		base.Area = override.Area
		base.AssignedAgentIDs = append([]string{}, override.AssignedAgentIDs...)
		base.UpdatedAt = override.UpdatedAt
		items[id] = normalizeToolCatalogEntry(base)
	}

	for id, item := range custom {
		item.ID = id
		item.Source = "custom"
		items[id] = normalizeToolCatalogEntry(item)
	}

	list := make([]toolCatalogEntry, 0, len(items))
	for _, item := range items {
		list = append(list, item)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Source != list[j].Source {
			return list[i].Source < list[j].Source
		}
		return strings.ToLower(list[i].ID) < strings.ToLower(list[j].ID)
	})
	return toolCatalog{Tools: list, SyncedAt: time.Now().UnixMilli()}, nil
}

type createToolCatalogRequest struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Enabled          *bool    `json:"enabled"`
	AssignedAgentIDs []string `json:"assignedAgentIds"`
	Area             string   `json:"area"`
	Command          string   `json:"command"`
	Args             string   `json:"args"`
}

type patchToolCatalogRequest struct {
	Name             *string   `json:"name"`
	Description      *string   `json:"description"`
	Enabled          *bool     `json:"enabled"`
	AssignedAgentIDs *[]string `json:"assignedAgentIds"`
	Area             *string   `json:"area"`
	Command          *string   `json:"command"`
	Args             *string   `json:"args"`
}

func (r *toolCatalogRuntime) CreateCustom(ctx context.Context, req createToolCatalogRequest) (toolCatalogEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	id := strings.TrimSpace(req.ID)
	if id == "" {
		return toolCatalogEntry{}, fmt.Errorf("id required")
	}
	if !validToolID(id) {
		return toolCatalogEntry{}, fmt.Errorf("invalid id")
	}
	if req.Command == "" {
		return toolCatalogEntry{}, fmt.Errorf("command required")
	}
	if existing, _ := r.store.list(ctx, toolCatalogCustomKey); existing[id].ID != "" {
		return toolCatalogEntry{}, fmt.Errorf("tool already exists")
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	now := time.Now().UnixMilli()
	item := normalizeToolCatalogEntry(toolCatalogEntry{
		ID:               id,
		Name:             strings.TrimSpace(req.Name),
		Description:      strings.TrimSpace(req.Description),
		Source:           "custom",
		Enabled:          enabled,
		AssignedAgentIDs: req.AssignedAgentIDs,
		Area:             strings.TrimSpace(req.Area),
		Command:          strings.TrimSpace(req.Command),
		Args:             strings.TrimSpace(req.Args),
		CreatedAt:        now,
		UpdatedAt:        now,
	})
	if item.Name == "" {
		item.Name = item.ID
	}
	if err := r.store.set(ctx, toolCatalogCustomKey, item); err != nil {
		return toolCatalogEntry{}, err
	}
	return item, nil
}

func (r *toolCatalogRuntime) Update(ctx context.Context, id string, req patchToolCatalogRequest) (toolCatalogEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	id = strings.TrimSpace(id)
	if id == "" {
		return toolCatalogEntry{}, fmt.Errorf("id required")
	}

	custom, err := r.store.list(ctx, toolCatalogCustomKey)
	if err != nil {
		return toolCatalogEntry{}, err
	}
	if item, ok := custom[id]; ok {
		item = applyToolPatch(item, req)
		item.Source = "custom"
		item.UpdatedAt = time.Now().UnixMilli()
		item = normalizeToolCatalogEntry(item)
		if err := r.store.set(ctx, toolCatalogCustomKey, item); err != nil {
			return toolCatalogEntry{}, err
		}
		return item, nil
	}

	// For MCP tools, persist only override metadata.
	item := toolCatalogEntry{ID: id, Source: "mcp", Enabled: true}
	overrides, err := r.store.list(ctx, toolCatalogOverridesKey)
	if err != nil {
		return toolCatalogEntry{}, err
	}
	if existing, ok := overrides[id]; ok {
		item = existing
	}
	item = applyToolPatch(item, req)
	item.Source = "mcp"
	item.UpdatedAt = time.Now().UnixMilli()
	item = normalizeToolCatalogEntry(item)
	if err := r.store.set(ctx, toolCatalogOverridesKey, item); err != nil {
		return toolCatalogEntry{}, err
	}
	return item, nil
}

func (r *toolCatalogRuntime) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	custom, err := r.store.list(ctx, toolCatalogCustomKey)
	if err != nil {
		return err
	}
	if _, ok := custom[id]; ok {
		return r.store.del(ctx, toolCatalogCustomKey, id)
	}
	return r.store.del(ctx, toolCatalogOverridesKey, id)
}

func normalizeToolCatalogEntry(item toolCatalogEntry) toolCatalogEntry {
	item.ID = strings.TrimSpace(item.ID)
	item.Name = strings.TrimSpace(item.Name)
	item.Description = strings.TrimSpace(item.Description)
	item.Area = strings.TrimSpace(item.Area)
	item.Command = strings.TrimSpace(item.Command)
	item.Args = strings.TrimSpace(item.Args)
	if item.Name == "" {
		item.Name = item.ID
	}
	seen := make(map[string]struct{}, len(item.AssignedAgentIDs))
	cleaned := make([]string, 0, len(item.AssignedAgentIDs))
	for _, agentID := range item.AssignedAgentIDs {
		agentID = strings.TrimSpace(agentID)
		if agentID == "" {
			continue
		}
		if _, ok := seen[agentID]; ok {
			continue
		}
		seen[agentID] = struct{}{}
		cleaned = append(cleaned, agentID)
	}
	sort.Strings(cleaned)
	item.AssignedAgentIDs = cleaned
	return item
}

func applyToolPatch(item toolCatalogEntry, req patchToolCatalogRequest) toolCatalogEntry {
	if req.Name != nil {
		item.Name = strings.TrimSpace(*req.Name)
	}
	if req.Description != nil {
		item.Description = strings.TrimSpace(*req.Description)
	}
	if req.Enabled != nil {
		item.Enabled = *req.Enabled
	}
	if req.AssignedAgentIDs != nil {
		item.AssignedAgentIDs = append([]string{}, *req.AssignedAgentIDs...)
	}
	if req.Area != nil {
		item.Area = strings.TrimSpace(*req.Area)
	}
	if req.Command != nil {
		item.Command = strings.TrimSpace(*req.Command)
	}
	if req.Args != nil {
		item.Args = strings.TrimSpace(*req.Args)
	}
	return item
}

func validToolID(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		switch r {
		case '-', '_', ':', '.':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func registerToolCatalogRoutes(mux *http.ServeMux, runtime *toolCatalogRuntime) {
	mux.HandleFunc("/tools/catalog", func(w http.ResponseWriter, r *http.Request) {
		if runtime == nil {
			http.Error(w, "tool runtime unavailable", http.StatusServiceUnavailable)
			return
		}
		if !isAllowedChatRequest(r) {
			http.Error(w, "chat requests must originate from this host", http.StatusForbidden)
			return
		}
		switch r.Method {
		case http.MethodGet:
			catalog, err := runtime.Catalog(r.Context())
			if err != nil {
				http.Error(w, "tool catalog unavailable: "+err.Error(), http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(catalog)
		case http.MethodPost:
			var body createToolCatalogRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			created, err := runtime.CreateCustom(r.Context(), body)
			if err != nil {
				http.Error(w, "tool create failed: "+err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(created)
		default:
			http.Error(w, "GET or POST only", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/tools/catalog/", func(w http.ResponseWriter, r *http.Request) {
		if runtime == nil {
			http.Error(w, "tool runtime unavailable", http.StatusServiceUnavailable)
			return
		}
		if !isAllowedChatRequest(r) {
			http.Error(w, "chat requests must originate from this host", http.StatusForbidden)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/tools/catalog/")
		id = strings.TrimSpace(id)
		if id == "" {
			http.Error(w, "tool id required", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodPatch:
			var body patchToolCatalogRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			updated, err := runtime.Update(r.Context(), id, body)
			if err != nil {
				http.Error(w, "tool update failed: "+err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(updated)
		case http.MethodDelete:
			if err := runtime.Delete(r.Context(), id); err != nil {
				http.Error(w, "tool delete failed: "+err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "PATCH or DELETE only", http.StatusMethodNotAllowed)
		}
	})
}
