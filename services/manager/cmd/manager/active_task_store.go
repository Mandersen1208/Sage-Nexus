package main

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

const (
	activeTaskStateActive    = "active"
	activeTaskStateWaiting   = "waiting"
	activeTaskStateCompleted = "completed"

	activeRunStateRunning       = "running"
	activeRunStateInputRequired = "input-required"
	activeRunStateStopped       = "stopped"
	activeRunStateFailed        = "failed"
	activeRunStateCompleted     = "completed"
	activeRunStateCanceled      = "canceled"
)

type chatActiveTaskStore struct {
	rc  *redis.Client
	ttl time.Duration
}

type chatActiveTaskPointer struct {
	ContextID                 string `json:"contextId"`
	ActiveTaskID              string `json:"activeTaskId"`
	TaskState                 string `json:"taskState"`
	ActiveRunID               string `json:"activeRunId"`
	RunState                  string `json:"runState"`
	Objective                 string `json:"objective"`
	PendingQuestion           string `json:"pendingQuestion,omitempty"`
	StableCheckpointMessageID string `json:"stableCheckpointMessageId,omitempty"`
	StableCheckpointEventID   string `json:"stableCheckpointEventId,omitempty"`
	WorkContextID             string `json:"workContextId,omitempty"`
	CreatedAt                 int64  `json:"createdAt"`
	UpdatedAt                 int64  `json:"updatedAt"`
	ExpiresAt                 int64  `json:"expiresAt"`
}

type chatTaskRunRecord struct {
	RunID        string `json:"runId"`
	ActiveTaskID string `json:"activeTaskId"`
	ContextID    string `json:"contextId"`
	State        string `json:"state"`
	Objective    string `json:"objective"`
	CreatedAt    int64  `json:"createdAt"`
	UpdatedAt    int64  `json:"updatedAt"`
}

func newChatActiveTaskStore(rc *redis.Client, ttlHours int) *chatActiveTaskStore {
	if rc == nil {
		return nil
	}
	ttl := time.Duration(ttlHours) * time.Hour
	if ttl <= 0 {
		ttl = 168 * time.Hour
	}
	return &chatActiveTaskStore{rc: rc, ttl: ttl}
}

func chatActiveTaskKey(contextID string) string {
	return "sage:chat:" + contextID + ":active_task"
}

func chatTaskStateKey(activeTaskID string) string {
	return "sage:task:" + activeTaskID + ":state"
}

func chatTaskRunsKey(activeTaskID string) string {
	return "sage:task:" + activeTaskID + ":runs"
}

func chatTaskCheckpointKey(activeTaskID string) string {
	return "sage:task:" + activeTaskID + ":checkpoint"
}

func chatTaskIgnoredEventsKey(activeTaskID string) string {
	return "sage:task:" + activeTaskID + ":ignored_events"
}

func chatRunTaskKey(runID string) string {
	return "sage:run:" + runID + ":active_task"
}

func (s *chatActiveTaskStore) get(ctx context.Context, contextID string) (chatActiveTaskPointer, bool) {
	if s == nil || s.rc == nil || strings.TrimSpace(contextID) == "" {
		return chatActiveTaskPointer{}, false
	}
	values, err := s.rc.HGetAll(ctx, chatActiveTaskKey(contextID)).Result()
	if err != nil {
		log.Printf("[active-task] read %s failed: %v", contextID, err)
		return chatActiveTaskPointer{}, false
	}
	if len(values) == 0 {
		return chatActiveTaskPointer{}, false
	}
	ptr := activeTaskPointerFromMap(values)
	if ptr.ExpiresAt > 0 && ptr.ExpiresAt <= time.Now().UnixMilli() {
		_ = s.rc.Del(ctx, chatActiveTaskKey(ptr.ContextID)).Err()
		return chatActiveTaskPointer{}, false
	}
	return ptr, ptr.ActiveTaskID != ""
}

func (s *chatActiveTaskStore) startRun(ctx context.Context, ptr chatActiveTaskPointer, runID, objective, checkpointMessageID string) (chatActiveTaskPointer, error) {
	if s == nil || s.rc == nil {
		return chatActiveTaskPointer{}, fmt.Errorf("active task store unavailable")
	}
	now := time.Now().UnixMilli()
	if ptr.ContextID == "" {
		return chatActiveTaskPointer{}, fmt.Errorf("context id required")
	}
	if ptr.ActiveTaskID == "" {
		ptr.ActiveTaskID = newActiveTaskID()
		ptr.CreatedAt = now
	}
	if ptr.CreatedAt == 0 {
		ptr.CreatedAt = now
	}
	if strings.TrimSpace(objective) != "" {
		ptr.Objective = strings.TrimSpace(objective)
	}
	ptr.TaskState = activeTaskStateActive
	ptr.ActiveRunID = runID
	ptr.RunState = activeRunStateRunning
	ptr.PendingQuestion = ""
	ptr.StableCheckpointMessageID = checkpointMessageID
	ptr.UpdatedAt = now
	ptr.ExpiresAt = now + s.ttl.Milliseconds()
	if err := s.writePointer(ctx, ptr); err != nil {
		return chatActiveTaskPointer{}, err
	}
	run := chatTaskRunRecord{
		RunID:        runID,
		ActiveTaskID: ptr.ActiveTaskID,
		ContextID:    ptr.ContextID,
		State:        activeRunStateRunning,
		Objective:    ptr.Objective,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	raw, _ := json.Marshal(run)
	pipe := s.rc.Pipeline()
	pipe.RPush(ctx, chatTaskRunsKey(ptr.ActiveTaskID), raw)
	pipe.HSet(ctx, chatRunTaskKey(runID), map[string]interface{}{
		"activeTaskId": ptr.ActiveTaskID,
		"contextId":    ptr.ContextID,
		"state":        activeRunStateRunning,
		"createdAt":    now,
		"updatedAt":    now,
	})
	pipe.HSet(ctx, chatTaskCheckpointKey(ptr.ActiveTaskID), map[string]interface{}{
		"stableCheckpointMessageId": checkpointMessageID,
		"updatedAt":                 now,
	})
	s.expireKeys(pipe, ptr.ContextID, ptr.ActiveTaskID, runID)
	_, err := pipe.Exec(ctx)
	return ptr, err
}

func (s *chatActiveTaskStore) markInputRequired(ctx context.Context, contextID, activeTaskID, runID, prompt, workContextID string) {
	s.updatePointer(ctx, contextID, activeTaskID, runID, activeTaskStateWaiting, activeRunStateInputRequired, prompt, workContextID)
}

func (s *chatActiveTaskStore) markRunRunning(ctx context.Context, contextID, activeTaskID, runID string) {
	s.updatePointer(ctx, contextID, activeTaskID, runID, activeTaskStateActive, activeRunStateRunning, "", "")
}

func (s *chatActiveTaskStore) markRunStopped(ctx context.Context, contextID, activeTaskID, runID string) {
	s.markIgnoredRun(ctx, activeTaskID, runID, "stopped_by_user")
	s.updatePointer(ctx, contextID, activeTaskID, runID, activeTaskStateWaiting, activeRunStateStopped, "", "")
}

func (s *chatActiveTaskStore) markRunFailed(ctx context.Context, contextID, activeTaskID, runID string) {
	s.markIgnoredRun(ctx, activeTaskID, runID, "run_failed")
	s.updatePointer(ctx, contextID, activeTaskID, runID, activeTaskStateWaiting, activeRunStateFailed, "", "")
}

func (s *chatActiveTaskStore) markRunCanceled(ctx context.Context, contextID, activeTaskID, runID string) {
	s.markIgnoredRun(ctx, activeTaskID, runID, "run_canceled")
	s.updatePointer(ctx, contextID, activeTaskID, runID, activeTaskStateWaiting, activeRunStateCanceled, "", "")
}

func (s *chatActiveTaskStore) markRunCompleted(ctx context.Context, contextID, activeTaskID, runID string) {
	s.updatePointer(ctx, contextID, activeTaskID, runID, activeTaskStateCompleted, activeRunStateCompleted, "", "")
}

func (s *chatActiveTaskStore) resolveRun(ctx context.Context, runID string) (chatActiveTaskPointer, bool) {
	if s == nil || s.rc == nil || strings.TrimSpace(runID) == "" {
		return chatActiveTaskPointer{}, false
	}
	values, err := s.rc.HGetAll(ctx, chatRunTaskKey(runID)).Result()
	if err != nil || len(values) == 0 {
		return chatActiveTaskPointer{}, false
	}
	contextID := values["contextId"]
	if contextID == "" {
		return chatActiveTaskPointer{}, false
	}
	ptr, ok := s.get(ctx, contextID)
	return ptr, ok && ptr.ActiveTaskID == values["activeTaskId"]
}

func (s *chatActiveTaskStore) clearIfTask(ctx context.Context, contextID, activeTaskID string) {
	if s == nil || s.rc == nil || strings.TrimSpace(contextID) == "" || strings.TrimSpace(activeTaskID) == "" {
		return
	}
	values, err := s.rc.HGetAll(ctx, chatActiveTaskKey(contextID)).Result()
	if err != nil || values["activeTaskId"] != activeTaskID {
		return
	}
	if err := s.rc.Del(ctx, chatActiveTaskKey(contextID)).Err(); err != nil {
		log.Printf("[active-task] clear %s failed: %v", contextID, err)
	}
}

func (s *chatActiveTaskStore) updatePointer(ctx context.Context, contextID, activeTaskID, runID, taskState, runState, pendingQuestion, workContextID string) {
	if s == nil || s.rc == nil || strings.TrimSpace(contextID) == "" || strings.TrimSpace(activeTaskID) == "" {
		return
	}
	ptr, ok := s.get(ctx, contextID)
	if !ok || ptr.ActiveTaskID != activeTaskID {
		return
	}
	now := time.Now().UnixMilli()
	ptr.TaskState = taskState
	ptr.RunState = runState
	ptr.ActiveRunID = runID
	ptr.UpdatedAt = now
	ptr.ExpiresAt = now + s.ttl.Milliseconds()
	if pendingQuestion != "" {
		ptr.PendingQuestion = pendingQuestion
	}
	if workContextID != "" {
		ptr.WorkContextID = workContextID
	}
	if err := s.writePointer(ctx, ptr); err != nil {
		log.Printf("[active-task] update %s failed: %v", activeTaskID, err)
		return
	}
	_ = s.rc.HSet(ctx, chatRunTaskKey(runID), map[string]interface{}{
		"state":     runState,
		"updatedAt": now,
	}).Err()
}

func (s *chatActiveTaskStore) writePointer(ctx context.Context, ptr chatActiveTaskPointer) error {
	meta := map[string]interface{}{
		"contextId":                 ptr.ContextID,
		"activeTaskId":              ptr.ActiveTaskID,
		"taskState":                 ptr.TaskState,
		"activeRunId":               ptr.ActiveRunID,
		"runState":                  ptr.RunState,
		"objective":                 ptr.Objective,
		"pendingQuestion":           ptr.PendingQuestion,
		"stableCheckpointMessageId": ptr.StableCheckpointMessageID,
		"stableCheckpointEventId":   ptr.StableCheckpointEventID,
		"workContextId":             ptr.WorkContextID,
		"createdAt":                 ptr.CreatedAt,
		"updatedAt":                 ptr.UpdatedAt,
		"expiresAt":                 ptr.ExpiresAt,
	}
	pipe := s.rc.Pipeline()
	pipe.HSet(ctx, chatActiveTaskKey(ptr.ContextID), meta)
	pipe.HSet(ctx, chatTaskStateKey(ptr.ActiveTaskID), meta)
	s.expireKeys(pipe, ptr.ContextID, ptr.ActiveTaskID, ptr.ActiveRunID)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *chatActiveTaskStore) markIgnoredRun(ctx context.Context, activeTaskID, runID, reason string) {
	if s == nil || s.rc == nil || strings.TrimSpace(activeTaskID) == "" || strings.TrimSpace(runID) == "" {
		return
	}
	raw, _ := json.Marshal(map[string]interface{}{
		"runId":     runID,
		"reason":    reason,
		"ignored":   true,
		"createdAt": time.Now().UnixMilli(),
	})
	if err := s.rc.RPush(ctx, chatTaskIgnoredEventsKey(activeTaskID), raw).Err(); err != nil {
		log.Printf("[active-task] ignored run %s failed: %v", runID, err)
	}
	_ = s.rc.Expire(ctx, chatTaskIgnoredEventsKey(activeTaskID), s.ttl).Err()
}

func (s *chatActiveTaskStore) expireKeys(pipe redis.Pipeliner, contextID, activeTaskID, runID string) {
	if s == nil || s.ttl <= 0 {
		return
	}
	if contextID != "" {
		pipe.Expire(context.Background(), chatActiveTaskKey(contextID), s.ttl)
	}
	if activeTaskID != "" {
		pipe.Expire(context.Background(), chatTaskStateKey(activeTaskID), s.ttl)
		pipe.Expire(context.Background(), chatTaskRunsKey(activeTaskID), s.ttl)
		pipe.Expire(context.Background(), chatTaskCheckpointKey(activeTaskID), s.ttl)
		pipe.Expire(context.Background(), chatTaskIgnoredEventsKey(activeTaskID), s.ttl)
	}
	if runID != "" {
		pipe.Expire(context.Background(), chatRunTaskKey(runID), s.ttl)
	}
}

func activeTaskPointerFromMap(values map[string]string) chatActiveTaskPointer {
	return chatActiveTaskPointer{
		ContextID:                 values["contextId"],
		ActiveTaskID:              values["activeTaskId"],
		TaskState:                 values["taskState"],
		ActiveRunID:               values["activeRunId"],
		RunState:                  values["runState"],
		Objective:                 values["objective"],
		PendingQuestion:           values["pendingQuestion"],
		StableCheckpointMessageID: values["stableCheckpointMessageId"],
		StableCheckpointEventID:   values["stableCheckpointEventId"],
		WorkContextID:             values["workContextId"],
		CreatedAt:                 parseRedisMillis(values["createdAt"]),
		UpdatedAt:                 parseRedisMillis(values["updatedAt"]),
		ExpiresAt:                 parseRedisMillis(values["expiresAt"]),
	}
}

func parseRedisMillis(raw string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	return n
}
