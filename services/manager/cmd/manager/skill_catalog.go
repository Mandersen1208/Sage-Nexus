package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultEditableSkillsRelPath = "workspace/skills"

type skillCatalogEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Enabled     bool     `json:"enabled"`
	Source      string   `json:"source"`
	UpdatedAt   int64    `json:"updatedAt"`
}

type skillCatalog struct {
	Skills   []skillCatalogEntry `json:"skills"`
	SyncedAt int64               `json:"syncedAt"`
}

type skillContentEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Enabled     bool     `json:"enabled"`
	Content     string   `json:"content"`
	UpdatedAt   int64    `json:"updatedAt"`
}

type skillUpsertRequest struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Enabled     *bool    `json:"enabled"`
	Content     string   `json:"content"`
}

type skillPatchRequest struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Tags        *[]string `json:"tags"`
	Enabled     *bool     `json:"enabled"`
	Content     *string   `json:"content"`
}

type skillComposeRequest struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Tags             []string `json:"tags"`
	Enabled          *bool    `json:"enabled"`
	Trigger          string   `json:"trigger"`
	AssignedAgentIDs []string `json:"assignedAgentIds"`
	Inputs           string   `json:"inputs"`
	Outputs          string   `json:"outputs"`
	Notes            string   `json:"notes"`
}

type skillMetadata struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Disabled    bool     `json:"disabled,omitempty"`
}

type skillCatalogRuntime struct {
	mu   sync.RWMutex
	root string
}

func newSkillCatalogRuntime(stateDir string) *skillCatalogRuntime {
	base := strings.TrimSpace(stateDir)
	if base == "" {
		base = "/sage-state"
	}
	root := filepath.Join(base, defaultEditableSkillsRelPath)
	return &skillCatalogRuntime{root: root}
}

func (r *skillCatalogRuntime) Catalog() (skillCatalog, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries, err := r.listEditableSkillsLocked()
	if err != nil {
		return skillCatalog{}, err
	}
	return skillCatalog{Skills: entries, SyncedAt: time.Now().UnixMilli()}, nil
}

func (r *skillCatalogRuntime) Get(id string) (skillContentEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.readSkillLocked(id)
}

func (r *skillCatalogRuntime) Create(req skillUpsertRequest) (skillContentEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	id := normalizeSkillID(req.ID)
	if id == "" {
		return skillContentEntry{}, fmt.Errorf("id required")
	}
	if !validSkillID(id) {
		return skillContentEntry{}, fmt.Errorf("invalid id")
	}
	if _, err := os.Stat(r.skillDir(id)); err == nil {
		return skillContentEntry{}, fmt.Errorf("skill already exists")
	} else if !os.IsNotExist(err) {
		return skillContentEntry{}, err
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if err := r.writeSkillLocked(id, req.Name, req.Description, req.Tags, enabled, req.Content); err != nil {
		return skillContentEntry{}, err
	}
	return r.readSkillLocked(id)
}

func (r *skillCatalogRuntime) Compose(req skillComposeRequest) (skillContentEntry, error) {
	content := composeSkillMarkdown(req)
	return r.Create(skillUpsertRequest{
		ID:          req.ID,
		Name:        req.Name,
		Description: req.Description,
		Tags:        req.Tags,
		Enabled:     req.Enabled,
		Content:     content,
	})
}

func (r *skillCatalogRuntime) Update(id string, req skillPatchRequest) (skillContentEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	id = normalizeSkillID(id)
	if id == "" {
		return skillContentEntry{}, fmt.Errorf("id required")
	}
	current, err := r.readSkillLocked(id)
	if err != nil {
		return skillContentEntry{}, err
	}

	name := current.Name
	description := current.Description
	tags := append([]string{}, current.Tags...)
	enabled := current.Enabled
	content := current.Content

	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	if req.Description != nil {
		description = strings.TrimSpace(*req.Description)
	}
	if req.Tags != nil {
		tags = append([]string{}, *req.Tags...)
	}
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if req.Content != nil {
		content = *req.Content
	}
	if err := r.writeSkillLocked(id, name, description, tags, enabled, content); err != nil {
		return skillContentEntry{}, err
	}
	return r.readSkillLocked(id)
}

func (r *skillCatalogRuntime) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	id = normalizeSkillID(id)
	if id == "" {
		return nil
	}
	err := os.RemoveAll(r.skillDir(id))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (r *skillCatalogRuntime) listEditableSkillsLocked() ([]skillCatalogEntry, error) {
	if err := os.MkdirAll(r.root, 0o755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(r.root)
	if err != nil {
		return nil, err
	}
	out := make([]skillCatalogEntry, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := normalizeSkillID(entry.Name())
		if !validSkillID(id) {
			continue
		}
		skill, err := r.readSkillLocked(id)
		if err != nil {
			continue
		}
		out = append(out, skillCatalogEntry{
			ID:          skill.ID,
			Name:        skill.Name,
			Description: skill.Description,
			Tags:        skill.Tags,
			Enabled:     skill.Enabled,
			Source:      "editable",
			UpdatedAt:   skill.UpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].ID) < strings.ToLower(out[j].ID) })
	return out, nil
}

func (r *skillCatalogRuntime) readSkillLocked(id string) (skillContentEntry, error) {
	id = normalizeSkillID(id)
	if !validSkillID(id) {
		return skillContentEntry{}, fmt.Errorf("invalid skill id")
	}
	skillPath := filepath.Join(r.skillDir(id), "SKILL.md")
	raw, err := os.ReadFile(skillPath)
	if err != nil {
		if os.IsNotExist(err) {
			return skillContentEntry{}, fmt.Errorf("skill %s not found", id)
		}
		return skillContentEntry{}, err
	}
	content := string(raw)
	meta := r.readMetadataLocked(id)

	name := strings.TrimSpace(meta.Name)
	if name == "" {
		name = titleFromMarkdown(content)
	}
	if name == "" {
		name = id
	}
	description := strings.TrimSpace(meta.Description)
	if description == "" {
		description = descriptionFromMarkdown(content)
	}
	tags := dedupeSkillTags(meta.Tags)
	enabled := !meta.Disabled
	updatedAt := time.Now().UnixMilli()
	if fi, statErr := os.Stat(skillPath); statErr == nil {
		updatedAt = fi.ModTime().UnixMilli()
	}
	return skillContentEntry{
		ID:          id,
		Name:        name,
		Description: description,
		Tags:        tags,
		Enabled:     enabled,
		Content:     content,
		UpdatedAt:   updatedAt,
	}, nil
}

func (r *skillCatalogRuntime) writeSkillLocked(id, name, description string, tags []string, enabled bool, content string) error {
	id = normalizeSkillID(id)
	if !validSkillID(id) {
		return fmt.Errorf("invalid id")
	}
	dir := r.skillDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = id
	}
	description = strings.TrimSpace(description)
	tags = dedupeSkillTags(tags)
	content = strings.TrimSpace(content)
	if content == "" {
		content = defaultSkillMarkdown(name, description)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content+"\n"), 0o644); err != nil {
		return err
	}
	meta := skillMetadata{ID: id, Name: name, Description: description, Tags: tags, Disabled: !enabled}
	payload, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), append(payload, '\n'), 0o644); err != nil {
		return err
	}
	return nil
}

func (r *skillCatalogRuntime) readMetadataLocked(id string) skillMetadata {
	var meta skillMetadata
	path := filepath.Join(r.skillDir(id), "metadata.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return meta
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return skillMetadata{}
	}
	meta.ID = normalizeSkillID(meta.ID)
	meta.Name = strings.TrimSpace(meta.Name)
	meta.Description = strings.TrimSpace(meta.Description)
	meta.Tags = dedupeSkillTags(meta.Tags)
	return meta
}

func (r *skillCatalogRuntime) skillDir(id string) string {
	return filepath.Join(r.root, normalizeSkillID(id))
}

func normalizeSkillID(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, " ", "-")
	return value
}

func validSkillID(value string) bool {
	if value == "" {
		return false
	}
	for i, ch := range value {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			continue
		}
		switch ch {
		case '-', '_':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func dedupeSkillTags(tags []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(strings.ToLower(t))
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func defaultSkillMarkdown(name, description string) string {
	title := strings.TrimSpace(name)
	if title == "" {
		title = "Untitled Skill"
	}
	desc := strings.TrimSpace(description)
	if desc == "" {
		desc = "Add practical execution guidance for this skill."
	}
	return "# " + title + "\n\n" +
		desc + "\n\n" +
		"## Execution\n\n" +
		"- Define when this skill should be selected.\n" +
		"- Describe required inputs and expected outputs.\n" +
		"- Include safety and validation checks.\n"
}

func composeSkillMarkdown(req skillComposeRequest) string {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.TrimSpace(req.ID)
	}
	if name == "" {
		name = "Untitled Skill"
	}
	description := strings.TrimSpace(req.Description)
	if description == "" {
		description = "Add practical execution guidance for this skill."
	}

	var b strings.Builder
	b.WriteString("# " + name + "\n\n")
	b.WriteString(description + "\n\n")
	writeSkillSection(&b, "When To Use", req.Trigger, []string{
		"Use this skill when the request matches the purpose above.",
	})
	if len(req.AssignedAgentIDs) > 0 {
		writeSkillSection(&b, "Intended Agents", strings.Join(req.AssignedAgentIDs, "\n"), nil)
	}
	writeSkillSection(&b, "Inputs", req.Inputs, []string{
		"Clarify missing inputs before acting.",
	})
	writeSkillSection(&b, "Expected Output", req.Outputs, []string{
		"Return the smallest useful artifact or answer that satisfies the request.",
	})
	writeSkillSection(&b, "Execution", req.Notes, []string{
		"Inspect relevant project context first.",
		"Prefer existing repo conventions and keep edits scoped.",
		"Run focused validation before reporting completion.",
	})
	return strings.TrimSpace(b.String())
}

func writeSkillSection(b *strings.Builder, title, body string, fallback []string) {
	b.WriteString("## " + title + "\n\n")
	lines := normalizedSkillLines(body)
	if len(lines) == 0 {
		lines = fallback
	}
	for _, line := range lines {
		b.WriteString("- " + line + "\n")
	}
	b.WriteString("\n")
}

func normalizedSkillLines(value string) []string {
	out := make([]string, 0)
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func titleFromMarkdown(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func descriptionFromMarkdown(content string) string {
	lines := strings.Split(content, "\n")
	headingSeen := false
	paragraph := make([]string, 0)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !headingSeen {
			headingSeen = strings.HasPrefix(trimmed, "# ")
			continue
		}
		if trimmed == "" {
			if len(paragraph) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			break
		}
		paragraph = append(paragraph, trimmed)
	}
	return strings.Join(paragraph, " ")
}

func registerSkillCatalogRoutes(mux *http.ServeMux, runtime *skillCatalogRuntime) {
	mux.HandleFunc("/skills/compose", func(w http.ResponseWriter, r *http.Request) {
		if runtime == nil {
			http.Error(w, "skill runtime unavailable", http.StatusServiceUnavailable)
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
		var body skillComposeRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		created, err := runtime.Compose(body)
		if err != nil {
			http.Error(w, "skill compose failed: "+err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(created)
	})

	mux.HandleFunc("/skills/catalog", func(w http.ResponseWriter, r *http.Request) {
		if runtime == nil {
			http.Error(w, "skill runtime unavailable", http.StatusServiceUnavailable)
			return
		}
		if !isAllowedChatRequest(r) {
			http.Error(w, "chat requests must originate from this host", http.StatusForbidden)
			return
		}
		switch r.Method {
		case http.MethodGet:
			catalog, err := runtime.Catalog()
			if err != nil {
				http.Error(w, "skill catalog unavailable: "+err.Error(), http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(catalog)
		case http.MethodPost:
			var body skillUpsertRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			created, err := runtime.Create(body)
			if err != nil {
				http.Error(w, "skill create failed: "+err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(created)
		default:
			http.Error(w, "GET or POST only", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/skills/catalog/", func(w http.ResponseWriter, r *http.Request) {
		if runtime == nil {
			http.Error(w, "skill runtime unavailable", http.StatusServiceUnavailable)
			return
		}
		if !isAllowedChatRequest(r) {
			http.Error(w, "chat requests must originate from this host", http.StatusForbidden)
			return
		}
		id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/skills/catalog/"))
		if id == "" {
			http.Error(w, "skill id required", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodGet:
			item, err := runtime.Get(id)
			if err != nil {
				http.Error(w, "skill read failed: "+err.Error(), http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(item)
		case http.MethodPatch:
			var body skillPatchRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			updated, err := runtime.Update(id, body)
			if err != nil {
				http.Error(w, "skill update failed: "+err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(updated)
		case http.MethodDelete:
			if err := runtime.Delete(id); err != nil {
				http.Error(w, "skill delete failed: "+err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "GET, PATCH, or DELETE only", http.StatusMethodNotAllowed)
		}
	})
}
