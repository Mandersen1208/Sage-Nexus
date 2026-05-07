package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
)

const skillDiscoverySourcePolicyKey = "sage:skills:discovery:source_policy"
const skillDiscoverySkillPolicyKey = "sage:skills:discovery:skill_policy"

type sourcePolicy struct {
	ID      string `json:"id"`
	Trust   string `json:"trust"`
	Enabled *bool  `json:"enabled,omitempty"`
}

type skillPolicy struct {
	ID            string   `json:"id"`
	State         string   `json:"state"`
	AllowedAgents []string `json:"allowedAgents,omitempty"`
}

type skillDiscoverySource struct {
	ID             string `json:"id"`
	DisplayName    string `json:"displayName"`
	Endpoint       string `json:"endpoint"`
	Trust          string `json:"trust"`
	Enabled        bool   `json:"enabled"`
	SourceType     string `json:"sourceType"`
	LastSyncAt     string `json:"lastSyncAt,omitempty"`
	LastSyncStatus string `json:"lastSyncStatus,omitempty"`
	LastSyncError  string `json:"lastSyncError,omitempty"`
	CreatedAt      string `json:"createdAt,omitempty"`
	UpdatedAt      string `json:"updatedAt,omitempty"`
}

type skillDiscoverySkill struct {
	ID              string   `json:"id"`
	SourceID        string   `json:"sourceId"`
	SourceName      string   `json:"sourceName"`
	OriginalName    string   `json:"originalToolName"`
	CanonicalName   string   `json:"canonicalName"`
	Description     string   `json:"description"`
	Tags            []string `json:"tags,omitempty"`
	MetadataQ       float64  `json:"metadataQuality"`
	RiskLevel       string   `json:"riskLevel"`
	ExecutionType   string   `json:"executionType"`
	RequiresSession bool     `json:"requiresSession"`
	State           string   `json:"skillState"`
	AllowedAgents   []string `json:"allowedAgents,omitempty"`
	AvgLatencyMS    float64  `json:"avgLatencyMs"`
	SuccessRate     float64  `json:"successRate"`
	DiscoveredAt    string   `json:"discoveredAt,omitempty"`
	UpdatedAt       string   `json:"updatedAt,omitempty"`
}

type skillDiscoverySearchResponse struct {
	Skills []skillDiscoverySkill `json:"skills"`
	Count  int                   `json:"count"`
	Query  string                `json:"query"`
}

type skillDiscoveryStore struct {
	rc *redis.Client
}

func newSkillDiscoveryStore(rc *redis.Client) *skillDiscoveryStore {
	if rc == nil {
		return nil
	}
	return &skillDiscoveryStore{rc: rc}
}

func (s *skillDiscoveryStore) setSourcePolicy(ctx context.Context, policy sourcePolicy) error {
	if s == nil || s.rc == nil {
		return nil
	}
	policy.ID = strings.TrimSpace(policy.ID)
	if policy.ID == "" {
		return fmt.Errorf("source policy id required")
	}
	payload, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	return s.rc.HSet(ctx, skillDiscoverySourcePolicyKey, policy.ID, payload).Err()
}

func (s *skillDiscoveryStore) setSkillPolicy(ctx context.Context, policy skillPolicy) error {
	if s == nil || s.rc == nil {
		return nil
	}
	policy.ID = strings.TrimSpace(policy.ID)
	if policy.ID == "" {
		return fmt.Errorf("skill policy id required")
	}
	policy.AllowedAgents = dedupeStringList(policy.AllowedAgents)
	payload, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	return s.rc.HSet(ctx, skillDiscoverySkillPolicyKey, policy.ID, payload).Err()
}

func (s *skillDiscoveryStore) listSourcePolicies(ctx context.Context) (map[string]sourcePolicy, error) {
	if s == nil || s.rc == nil {
		return map[string]sourcePolicy{}, nil
	}
	raw, err := s.rc.HGetAll(ctx, skillDiscoverySourcePolicyKey).Result()
	if err != nil {
		return nil, err
	}
	out := make(map[string]sourcePolicy, len(raw))
	for id, value := range raw {
		var policy sourcePolicy
		if err := json.Unmarshal([]byte(value), &policy); err != nil {
			continue
		}
		policy.ID = strings.TrimSpace(id)
		out[policy.ID] = policy
	}
	return out, nil
}

func (s *skillDiscoveryStore) listSkillPolicies(ctx context.Context) (map[string]skillPolicy, error) {
	if s == nil || s.rc == nil {
		return map[string]skillPolicy{}, nil
	}
	raw, err := s.rc.HGetAll(ctx, skillDiscoverySkillPolicyKey).Result()
	if err != nil {
		return nil, err
	}
	out := make(map[string]skillPolicy, len(raw))
	for id, value := range raw {
		var policy skillPolicy
		if err := json.Unmarshal([]byte(value), &policy); err != nil {
			continue
		}
		policy.ID = strings.TrimSpace(id)
		policy.AllowedAgents = dedupeStringList(policy.AllowedAgents)
		out[policy.ID] = policy
	}
	return out, nil
}

type skillDiscoveryRuntime struct {
	store   *skillDiscoveryStore
	baseURL string
	client  *http.Client
}

func newSkillDiscoveryRuntime(store *skillDiscoveryStore, mcpBaseURL string) *skillDiscoveryRuntime {
	baseURL := strings.TrimSpace(mcpBaseURL)
	baseURL = strings.TrimRight(baseURL, "/")
	return &skillDiscoveryRuntime{
		store:   store,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 45 * time.Second},
	}
}

func (r *skillDiscoveryRuntime) applyStoredPolicies(ctx context.Context) error {
	sourcePolicies, err := r.store.listSourcePolicies(ctx)
	if err != nil {
		return err
	}
	for sourceID, policy := range sourcePolicies {
		patch := map[string]interface{}{}
		if policy.Trust != "" {
			patch["trust"] = policy.Trust
		}
		if policy.Enabled != nil {
			patch["enabled"] = *policy.Enabled
		}
		if len(patch) == 0 {
			continue
		}
		_, _ = r.patchSource(ctx, sourceID, patch)
	}
	skillPolicies, err := r.store.listSkillPolicies(ctx)
	if err != nil {
		return err
	}
	for skillID, policy := range skillPolicies {
		patch := map[string]interface{}{}
		if policy.State != "" {
			patch["state"] = policy.State
		}
		if len(policy.AllowedAgents) > 0 {
			patch["allowedAgents"] = policy.AllowedAgents
		}
		if len(patch) == 0 {
			continue
		}
		_, _ = r.patchSkill(ctx, skillID, patch)
	}
	return nil
}

func (r *skillDiscoveryRuntime) syncDaily() error {
	_, _, err := r.postJSON("/skills/discovery/sync", map[string]interface{}{})
	return err
}

func (r *skillDiscoveryRuntime) listSources(ctx context.Context) ([]skillDiscoverySource, error) {
	status, body, err := r.getJSON("/skills/discovery/servers?includeLocal=true")
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, fmt.Errorf("source catalog failed: %s", string(body))
	}
	var payload struct {
		Sources []skillDiscoverySource `json:"sources"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	policies, err := r.store.listSourcePolicies(ctx)
	if err == nil {
		for i := range payload.Sources {
			if policy, ok := policies[payload.Sources[i].ID]; ok {
				if policy.Trust != "" {
					payload.Sources[i].Trust = policy.Trust
				}
				if policy.Enabled != nil {
					payload.Sources[i].Enabled = *policy.Enabled
				}
			}
		}
	}
	sort.Slice(payload.Sources, func(i, j int) bool {
		if payload.Sources[i].SourceType != payload.Sources[j].SourceType {
			return payload.Sources[i].SourceType < payload.Sources[j].SourceType
		}
		return strings.ToLower(payload.Sources[i].DisplayName) < strings.ToLower(payload.Sources[j].DisplayName)
	})
	return payload.Sources, nil
}

func (r *skillDiscoveryRuntime) createSource(ctx context.Context, body map[string]interface{}) (skillDiscoverySource, error) {
	status, payload, err := r.postJSON("/skills/discovery/servers", body)
	if err != nil {
		return skillDiscoverySource{}, err
	}
	if status >= 300 {
		return skillDiscoverySource{}, fmt.Errorf("source create failed: %s", string(payload))
	}
	var source skillDiscoverySource
	if err := json.Unmarshal(payload, &source); err != nil {
		return skillDiscoverySource{}, err
	}
	enabled := source.Enabled
	if err := r.store.setSourcePolicy(ctx, sourcePolicy{
		ID:      source.ID,
		Trust:   source.Trust,
		Enabled: &enabled,
	}); err != nil {
		return skillDiscoverySource{}, err
	}
	return source, nil
}

func (r *skillDiscoveryRuntime) patchSource(ctx context.Context, sourceID string, patch map[string]interface{}) (skillDiscoverySource, error) {
	status, payload, err := r.patchJSON("/skills/discovery/servers/"+url.PathEscape(sourceID), patch)
	if err != nil {
		return skillDiscoverySource{}, err
	}
	if status >= 300 {
		return skillDiscoverySource{}, fmt.Errorf("source update failed: %s", string(payload))
	}
	var source skillDiscoverySource
	if err := json.Unmarshal(payload, &source); err != nil {
		return skillDiscoverySource{}, err
	}
	enabled := source.Enabled
	if err := r.store.setSourcePolicy(ctx, sourcePolicy{
		ID:      source.ID,
		Trust:   source.Trust,
		Enabled: &enabled,
	}); err != nil {
		return skillDiscoverySource{}, err
	}
	return source, nil
}

func (r *skillDiscoveryRuntime) releaseSource(sourceID string) (int, error) {
	status, payload, err := r.postJSON("/skills/discovery/servers/"+url.PathEscape(sourceID)+"/release", map[string]interface{}{})
	if err != nil {
		return 0, err
	}
	if status >= 300 {
		return 0, fmt.Errorf("release source failed: %s", string(payload))
	}
	var out struct {
		Released int `json:"released"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		return 0, err
	}
	return out.Released, nil
}

func (r *skillDiscoveryRuntime) syncSource(sourceID string) (map[string]interface{}, error) {
	status, payload, err := r.postJSON("/skills/discovery/servers/"+url.PathEscape(sourceID)+"/sync", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, fmt.Errorf("sync source failed: %s", string(payload))
	}
	var out map[string]interface{}
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *skillDiscoveryRuntime) syncAll() (map[string]interface{}, error) {
	status, payload, err := r.postJSON("/skills/discovery/sync", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, fmt.Errorf("sync all failed: %s", string(payload))
	}
	var out map[string]interface{}
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *skillDiscoveryRuntime) listSkills(filters map[string]string) ([]skillDiscoverySkill, error) {
	query := url.Values{}
	query.Set("includeLocal", "true")
	for k, v := range filters {
		v = strings.TrimSpace(v)
		if v != "" {
			query.Set(k, v)
		}
	}
	path := "/skills/discovery/skills"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	status, body, err := r.getJSON(path)
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, fmt.Errorf("skill list failed: %s", string(body))
	}
	var payload struct {
		Skills []skillDiscoverySkill `json:"skills"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload.Skills, nil
}

func (r *skillDiscoveryRuntime) patchSkill(ctx context.Context, skillID string, patch map[string]interface{}) (skillDiscoverySkill, error) {
	status, payload, err := r.patchJSON("/skills/discovery/skills/"+url.PathEscape(skillID), patch)
	if err != nil {
		return skillDiscoverySkill{}, err
	}
	if status >= 300 {
		return skillDiscoverySkill{}, fmt.Errorf("skill update failed: %s", string(payload))
	}
	var skill skillDiscoverySkill
	if err := json.Unmarshal(payload, &skill); err != nil {
		return skillDiscoverySkill{}, err
	}
	if err := r.store.setSkillPolicy(ctx, skillPolicy{
		ID:            skill.ID,
		State:         skill.State,
		AllowedAgents: dedupeStringList(skill.AllowedAgents),
	}); err != nil {
		return skillDiscoverySkill{}, err
	}
	return skill, nil
}

func (r *skillDiscoveryRuntime) searchSkills(query, agentID string, limit int) ([]skillDiscoverySkill, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	params := url.Values{}
	params.Set("query", query)
	if strings.TrimSpace(agentID) != "" {
		params.Set("agentId", strings.TrimSpace(agentID))
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	status, body, err := r.getJSON("/skills/discovery/search?" + params.Encode())
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, fmt.Errorf("skill search failed: %s", string(body))
	}
	var payload skillDiscoverySearchResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload.Skills, nil
}

func (r *skillDiscoveryRuntime) getJSON(path string) (int, []byte, error) {
	endpoint := r.baseURL + path
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

func (r *skillDiscoveryRuntime) postJSON(path string, payload interface{}) (int, []byte, error) {
	return r.sendJSON(http.MethodPost, path, payload)
}

func (r *skillDiscoveryRuntime) patchJSON(path string, payload interface{}) (int, []byte, error) {
	return r.sendJSON(http.MethodPatch, path, payload)
}

func (r *skillDiscoveryRuntime) sendJSON(method, path string, payload interface{}) (int, []byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	endpoint := r.baseURL + path
	req, err := http.NewRequest(method, endpoint, strings.NewReader(string(data)))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

func dedupeStringList(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func registerSkillDiscoveryRoutes(mux *http.ServeMux, runtime *skillDiscoveryRuntime) {
	mux.HandleFunc("/skills/discovered/servers", func(w http.ResponseWriter, r *http.Request) {
		if runtime == nil {
			http.Error(w, "discovery runtime unavailable", http.StatusServiceUnavailable)
			return
		}
		if !isAllowedChatRequest(r) {
			http.Error(w, "chat requests must originate from this host", http.StatusForbidden)
			return
		}
		switch r.Method {
		case http.MethodGet:
			sources, err := runtime.listSources(r.Context())
			if err != nil {
				http.Error(w, "source list failed: "+err.Error(), http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"sources": sources,
				"count":   len(sources),
			})
		case http.MethodPost:
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			source, err := runtime.createSource(r.Context(), body)
			if err != nil {
				http.Error(w, "source create failed: "+err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(source)
		default:
			http.Error(w, "GET or POST only", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/skills/discovered/servers/", func(w http.ResponseWriter, r *http.Request) {
		if runtime == nil {
			http.Error(w, "discovery runtime unavailable", http.StatusServiceUnavailable)
			return
		}
		if !isAllowedChatRequest(r) {
			http.Error(w, "chat requests must originate from this host", http.StatusForbidden)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/skills/discovered/servers/")
		path = strings.TrimSpace(path)
		if path == "" {
			http.Error(w, "source id required", http.StatusBadRequest)
			return
		}
		if strings.HasSuffix(path, "/release") {
			sourceID := strings.TrimSuffix(path, "/release")
			sourceID = strings.Trim(sourceID, "/ ")
			if r.Method != http.MethodPost {
				http.Error(w, "POST only", http.StatusMethodNotAllowed)
				return
			}
			released, err := runtime.releaseSource(sourceID)
			if err != nil {
				http.Error(w, "release failed: "+err.Error(), http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"sourceId": sourceID,
				"released": released,
			})
			return
		}
		if strings.HasSuffix(path, "/sync") {
			sourceID := strings.TrimSuffix(path, "/sync")
			sourceID = strings.Trim(sourceID, "/ ")
			if r.Method != http.MethodPost {
				http.Error(w, "POST only", http.StatusMethodNotAllowed)
				return
			}
			result, err := runtime.syncSource(sourceID)
			if err != nil {
				http.Error(w, "sync failed: "+err.Error(), http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(result)
			return
		}
		sourceID := strings.Trim(path, "/ ")
		if sourceID == "" {
			http.Error(w, "source id required", http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPatch {
			http.Error(w, "PATCH only", http.StatusMethodNotAllowed)
			return
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		source, err := runtime.patchSource(r.Context(), sourceID, body)
		if err != nil {
			http.Error(w, "source update failed: "+err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(source)
	})

	mux.HandleFunc("/skills/discovered/sync", func(w http.ResponseWriter, r *http.Request) {
		if runtime == nil {
			http.Error(w, "discovery runtime unavailable", http.StatusServiceUnavailable)
			return
		}
		if !isAllowedChatRequest(r) {
			http.Error(w, "chat requests must originate from this host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		result, err := runtime.syncAll()
		if err != nil {
			http.Error(w, "sync failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})

	mux.HandleFunc("/skills/discovered/skills", func(w http.ResponseWriter, r *http.Request) {
		if runtime == nil {
			http.Error(w, "discovery runtime unavailable", http.StatusServiceUnavailable)
			return
		}
		if !isAllowedChatRequest(r) {
			http.Error(w, "chat requests must originate from this host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}
		filters := map[string]string{
			"sourceId": r.URL.Query().Get("sourceId"),
			"state":    r.URL.Query().Get("state"),
		}
		skills, err := runtime.listSkills(filters)
		if err != nil {
			http.Error(w, "skill list failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"skills": skills,
			"count":  len(skills),
		})
	})

	mux.HandleFunc("/skills/discovered/skills/", func(w http.ResponseWriter, r *http.Request) {
		if runtime == nil {
			http.Error(w, "discovery runtime unavailable", http.StatusServiceUnavailable)
			return
		}
		if !isAllowedChatRequest(r) {
			http.Error(w, "chat requests must originate from this host", http.StatusForbidden)
			return
		}
		skillID := strings.TrimPrefix(r.URL.Path, "/skills/discovered/skills/")
		skillID = strings.TrimSpace(strings.Trim(skillID, "/"))
		if skillID == "" {
			http.Error(w, "skill id required", http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPatch {
			http.Error(w, "PATCH only", http.StatusMethodNotAllowed)
			return
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		skill, err := runtime.patchSkill(r.Context(), skillID, body)
		if err != nil {
			http.Error(w, "skill update failed: "+err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(skill)
	})
}
