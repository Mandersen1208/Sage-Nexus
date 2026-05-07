// Package a2a implements Google's Agent2Agent (A2A) protocol event vocabulary
// for use over Redis pub/sub. The wire format mirrors the A2A spec section 4.2
// (TaskStatusUpdateEvent and TaskArtifactUpdateEvent) so that any A2A-aware
// consumer can read Sage's bus without translation.
//
// Spec reference: https://a2a-protocol.org/latest/specification/
package a2a

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/go-redis/redis/v8"
)

// Redis channel names.
const (
	ChannelTasks   = "sage:tasks"   // Sage publishes Message → Manager subscribes
	ChannelEvents  = "sage:events"  // Manager publishes status/artifact updates
	ChannelControl = "sage:control" // Sage publishes continue/stop control messages for paused tasks
)

// ControlMessage is the wire format for resuming an input-required task.
// Sage's delegate_continue MCP tool publishes one of these to ChannelControl
// after the human picks "continue" or "stop".
type ControlMessage struct {
	TaskID   string `json:"taskId"`
	Decision string `json:"decision"` // "continue" | "stop"
	Note     string `json:"note,omitempty"`
}

// TaskState is the canonical A2A task lifecycle state.
type TaskState string

const (
	StateSubmitted     TaskState = "submitted"
	StateWorking       TaskState = "working"
	StateCompleted     TaskState = "completed" // terminal
	StateFailed        TaskState = "failed"    // terminal
	StateCanceled      TaskState = "canceled"  // terminal
	StateRejected      TaskState = "rejected"  // terminal
	StateInputRequired TaskState = "input-required"
	StateAuthRequired  TaskState = "auth-required"
)

// Part is a content fragment (text-only for now; A2A also defines file/data parts).
type Part struct {
	Kind string `json:"kind"` // "text"
	Text string `json:"text,omitempty"`
}

// Message is the A2A request envelope Sage publishes on sage:tasks.
type Message struct {
	Kind      string                 `json:"kind"` // always "message"
	MessageID string                 `json:"messageId"`
	TaskID    string                 `json:"taskId"`
	ContextID string                 `json:"contextId,omitempty"`
	Role      string                 `json:"role"` // "user"
	Parts     []Part                 `json:"parts"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"` // capability, resource, etc.
}

// TaskStatus carries lifecycle state, an optional human-readable message,
// and a timestamp.
type TaskStatus struct {
	State     TaskState `json:"state"`
	Message   *Message  `json:"message,omitempty"`
	Timestamp string    `json:"timestamp"` // RFC3339
}

// TaskStatusUpdateEvent is emitted on every state transition or intermediate
// progress update. Heartbeats reuse this with state=working and an updated
// metadata.activity field.
type TaskStatusUpdateEvent struct {
	Kind      string                 `json:"kind"` // always "status-update"
	TaskID    string                 `json:"taskId"`
	ContextID string                 `json:"contextId,omitempty"`
	Status    TaskStatus             `json:"status"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// Artifact is a generated output (the final reply text, in our case).
type Artifact struct {
	ArtifactID string `json:"artifactId"`
	Parts      []Part `json:"parts"`
}

// TaskArtifactUpdateEvent is emitted when the orchestrator produces output.
// We send a single artifact per task with lastChunk=true.
type TaskArtifactUpdateEvent struct {
	Kind      string                 `json:"kind"` // always "artifact-update"
	TaskID    string                 `json:"taskId"`
	ContextID string                 `json:"contextId,omitempty"`
	Artifact  Artifact               `json:"artifact"`
	LastChunk bool                   `json:"lastChunk,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// ── Constructors ──────────────────────────────────────────────────────────────

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

func NewSubmittedStatus(taskID, contextID string) TaskStatusUpdateEvent {
	return TaskStatusUpdateEvent{
		Kind: "status-update", TaskID: taskID, ContextID: contextID,
		Status: TaskStatus{State: StateSubmitted, Timestamp: nowRFC3339()},
	}
}

func NewWorkingStatus(taskID, contextID, message string, metadata map[string]interface{}) TaskStatusUpdateEvent {
	evt := TaskStatusUpdateEvent{
		Kind: "status-update", TaskID: taskID, ContextID: contextID,
		Status:   TaskStatus{State: StateWorking, Timestamp: nowRFC3339()},
		Metadata: metadata,
	}
	if message != "" {
		evt.Status.Message = &Message{
			Kind: "message", Role: "agent",
			Parts: []Part{{Kind: "text", Text: message}},
		}
	}
	return evt
}

func NewCompletedStatus(taskID, contextID string) TaskStatusUpdateEvent {
	return TaskStatusUpdateEvent{
		Kind: "status-update", TaskID: taskID, ContextID: contextID,
		Status: TaskStatus{State: StateCompleted, Timestamp: nowRFC3339()},
	}
}

// NewInputRequiredStatus signals the task is paused, awaiting a human
// decision. Sage's delegate sees this and returns a paused payload to her;
// she presents the prompt + last reply and asks the user, then publishes a
// ControlMessage on ChannelControl to resume.
func NewInputRequiredStatus(taskID, contextID, prompt string, metadata map[string]interface{}) TaskStatusUpdateEvent {
	evt := TaskStatusUpdateEvent{
		Kind: "status-update", TaskID: taskID, ContextID: contextID,
		Status:   TaskStatus{State: StateInputRequired, Timestamp: nowRFC3339()},
		Metadata: metadata,
	}
	if prompt != "" {
		evt.Status.Message = &Message{
			Kind: "message", Role: "agent",
			Parts: []Part{{Kind: "text", Text: prompt}},
		}
	}
	return evt
}

func NewFailedStatus(taskID, contextID, errMsg string) TaskStatusUpdateEvent {
	return NewFailedStatusWithMeta(taskID, contextID, errMsg, nil)
}

// NewFailedStatusWithMeta is the structured-failure variant. metadata may
// carry: reason (round_cap_exceeded|tool_error|acp_denied|client_aborted|
// manager_crashed|timeout|unknown), last_agent, rounds_completed, trace,
// raw_error. The dashboard uses these to render a readable failure card and
// Sage's MCP delegate forwards them so she can speak to the failure mode.
func NewFailedStatusWithMeta(taskID, contextID, errMsg string, metadata map[string]interface{}) TaskStatusUpdateEvent {
	evt := TaskStatusUpdateEvent{
		Kind: "status-update", TaskID: taskID, ContextID: contextID,
		Status:   TaskStatus{State: StateFailed, Timestamp: nowRFC3339()},
		Metadata: metadata,
	}
	if errMsg != "" {
		evt.Status.Message = &Message{
			Kind: "message", Role: "agent",
			Parts: []Part{{Kind: "text", Text: errMsg}},
		}
	}
	return evt
}

func NewCanceledStatus(taskID, contextID, message string, metadata map[string]interface{}) TaskStatusUpdateEvent {
	evt := TaskStatusUpdateEvent{
		Kind: "status-update", TaskID: taskID, ContextID: contextID,
		Status:   TaskStatus{State: StateCanceled, Timestamp: nowRFC3339()},
		Metadata: metadata,
	}
	if message != "" {
		evt.Status.Message = &Message{
			Kind: "message", Role: "agent",
			Parts: []Part{{Kind: "text", Text: message}},
		}
	}
	return evt
}

func NewArtifactUpdate(taskID, contextID, text string) TaskArtifactUpdateEvent {
	return TaskArtifactUpdateEvent{
		Kind: "artifact-update", TaskID: taskID, ContextID: contextID,
		Artifact: Artifact{
			ArtifactID: "result",
			Parts:      []Part{{Kind: "text", Text: text}},
		},
		LastChunk: true,
	}
}

// ── Publisher ─────────────────────────────────────────────────────────────────

// PublishEvent JSON-marshals evt and publishes it on ChannelEvents. Fire and
// forget — failures are logged but never returned to the caller, so the bus
// can never itself fail a request.
func PublishEvent(rc *redis.Client, evt interface{}) {
	if rc == nil {
		return
	}
	b, err := json.Marshal(evt)
	if err != nil {
		log.Printf("[a2a] marshal failed: %v", err)
		return
	}
	if err := rc.Publish(context.Background(), ChannelEvents, b).Err(); err != nil {
		log.Printf("[a2a] publish failed: %v", err)
	}
}
