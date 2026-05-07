// sessions.go — multi-turn conversation memory for AGT-sage. Backed by Redis
// so sessions survive manager restarts and container redeploys. Keyed by an
// opaque contextID (the chat surface decides its shape — local-<userid>,
// discord channel id, etc.).
//
// The store keeps a per-context LIST of JSON-serialized ChatMessage entries
// at sage:session:<contextID>. Reads load the whole list (Redis LRANGE 0 -1),
// writes RPUSH a single message, and trim is an LTRIM to the most recent N.
package sageagents

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
)

// SessionStore is the abstraction over per-context conversation history.
// All operations are best-effort — failures are logged but do not return an
// error; Sage falls back to a stateless turn rather than blocking the user.
type SessionStore interface {
	Append(ctx context.Context, contextID string, msg ChatMessage)
	Load(ctx context.Context, contextID string) []ChatMessage
	Trim(ctx context.Context, contextID string, maxTurns int)
}

// noopSessionStore is the fallback when no Redis client is available.
type noopSessionStore struct{}

func (noopSessionStore) Append(_ context.Context, _ string, _ ChatMessage) {}
func (noopSessionStore) Load(_ context.Context, _ string) []ChatMessage    { return nil }
func (noopSessionStore) Trim(_ context.Context, _ string, _ int)           {}

// NewNoopSessionStore returns a SessionStore that drops everything. Used when
// REDIS_ADDR is unset OR the Redis ping fails at startup.
func NewNoopSessionStore() SessionStore { return noopSessionStore{} }

// RedisSessionStore persists messages in a Redis LIST per context.
type RedisSessionStore struct {
	Client *redis.Client
	TTL    time.Duration // 0 = no expiry
}

// NewRedisSessionStore wires up a store with a TTL refresh on every write.
// ttlHours of 0 disables expiry entirely (not recommended in production).
func NewRedisSessionStore(client *redis.Client, ttlHours int) *RedisSessionStore {
	var ttl time.Duration
	if ttlHours > 0 {
		ttl = time.Duration(ttlHours) * time.Hour
	}
	return &RedisSessionStore{Client: client, TTL: ttl}
}

func sessionKey(contextID string) string {
	return "sage:session:" + contextID
}

// Append RPUSHes the encoded message and refreshes the TTL.
func (s *RedisSessionStore) Append(ctx context.Context, contextID string, msg ChatMessage) {
	if s == nil || s.Client == nil || contextID == "" {
		return
	}
	b, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[sessions] marshal failed for %s: %v", contextID, err)
		return
	}
	key := sessionKey(contextID)
	if err := s.Client.RPush(ctx, key, b).Err(); err != nil {
		log.Printf("[sessions] RPUSH %s failed: %v", key, err)
		return
	}
	if s.TTL > 0 {
		if err := s.Client.Expire(ctx, key, s.TTL).Err(); err != nil {
			log.Printf("[sessions] EXPIRE %s failed: %v", key, err)
		}
	}
}

// Load returns all messages currently in the context, oldest first. Returns
// nil on miss or any error — callers continue stateless.
func (s *RedisSessionStore) Load(ctx context.Context, contextID string) []ChatMessage {
	if s == nil || s.Client == nil || contextID == "" {
		return nil
	}
	key := sessionKey(contextID)
	raw, err := s.Client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		log.Printf("[sessions] LRANGE %s failed: %v", key, err)
		return nil
	}
	out := make([]ChatMessage, 0, len(raw))
	for i, item := range raw {
		var m ChatMessage
		if err := json.Unmarshal([]byte(item), &m); err != nil {
			log.Printf("[sessions] unmarshal item %d for %s failed: %v", i, contextID, err)
			continue
		}
		out = append(out, m)
	}
	return out
}

// Trim keeps only the most recent maxTurns messages in the context.
func (s *RedisSessionStore) Trim(ctx context.Context, contextID string, maxTurns int) {
	if s == nil || s.Client == nil || contextID == "" || maxTurns <= 0 {
		return
	}
	key := sessionKey(contextID)
	// LTRIM key -maxTurns -1 keeps the last maxTurns entries.
	if err := s.Client.LTrim(ctx, key, int64(-maxTurns), -1).Err(); err != nil {
		log.Printf("[sessions] LTRIM %s failed: %v", key, err)
	}
}

// formatSessionForLog returns a one-line summary of a loaded session, used
// when debug-logging is enabled. Kept short to avoid noisy logs.
func formatSessionForLog(msgs []ChatMessage) string {
	if len(msgs) == 0 {
		return "(empty)"
	}
	return fmt.Sprintf("%d msgs (last role=%s)", len(msgs), msgs[len(msgs)-1].Role)
}

const (
	chatSessionIndexKey  = "sage:chat:sessions"
	chatSessionMetaPref  = "sage:chat:session:"
	chatMessageListPref  = "sage:chat:messages:"
	defaultChatListLimit = 100
)

// ChatTranscriptMessage is the UI-facing chat transcript shape. It is kept
// separate from ChatMessage so display state never pollutes Sage's prompt
// history.
type ChatTranscriptMessage struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Text      string `json:"text"`
	CreatedAt int64  `json:"createdAt"`
	Status    string `json:"status,omitempty"`
	TaskID    string `json:"taskId,omitempty"`
}

// ChatSessionSummary is a central chat session record shared by every browser.
type ChatSessionSummary struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	CreatedAt     int64  `json:"createdAt"`
	UpdatedAt     int64  `json:"updatedAt"`
	MessageCount  int64  `json:"messageCount"`
	AgentMode     string `json:"agentMode,omitempty"`
	TargetAgentID string `json:"targetAgentId,omitempty"`
	ModeLabel     string `json:"modeLabel,omitempty"`
}

// ChatSessionDetail includes the session metadata and its transcript.
type ChatSessionDetail struct {
	Session  ChatSessionSummary      `json:"session"`
	Messages []ChatTranscriptMessage `json:"messages"`
}

func normalizeTranscriptMessage(msg ChatTranscriptMessage, now int64) ChatTranscriptMessage {
	if msg.CreatedAt == 0 {
		msg.CreatedAt = now
	}
	if msg.ID == "" {
		msg.ID = fmt.Sprintf("msg-%d", msg.CreatedAt)
	}
	if msg.Status == "" && msg.Role == "assistant" {
		msg.Status = "done"
	}
	return msg
}

// RedisChatSessionStore persists UI chat sessions centrally so phone and
// laptop share one source of truth.
type RedisChatSessionStore struct {
	Client *redis.Client
	TTL    time.Duration
	Limit  int64
}

func NewRedisChatSessionStore(client *redis.Client, ttlHours int) *RedisChatSessionStore {
	var ttl time.Duration
	if ttlHours > 0 {
		ttl = time.Duration(ttlHours) * time.Hour
	}
	return &RedisChatSessionStore{Client: client, TTL: ttl, Limit: defaultChatListLimit}
}

func chatSessionMetaKey(contextID string) string {
	return chatSessionMetaPref + contextID
}

func chatMessageListKey(contextID string) string {
	return chatMessageListPref + contextID
}

func (s *RedisChatSessionStore) Ensure(ctx context.Context, contextID, title string) (ChatSessionSummary, error) {
	if s == nil || s.Client == nil {
		return ChatSessionSummary{}, fmt.Errorf("chat session store unavailable")
	}
	contextID = strings.TrimSpace(contextID)
	if contextID == "" {
		return ChatSessionSummary{}, fmt.Errorf("context id required")
	}
	now := time.Now().UnixMilli()
	metaKey := chatSessionMetaKey(contextID)
	current, err := s.Client.HGetAll(ctx, metaKey).Result()
	if err != nil {
		return ChatSessionSummary{}, err
	}
	createdAt := now
	if v := current["createdAt"]; v != "" {
		if parsed, parseErr := strconv.ParseInt(v, 10, 64); parseErr == nil && parsed > 0 {
			createdAt = parsed
		}
	}
	currentTitle := current["title"]
	if strings.TrimSpace(currentTitle) == "" {
		currentTitle = "New chat"
	}
	nextTitle := strings.TrimSpace(title)
	if nextTitle == "" {
		nextTitle = currentTitle
	}
	if currentTitle != "" && currentTitle != "New chat" {
		nextTitle = currentTitle
	}
	if err := s.Client.HSet(ctx, metaKey, map[string]interface{}{
		"id":        contextID,
		"title":     nextTitle,
		"createdAt": createdAt,
		"updatedAt": now,
	}).Err(); err != nil {
		return ChatSessionSummary{}, err
	}
	if err := s.Client.ZAdd(ctx, chatSessionIndexKey, &redis.Z{Score: float64(now), Member: contextID}).Err(); err != nil {
		return ChatSessionSummary{}, err
	}
	s.refreshTTL(ctx, contextID)
	return s.summary(ctx, contextID)
}

func (s *RedisChatSessionStore) Touch(ctx context.Context, contextID, title string) {
	if _, err := s.Ensure(ctx, contextID, title); err != nil {
		log.Printf("[chat-sessions] touch %s failed: %v", contextID, err)
	}
}

func (s *RedisChatSessionStore) AppendMessage(ctx context.Context, contextID string, msg ChatTranscriptMessage) error {
	if s == nil || s.Client == nil {
		return fmt.Errorf("chat session store unavailable")
	}
	if strings.TrimSpace(contextID) == "" {
		return fmt.Errorf("context id required")
	}
	msg = normalizeTranscriptMessage(msg, time.Now().UnixMilli())
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if _, err := s.Ensure(ctx, contextID, ""); err != nil {
		return err
	}
	key := chatMessageListKey(contextID)
	if err := s.Client.RPush(ctx, key, b).Err(); err != nil {
		return err
	}
	limit := s.Limit
	if limit <= 0 {
		limit = defaultChatListLimit
	}
	if err := s.Client.LTrim(ctx, key, -limit, -1).Err(); err != nil {
		return err
	}
	return s.afterMessageWrite(ctx, contextID, msg)
}

// UpsertTaskMessage inserts or replaces the transcript row for a task/role.
// This lets one device see a pending assistant response while another device
// started the task, then see that same row become done/error at completion.
func (s *RedisChatSessionStore) UpsertTaskMessage(ctx context.Context, contextID string, msg ChatTranscriptMessage) error {
	if s == nil || s.Client == nil {
		return fmt.Errorf("chat session store unavailable")
	}
	if strings.TrimSpace(contextID) == "" {
		return fmt.Errorf("context id required")
	}
	if strings.TrimSpace(msg.TaskID) == "" || strings.TrimSpace(msg.Role) == "" {
		return s.AppendMessage(ctx, contextID, msg)
	}
	if _, err := s.Ensure(ctx, contextID, ""); err != nil {
		return err
	}

	key := chatMessageListKey(contextID)
	raw, err := s.Client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		return err
	}
	for i, item := range raw {
		var existing ChatTranscriptMessage
		if err := json.Unmarshal([]byte(item), &existing); err != nil {
			log.Printf("[chat-sessions] unmarshal item %d for %s failed: %v", i, contextID, err)
			continue
		}
		if existing.TaskID != msg.TaskID || existing.Role != msg.Role {
			continue
		}
		if existing.ID != "" {
			msg.ID = existing.ID
		}
		if existing.CreatedAt > 0 {
			msg.CreatedAt = existing.CreatedAt
		}
		msg = normalizeTranscriptMessage(msg, time.Now().UnixMilli())
		b, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		if err := s.Client.LSet(ctx, key, int64(i), b).Err(); err != nil {
			return err
		}
		return s.afterMessageWrite(ctx, contextID, msg)
	}

	return s.AppendMessage(ctx, contextID, msg)
}

func (s *RedisChatSessionStore) UpdateTitle(ctx context.Context, contextID, title string) (ChatSessionSummary, error) {
	if s == nil || s.Client == nil {
		return ChatSessionSummary{}, fmt.Errorf("chat session store unavailable")
	}
	contextID = strings.TrimSpace(contextID)
	if contextID == "" {
		return ChatSessionSummary{}, fmt.Errorf("context id required")
	}
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		title = "New chat"
	}
	if _, err := s.Ensure(ctx, contextID, ""); err != nil {
		return ChatSessionSummary{}, err
	}
	now := time.Now().UnixMilli()
	metaKey := chatSessionMetaKey(contextID)
	if err := s.Client.HSet(ctx, metaKey, map[string]interface{}{
		"title":     title,
		"updatedAt": now,
	}).Err(); err != nil {
		return ChatSessionSummary{}, err
	}
	if err := s.Client.ZAdd(ctx, chatSessionIndexKey, &redis.Z{Score: float64(now), Member: contextID}).Err(); err != nil {
		return ChatSessionSummary{}, err
	}
	s.refreshTTL(ctx, contextID)
	return s.summary(ctx, contextID)
}

func (s *RedisChatSessionStore) UpdateMode(ctx context.Context, contextID string, selection ChatModeSelection) (ChatSessionSummary, error) {
	if s == nil || s.Client == nil {
		return ChatSessionSummary{}, fmt.Errorf("chat session store unavailable")
	}
	contextID = strings.TrimSpace(contextID)
	if contextID == "" {
		return ChatSessionSummary{}, fmt.Errorf("context id required")
	}
	if _, err := s.Ensure(ctx, contextID, ""); err != nil {
		return ChatSessionSummary{}, err
	}
	now := time.Now().UnixMilli()
	metaKey := chatSessionMetaKey(contextID)
	if err := s.Client.HSet(ctx, metaKey, map[string]interface{}{
		"agentMode":     strings.TrimSpace(selection.AgentMode),
		"targetAgentId": strings.TrimSpace(selection.TargetAgentID),
		"modeLabel":     strings.TrimSpace(selection.Label),
		"updatedAt":     now,
	}).Err(); err != nil {
		return ChatSessionSummary{}, err
	}
	if err := s.Client.ZAdd(ctx, chatSessionIndexKey, &redis.Z{Score: float64(now), Member: contextID}).Err(); err != nil {
		return ChatSessionSummary{}, err
	}
	s.refreshTTL(ctx, contextID)
	return s.summary(ctx, contextID)
}

func (s *RedisChatSessionStore) afterMessageWrite(ctx context.Context, contextID string, msg ChatTranscriptMessage) error {
	now := time.Now().UnixMilli()
	metaKey := chatSessionMetaKey(contextID)
	if msg.Role == "user" {
		title := sessionTitleFromContent(msg.Text)
		if title != "" {
			currentTitle, _ := s.Client.HGet(ctx, metaKey, "title").Result()
			if currentTitle == "" || currentTitle == "New chat" {
				_ = s.Client.HSet(ctx, metaKey, "title", title).Err()
			}
		}
	}
	if err := s.Client.HSet(ctx, metaKey, "updatedAt", now).Err(); err != nil {
		return err
	}
	if err := s.Client.ZAdd(ctx, chatSessionIndexKey, &redis.Z{Score: float64(now), Member: contextID}).Err(); err != nil {
		return err
	}
	s.refreshTTL(ctx, contextID)
	return nil
}

func (s *RedisChatSessionStore) List(ctx context.Context, limit int64) ([]ChatSessionSummary, error) {
	if s == nil || s.Client == nil {
		return nil, fmt.Errorf("chat session store unavailable")
	}
	if limit <= 0 {
		limit = 50
	}
	ids, err := s.Client.ZRevRange(ctx, chatSessionIndexKey, 0, limit-1).Result()
	if err != nil {
		return nil, err
	}
	out := make([]ChatSessionSummary, 0, len(ids))
	for _, id := range ids {
		sum, err := s.summary(ctx, id)
		if err != nil {
			log.Printf("[chat-sessions] summary %s failed: %v", id, err)
			continue
		}
		out = append(out, sum)
	}
	return out, nil
}

func (s *RedisChatSessionStore) Get(ctx context.Context, contextID string) (ChatSessionDetail, error) {
	sum, err := s.summary(ctx, contextID)
	if err != nil {
		return ChatSessionDetail{}, err
	}
	raw, err := s.Client.LRange(ctx, chatMessageListKey(contextID), 0, -1).Result()
	if err != nil {
		return ChatSessionDetail{}, err
	}
	msgs := make([]ChatTranscriptMessage, 0, len(raw))
	for i, item := range raw {
		var msg ChatTranscriptMessage
		if err := json.Unmarshal([]byte(item), &msg); err != nil {
			log.Printf("[chat-sessions] unmarshal item %d for %s failed: %v", i, contextID, err)
			continue
		}
		msgs = append(msgs, msg)
	}
	return ChatSessionDetail{Session: sum, Messages: msgs}, nil
}

func (s *RedisChatSessionStore) Delete(ctx context.Context, contextID string) error {
	if s == nil || s.Client == nil {
		return fmt.Errorf("chat session store unavailable")
	}
	pipe := s.Client.TxPipeline()
	pipe.Del(ctx, chatSessionMetaKey(contextID))
	pipe.Del(ctx, chatMessageListKey(contextID))
	pipe.Del(ctx, sessionKey(contextID))
	pipe.ZRem(ctx, chatSessionIndexKey, contextID)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *RedisChatSessionStore) summary(ctx context.Context, contextID string) (ChatSessionSummary, error) {
	if s == nil || s.Client == nil {
		return ChatSessionSummary{}, fmt.Errorf("chat session store unavailable")
	}
	contextID = strings.TrimSpace(contextID)
	if contextID == "" {
		return ChatSessionSummary{}, fmt.Errorf("context id required")
	}
	meta, err := s.Client.HGetAll(ctx, chatSessionMetaKey(contextID)).Result()
	if err != nil {
		return ChatSessionSummary{}, err
	}
	if len(meta) == 0 {
		return ChatSessionSummary{}, redis.Nil
	}
	createdAt := parseInt64Or(meta["createdAt"], time.Now().UnixMilli())
	updatedAt := parseInt64Or(meta["updatedAt"], createdAt)
	count, err := s.Client.LLen(ctx, chatMessageListKey(contextID)).Result()
	if err != nil {
		count = 0
	}
	title := strings.TrimSpace(meta["title"])
	if title == "" {
		title = "New chat"
	}
	return ChatSessionSummary{
		ID:            contextID,
		Title:         title,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
		MessageCount:  count,
		AgentMode:     strings.TrimSpace(meta["agentMode"]),
		TargetAgentID: strings.TrimSpace(meta["targetAgentId"]),
		ModeLabel:     strings.TrimSpace(meta["modeLabel"]),
	}, nil
}

func (s *RedisChatSessionStore) refreshTTL(ctx context.Context, contextID string) {
	if s == nil || s.Client == nil || s.TTL <= 0 {
		return
	}
	_ = s.Client.Expire(ctx, chatSessionMetaKey(contextID), s.TTL).Err()
	_ = s.Client.Expire(ctx, chatMessageListKey(contextID), s.TTL).Err()
	_ = s.Client.Expire(ctx, sessionKey(contextID), s.TTL).Err()
}

func parseInt64Or(raw string, fallback int64) int64 {
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return v
}

func sessionTitleFromContent(content string) string {
	clean := strings.Join(strings.Fields(content), " ")
	if clean == "" {
		return ""
	}
	if len(clean) > 46 {
		return clean[:43] + "..."
	}
	return clean
}
