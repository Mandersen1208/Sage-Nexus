// error_bus.go — Redis pub/sub channel that carries structured failure events
// across the Sage system. Producers (CopilotAgent tool loop, SageOrchestrator
// worker dispatch, /dispatch admission and orchestration errors) call
// PublishError(ctx, evt). The SageOrchestratorAgent subscribes once at boot
// and maintains an in-memory ring buffer exposed via /orchestrator/errors.
//
// Emitting is decoupled from the request lifecycle: PublishError never blocks
// the caller on Redis I/O and never fails the request on a publish error.
package sageagents

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/matta/sage-nexus/services/manager/a2a"
)

// ChannelErrors is the Redis pub/sub channel for structured error events.
const ChannelErrors = "sage:errors"

// ErrorEvent is the wire format for an error published onto sage:errors.
// Kind classifies where in the stack the failure surfaced:
//   - "tool"       : MCP tool call (searxng_search, budget_*, skill_*, ...)
//   - "route"      : orchestrator LLM router tool (call_research_agent, ...)
//   - "worker"     : worker agent Chat() returned an error
//   - "orchestrate": top-level orchestration failure (outer catch)
//   - "acp_deny"   : ACP admission denied a request
type ErrorEvent struct {
	RequestID  string `json:"request_id,omitempty"`
	Timestamp  int64  `json:"timestamp"`
	Kind       string `json:"kind"`
	Agent      string `json:"agent,omitempty"`
	Tool       string `json:"tool,omitempty"`
	Error      string `json:"error"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

// ErrorPublisher wraps a Redis client so callers can publish ErrorEvents
// without knowing the channel name or JSON encoding.
type ErrorPublisher struct {
	Client *redis.Client
}

// Publish encodes evt as JSON and publishes it on ChannelErrors. A nil
// publisher or marshal/publish failure is swallowed into a log line — the
// error bus must never itself fail a user request.
func (p *ErrorPublisher) Publish(ctx context.Context, evt ErrorEvent) {
	if p == nil || p.Client == nil {
		return
	}
	if evt.Timestamp == 0 {
		evt.Timestamp = time.Now().Unix()
	}
	b, err := json.Marshal(evt)
	if err != nil {
		log.Printf("[error-bus] marshal failed: %v", err)
		return
	}
	if err := p.Client.Publish(ctx, ChannelErrors, b).Err(); err != nil {
		log.Printf("[error-bus] publish failed: %v", err)
	}

	// Echo into the per-task A2A timeline so the dashboard shows the error
	// inline with the task's other events. The cross-cutting sage:errors
	// channel is still the source of truth for the global error feed.
	if evt.RequestID != "" {
		meta := map[string]interface{}{
			"activity": "error",
			"kind":     evt.Kind,
			"error":    evt.Error,
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
		a2a.PublishEvent(p.Client, a2a.NewWorkingStatus(evt.RequestID, "", evt.Error, meta))
	}
}

type errorPublisherKey struct{}

// WithErrorPublisher returns a ctx that carries the given publisher. Call
// sites then invoke PublishError(ctx, evt) without needing a direct reference.
func WithErrorPublisher(ctx context.Context, pub *ErrorPublisher) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, errorPublisherKey{}, pub)
}

// PublishError publishes evt via the publisher stashed in ctx. No-op when no
// publisher is attached, so producers can call unconditionally.
func PublishError(ctx context.Context, evt ErrorEvent) {
	if ctx == nil {
		return
	}
	pub, ok := ctx.Value(errorPublisherKey{}).(*ErrorPublisher)
	if !ok || pub == nil {
		return
	}
	pub.Publish(ctx, evt)
}
