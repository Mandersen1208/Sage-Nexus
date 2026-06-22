package sageagents

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-redis/redis/v8"
)

const (
	workContextMetaPrefix   = "sage:workctx:"
	workContextEventsSuffix = ":events"
	workContextTaskPrefix   = "sage:workctx:by-task:"
	workContextTokenPrefix  = "sage:workctx:token:"
	defaultWorkContextLimit = 500
	defaultWorkContextBytes = 12000
)

var tokenLikePattern = regexp.MustCompile(`(?i)\b(sk-[A-Za-z0-9._\-]{16,}|ghp_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,}|xox[baprs]-[A-Za-z0-9\-]{16,}|eyJ[A-Za-z0-9._\-]{16,}|[A-Za-z0-9_\-]{64,})\b`)

// WorkContextStore owns the Redis-backed per-task execution context shared by
// specialist agents. It is intentionally separate from Sage's human chat
// session memory.
type WorkContextStore struct {
	Client        *redis.Client
	TTL           time.Duration
	MaxEvents     int64
	MaxEventBytes int
}

type WorkContextAccess struct {
	ID        string `json:"workContextId"`
	TaskID    string `json:"taskId"`
	ContextID string `json:"contextId,omitempty"`
	Token     string `json:"-"`
}

type WorkContextMeta struct {
	ID        string `json:"id"`
	TaskID    string `json:"taskId"`
	ContextID string `json:"contextId,omitempty"`
	Status    string `json:"status"`
	Actor     string `json:"actor,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type WorkContextEvent struct {
	ID        string                 `json:"id"`
	Kind      string                 `json:"kind"`
	Actor     string                 `json:"actor,omitempty"`
	Summary   string                 `json:"summary"`
	Content   string                 `json:"content,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt int64                  `json:"createdAt"`
}

type WorkContextDetail struct {
	Meta   WorkContextMeta    `json:"meta"`
	Events []WorkContextEvent `json:"events"`
}

type WorkContextFilter struct {
	Limit int64
	Kind  string
	Actor string
}

type workContextRuntime struct {
	Store  *WorkContextStore
	Access WorkContextAccess
}

type workContextKey struct{}

func NewRedisWorkContextStore(client *redis.Client, ttlHours, maxEvents, maxEventBytes int) *WorkContextStore {
	var ttl time.Duration
	if ttlHours > 0 {
		ttl = time.Duration(ttlHours) * time.Hour
	}
	if maxEvents <= 0 {
		maxEvents = defaultWorkContextLimit
	}
	if maxEventBytes <= 0 {
		maxEventBytes = defaultWorkContextBytes
	}
	return &WorkContextStore{
		Client:        client,
		TTL:           ttl,
		MaxEvents:     int64(maxEvents),
		MaxEventBytes: maxEventBytes,
	}
}

func WithWorkContext(ctx context.Context, store *WorkContextStore, access WorkContextAccess) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if store == nil || store.Client == nil || access.ID == "" {
		return ctx
	}
	return context.WithValue(ctx, workContextKey{}, workContextRuntime{Store: store, Access: access})
}

func WorkContextFromContext(ctx context.Context) (WorkContextAccess, bool) {
	runtime, ok := workContextRuntimeFromContext(ctx)
	if !ok {
		return WorkContextAccess{}, false
	}
	return runtime.Access, true
}

func workContextRuntimeFromContext(ctx context.Context) (workContextRuntime, bool) {
	if ctx == nil {
		return workContextRuntime{}, false
	}
	runtime, ok := ctx.Value(workContextKey{}).(workContextRuntime)
	if !ok || runtime.Store == nil || runtime.Access.ID == "" {
		return workContextRuntime{}, false
	}
	return runtime, true
}

func AppendWorkContextEvent(ctx context.Context, kind, actor, summary, content string, metadata map[string]interface{}) {
	runtime, ok := workContextRuntimeFromContext(ctx)
	if !ok {
		return
	}
	chunks := splitContentForWorkContext(content, runtime.Store.MaxEventBytes)
	if len(chunks) == 0 {
		chunks = []string{""}
	}
	for i, chunk := range chunks {
		chunkMeta := cloneWorkContextMetadata(metadata)
		chunkSummary := summary
		if len(chunks) > 1 {
			if chunkMeta == nil {
				chunkMeta = map[string]interface{}{}
			}
			chunkMeta["chunked"] = true
			chunkMeta["chunk_index"] = i + 1
			chunkMeta["chunk_total"] = len(chunks)
			chunkSummary = fmt.Sprintf("%s [part %d/%d]", summary, i+1, len(chunks))
		}
		if _, err := runtime.Store.AppendEvent(ctx, runtime.Access.ID, WorkContextEvent{
			Kind:     kind,
			Actor:    actor,
			Summary:  chunkSummary,
			Content:  chunk,
			Metadata: chunkMeta,
		}); err != nil {
			log.Printf("[work-context] append %s failed: %v", kind, err)
			return
		}
	}
}

func cloneWorkContextMetadata(metadata map[string]interface{}) map[string]interface{} {
	if metadata == nil {
		return nil
	}
	out := make(map[string]interface{}, len(metadata))
	for k, v := range metadata {
		out[k] = v
	}
	return out
}

func splitContentForWorkContext(content string, maxBytes int) []string {
	if content == "" {
		return []string{""}
	}
	if maxBytes <= 0 || len(content) <= maxBytes {
		return []string{content}
	}
	chunks := []string{}
	for start := 0; start < len(content); {
		end := start
		for end < len(content) {
			_, size := utf8.DecodeRuneInString(content[end:])
			if size <= 0 {
				size = 1
			}
			if (end-start)+size > maxBytes {
				if end == start {
					end = start + size
				}
				break
			}
			end += size
		}
		chunks = append(chunks, content[start:end])
		start = end
	}
	return chunks
}

func BuildWorkContextWorkerPrompt(access WorkContextAccess) string {
	if access.ID == "" || access.Token == "" {
		return ""
	}
	return "Agent Work Context is available for this task.\n" +
		"- task_id: " + access.TaskID + "\n" +
		"- work_context_id: " + access.ID + "\n" +
		"- token: " + access.Token + "\n" +
		"Use agent_context_read or agent_context_search to pull prior agent notes when needed. " +
		"Use agent_context_append to add concise findings, decisions, tool results, blockers, or final notes. " +
		"Do not reveal the token to the user, do not quote it in your answer, and keep context writes short."
}

func (s *WorkContextStore) Create(ctx context.Context, taskID, contextID, actor string) (WorkContextAccess, error) {
	if s == nil || s.Client == nil {
		return WorkContextAccess{}, errors.New("work context store unavailable")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return WorkContextAccess{}, errors.New("task id required")
	}
	id, err := randomWorkContextID("wc-", 12)
	if err != nil {
		return WorkContextAccess{}, err
	}
	token, err := randomWorkContextID("wct-", 32)
	if err != nil {
		return WorkContextAccess{}, err
	}
	now := time.Now().UnixMilli()
	meta := map[string]interface{}{
		"id":        id,
		"taskId":    taskID,
		"contextId": contextID,
		"status":    "working",
		"actor":     actor,
		"createdAt": now,
		"updatedAt": now,
	}
	if err := s.Client.HSet(ctx, workContextMetaKey(id), meta).Err(); err != nil {
		return WorkContextAccess{}, err
	}
	if err := s.Client.Set(ctx, workContextTaskKey(taskID), id, s.TTL).Err(); err != nil {
		return WorkContextAccess{}, err
	}
	hash := WorkContextTokenHash(token)
	if err := s.Client.HSet(ctx, workContextTokenKey(hash), map[string]interface{}{
		"workContextId": id,
		"taskId":        taskID,
		"contextId":     contextID,
		"permissions":   "read_write",
		"createdAt":     now,
	}).Err(); err != nil {
		return WorkContextAccess{}, err
	}
	s.refreshTTL(ctx, id, taskID, hash)
	return WorkContextAccess{ID: id, TaskID: taskID, ContextID: contextID, Token: token}, nil
}

func (s *WorkContextStore) AppendEvent(ctx context.Context, id string, event WorkContextEvent) (WorkContextEvent, error) {
	if s == nil || s.Client == nil {
		return WorkContextEvent{}, errors.New("work context store unavailable")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return WorkContextEvent{}, errors.New("work context id required")
	}
	event = s.sanitizeEvent(event)
	raw, err := json.Marshal(event)
	if err != nil {
		return WorkContextEvent{}, err
	}
	key := workContextEventsKey(id)
	if err := s.Client.RPush(ctx, key, raw).Err(); err != nil {
		return WorkContextEvent{}, err
	}
	if s.MaxEvents > 0 {
		if err := s.Client.LTrim(ctx, key, -s.MaxEvents, -1).Err(); err != nil {
			return WorkContextEvent{}, err
		}
	}
	if err := s.Client.HSet(ctx, workContextMetaKey(id), "updatedAt", time.Now().UnixMilli()).Err(); err != nil {
		return WorkContextEvent{}, err
	}
	s.refreshTTLForID(ctx, id)
	return event, nil
}

func (s *WorkContextStore) Read(ctx context.Context, id string, filter WorkContextFilter) (WorkContextDetail, error) {
	if s == nil || s.Client == nil {
		return WorkContextDetail{}, errors.New("work context store unavailable")
	}
	meta, err := s.ReadMeta(ctx, id)
	if err != nil {
		return WorkContextDetail{}, err
	}
	limit := filter.Limit
	if limit <= 0 || limit > s.MaxEvents {
		limit = s.MaxEvents
	}
	start := int64(0)
	if limit > 0 {
		start = -limit
	}
	raw, err := s.Client.LRange(ctx, workContextEventsKey(id), start, -1).Result()
	if err != nil {
		return WorkContextDetail{}, err
	}
	events := make([]WorkContextEvent, 0, len(raw))
	kind := strings.TrimSpace(filter.Kind)
	actor := strings.TrimSpace(filter.Actor)
	for _, item := range raw {
		var event WorkContextEvent
		if err := json.Unmarshal([]byte(item), &event); err != nil {
			continue
		}
		if kind != "" && event.Kind != kind {
			continue
		}
		if actor != "" && event.Actor != actor {
			continue
		}
		events = append(events, event)
	}
	return WorkContextDetail{Meta: meta, Events: events}, nil
}

func (s *WorkContextStore) ReadMeta(ctx context.Context, id string) (WorkContextMeta, error) {
	if s == nil || s.Client == nil {
		return WorkContextMeta{}, errors.New("work context store unavailable")
	}
	values, err := s.Client.HGetAll(ctx, workContextMetaKey(id)).Result()
	if err != nil {
		return WorkContextMeta{}, err
	}
	if len(values) == 0 {
		return WorkContextMeta{}, redis.Nil
	}
	return workContextMetaFromMap(values), nil
}

func (s *WorkContextStore) SetStatus(ctx context.Context, id, status string) {
	if s == nil || s.Client == nil || strings.TrimSpace(id) == "" {
		return
	}
	if err := s.Client.HSet(ctx, workContextMetaKey(id), map[string]interface{}{
		"status":    strings.TrimSpace(status),
		"updatedAt": time.Now().UnixMilli(),
	}).Err(); err != nil {
		log.Printf("[work-context] status %s failed: %v", id, err)
		return
	}
	s.refreshTTLForID(ctx, id)
}

func (s *WorkContextStore) Authorize(ctx context.Context, id, token string) error {
	_, err := s.AccessForToken(ctx, id, token)
	return err
}

func (s *WorkContextStore) AccessForToken(ctx context.Context, id, token string) (WorkContextAccess, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return WorkContextAccess{}, errors.New("work context id required")
	}
	access, err := s.authorizeToken(ctx, token)
	if err != nil {
		return WorkContextAccess{}, err
	}
	if subtle.ConstantTimeCompare([]byte(access.ID), []byte(id)) != 1 {
		return WorkContextAccess{}, errors.New("work context token does not match requested context")
	}
	return access, nil
}

func (s *WorkContextStore) ResolveByTaskWithToken(ctx context.Context, taskID, token string) (WorkContextAccess, error) {
	taskID = strings.TrimSpace(taskID)
	access, err := s.authorizeToken(ctx, token)
	if err != nil {
		return WorkContextAccess{}, err
	}
	if subtle.ConstantTimeCompare([]byte(access.TaskID), []byte(taskID)) != 1 {
		return WorkContextAccess{}, errors.New("work context token does not match requested task")
	}
	id, err := s.Client.Get(ctx, workContextTaskKey(taskID)).Result()
	if err != nil {
		return WorkContextAccess{}, err
	}
	if subtle.ConstantTimeCompare([]byte(access.ID), []byte(id)) != 1 {
		return WorkContextAccess{}, errors.New("work context task mapping mismatch")
	}
	return access, nil
}

func (s *WorkContextStore) authorizeToken(ctx context.Context, token string) (WorkContextAccess, error) {
	if s == nil || s.Client == nil {
		return WorkContextAccess{}, errors.New("work context store unavailable")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return WorkContextAccess{}, errors.New("work context token required")
	}
	values, err := s.Client.HGetAll(ctx, workContextTokenKey(WorkContextTokenHash(token))).Result()
	if err != nil {
		return WorkContextAccess{}, err
	}
	if len(values) == 0 {
		return WorkContextAccess{}, errors.New("invalid work context token")
	}
	return WorkContextAccess{
		ID:        values["workContextId"],
		TaskID:    values["taskId"],
		ContextID: values["contextId"],
		Token:     token,
	}, nil
}

func WorkContextTokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func RedactSecretsInString(input string) string {
	if input == "" {
		return input
	}
	input = strings.ReplaceAll(input, "\r\n", "\n")
	input = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]+`).ReplaceAllString(input, "Bearer [REDACTED]")
	return tokenLikePattern.ReplaceAllString(input, "[REDACTED]")
}

func RedactWorkContextValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(v))
		for key, item := range v {
			if isSecretLikeKey(key) {
				out[key] = "[REDACTED]"
				continue
			}
			out[key] = RedactWorkContextValue(item)
		}
		return out
	case map[string]string:
		out := make(map[string]interface{}, len(v))
		for key, item := range v {
			if isSecretLikeKey(key) {
				out[key] = "[REDACTED]"
				continue
			}
			out[key] = RedactSecretsInString(item)
		}
		return out
	case []interface{}:
		out := make([]interface{}, 0, len(v))
		for _, item := range v {
			out = append(out, RedactWorkContextValue(item))
		}
		return out
	case []string:
		out := make([]interface{}, 0, len(v))
		for _, item := range v {
			out = append(out, RedactSecretsInString(item))
		}
		return out
	case string:
		return RedactSecretsInString(v)
	default:
		return value
	}
}

func (s *WorkContextStore) sanitizeEvent(event WorkContextEvent) WorkContextEvent {
	now := time.Now().UnixMilli()
	if event.ID == "" {
		event.ID = fmt.Sprintf("wce-%d-%s", now, randomSuffix(4))
	}
	if event.CreatedAt == 0 {
		event.CreatedAt = now
	}
	event.Kind = capWorkContextString(strings.TrimSpace(event.Kind), 120)
	event.Actor = capWorkContextString(strings.TrimSpace(event.Actor), 160)
	event.Summary = capWorkContextString(RedactSecretsInString(strings.TrimSpace(event.Summary)), s.MaxEventBytes)
	event.Content = capWorkContextString(RedactSecretsInString(strings.TrimSpace(event.Content)), s.MaxEventBytes)
	if event.Metadata != nil {
		if redacted, ok := RedactWorkContextValue(event.Metadata).(map[string]interface{}); ok {
			event.Metadata = redacted
		}
	}
	return event
}

func (s *WorkContextStore) refreshTTL(ctx context.Context, id, taskID, tokenHash string) {
	if s == nil || s.Client == nil || s.TTL <= 0 {
		return
	}
	keys := []string{workContextMetaKey(id), workContextEventsKey(id), workContextTaskKey(taskID), workContextTokenKey(tokenHash)}
	for _, key := range keys {
		_ = s.Client.Expire(ctx, key, s.TTL).Err()
	}
}

func (s *WorkContextStore) refreshTTLForID(ctx context.Context, id string) {
	if s == nil || s.Client == nil || s.TTL <= 0 || id == "" {
		return
	}
	_ = s.Client.Expire(ctx, workContextMetaKey(id), s.TTL).Err()
	_ = s.Client.Expire(ctx, workContextEventsKey(id), s.TTL).Err()
}

func workContextMetaKey(id string) string {
	return workContextMetaPrefix + strings.TrimSpace(id) + ":meta"
}

func workContextEventsKey(id string) string {
	return workContextMetaPrefix + strings.TrimSpace(id) + workContextEventsSuffix
}

func workContextTaskKey(taskID string) string {
	return workContextTaskPrefix + strings.TrimSpace(taskID)
}

func workContextTokenKey(hash string) string {
	return workContextTokenPrefix + strings.TrimSpace(hash)
}

func workContextMetaFromMap(values map[string]string) WorkContextMeta {
	return WorkContextMeta{
		ID:        values["id"],
		TaskID:    values["taskId"],
		ContextID: values["contextId"],
		Status:    values["status"],
		Actor:     values["actor"],
		CreatedAt: parseInt64Default(values["createdAt"], 0),
		UpdatedAt: parseInt64Default(values["updatedAt"], 0),
	}
}

func parseInt64Default(raw string, def int64) int64 {
	var out int64
	if _, err := fmt.Sscanf(raw, "%d", &out); err != nil {
		return def
	}
	return out
}

func randomWorkContextID(prefix string, bytesLen int) (string, error) {
	b := make([]byte, bytesLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}

func randomSuffix(bytesLen int) string {
	id, err := randomWorkContextID("", bytesLen)
	if err != nil {
		return "rand"
	}
	return id
}

func capWorkContextString(input string, maxBytes int) string {
	if maxBytes <= 0 || len(input) <= maxBytes {
		return input
	}
	if maxBytes < 24 {
		return input[:maxBytes]
	}
	return input[:maxBytes-24] + " ... [truncated]"
}

func isSecretLikeKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), " ", "_"))
	for _, marker := range []string{
		"token", "secret", "password", "passwd", "cookie", "authorization", "api_key", "apikey",
		"access_key", "private_key", "refresh", "credential", "capability_token",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}
