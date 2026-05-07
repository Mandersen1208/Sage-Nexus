package sageagents

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/matta/sage-nexus/services/manager/a2a"
)

// ProgressEvent is a single streaming update emitted during a dispatch.
// Fields are optional — only those relevant to the event type are set.
type ProgressEvent struct {
	Type          string `json:"type"`
	RequestID     string `json:"request_id,omitempty"`
	Agent         string `json:"agent,omitempty"`
	Tool          string `json:"tool,omitempty"`
	Phase         string `json:"phase,omitempty"`
	Mode          string `json:"mode,omitempty"`
	RouteReason   string `json:"route_reason,omitempty"`
	RevoicePolicy string `json:"revoice_policy,omitempty"`
	RevoiceMode   string `json:"revoice_mode,omitempty"`
	SkipReason    string `json:"skip_reason,omitempty"`
	WorkContextID string `json:"work_context_id,omitempty"`
	DurationMS    int64  `json:"duration_ms,omitempty"`
	ElapsedMS     int64  `json:"elapsed_ms,omitempty"`
	Message       string `json:"message,omitempty"`
	Error         string `json:"error,omitempty"`
	Timestamp     int64  `json:"timestamp,omitempty"`
}

// ProgressSink consumes progress events. Implementations must be non-blocking
// (typically a buffered channel send with default drop).
type ProgressSink func(ProgressEvent)

type progressKey struct{}

// WithProgressSink returns a ctx carrying the given sink.
func WithProgressSink(ctx context.Context, sink ProgressSink) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, progressKey{}, sink)
}

// EmitProgress sends an event to the sink stashed in ctx (if any).
// No-op when ctx has no sink, so call sites can emit unconditionally.
func EmitProgress(ctx context.Context, evt ProgressEvent) {
	if ctx == nil {
		return
	}
	sink, ok := ctx.Value(progressKey{}).(ProgressSink)
	if !ok || sink == nil {
		return
	}
	sink(evt)
}

// EmitLatencySpan records the duration of a named internal phase. It reuses
// the existing progress bus so the dashboard can show latency without a new API.
func EmitLatencySpan(ctx context.Context, span string, start time.Time) {
	EmitProgress(ctx, ProgressEvent{
		Type:       "latency",
		Tool:       span,
		Phase:      "end",
		DurationMS: time.Since(start).Milliseconds(),
		Timestamp:  time.Now().Unix(),
	})
}

// A2AProgressSink returns a ProgressSink that translates each ProgressEvent
// into an A2A TaskStatusUpdateEvent and publishes it to sage:events. If base
// is non-nil it is invoked first (used to compose Redis publishing with the
// in-process NDJSON streaming sink).
//
// The taskID and contextID are baked into the closure — they identify the
// A2A task that owns these events.
func A2AProgressSink(rc *redis.Client, taskID, contextID string, base ProgressSink, workContextID ...string) ProgressSink {
	var capturedWorkContextID string
	if len(workContextID) > 0 {
		capturedWorkContextID = workContextID[0]
	}
	return func(evt ProgressEvent) {
		if base != nil {
			base(evt)
		}
		if rc == nil {
			return
		}
		if evt.WorkContextID == "" {
			evt.WorkContextID = capturedWorkContextID
		}
		meta := map[string]interface{}{
			"activity": mapActivity(evt.Type),
		}
		if evt.WorkContextID != "" {
			meta["work_context_id"] = evt.WorkContextID
		}
		if evt.Phase != "" {
			meta["phase"] = evt.Phase
		}
		if evt.Mode != "" {
			meta["mode"] = evt.Mode
		}
		if evt.RouteReason != "" {
			meta["route_reason"] = evt.RouteReason
		}
		if evt.RevoicePolicy != "" {
			meta["revoice_policy"] = evt.RevoicePolicy
		}
		if evt.RevoiceMode != "" {
			meta["revoice_mode"] = evt.RevoiceMode
		}
		if evt.SkipReason != "" {
			meta["skip_reason"] = evt.SkipReason
		}
		if evt.Message != "" {
			meta["message"] = evt.Message
		}
		if evt.Agent != "" {
			meta["agent"] = evt.Agent
		}
		if evt.Tool != "" {
			meta["tool"] = evt.Tool
		}
		if evt.DurationMS > 0 {
			meta["duration_ms"] = evt.DurationMS
		}
		if evt.ElapsedMS > 0 {
			meta["elapsed_ms"] = evt.ElapsedMS
		}
		if evt.Error != "" {
			meta["error"] = evt.Error
		}
		a2a.PublishEvent(rc, a2a.NewWorkingStatus(taskID, contextID, summarizeProgress(evt), meta))
	}
}

// mapActivity normalizes ProgressEvent.Type into the activity vocabulary used
// by A2A metadata. "tick" becomes "heartbeat" because that's what it is.
func mapActivity(t string) string {
	switch t {
	case "tick":
		return "heartbeat"
	case "latency":
		return "latency"
	default:
		return t
	}
}

// summarizeProgress produces the human-readable status.message text for a
// TaskStatusUpdateEvent. Mirrors the dashboard summarizer in delegate.ts.
func summarizeProgress(evt ProgressEvent) string {
	switch evt.Type {
	case "start":
		return "starting"
	case "route":
		if evt.Phase == "start" {
			return fmt.Sprintf("routing to %s", evt.Tool)
		}
		return fmt.Sprintf("routed %s", evt.Tool)
	case "worker":
		if evt.Phase == "start" {
			return fmt.Sprintf("%s working", evt.Agent)
		}
		if evt.Error != "" {
			return fmt.Sprintf("%s failed", evt.Agent)
		}
		return fmt.Sprintf("%s done", evt.Agent)
	case "tool":
		if evt.Phase == "start" {
			return fmt.Sprintf("%s calling %s", evt.Agent, evt.Tool)
		}
		if evt.Error != "" {
			return fmt.Sprintf("%s.%s failed", evt.Agent, evt.Tool)
		}
		return fmt.Sprintf("%s.%s done", evt.Agent, evt.Tool)
	case "tick":
		return fmt.Sprintf("still working (%ds)", evt.ElapsedMS/1000)
	case "latency":
		if evt.Tool != "" {
			return fmt.Sprintf("%s took %dms", evt.Tool, evt.DurationMS)
		}
		return fmt.Sprintf("latency %dms", evt.DurationMS)
	case "done":
		return "done"
	case "error":
		return fmt.Sprintf("error: %s", evt.Error)
	default:
		return evt.Type
	}
}
