package main

import (
	"context"
	"sync"

	"github.com/matta/sage-nexus/services/manager/a2a"
)

// continuationRegistry holds per-task channels used to deliver ControlMessage
// values to a paused orchestration goroutine. Sage's delegate_continue MCP
// tool publishes to sage:control; the manager's subscriber routes by taskId.
type continuationRegistry struct {
	mu       sync.Mutex
	channels map[string]chan a2a.ControlMessage
}

type activeTaskRegistry struct {
	mu      sync.Mutex
	cancels map[string]*activeTaskCancel
}

type activeTaskCancel struct {
	cancel context.CancelFunc
}

var continuations = &continuationRegistry{channels: make(map[string]chan a2a.ControlMessage)}
var activeTasks = &activeTaskRegistry{cancels: make(map[string]*activeTaskCancel)}

func (r *activeTaskRegistry) register(parent context.Context, taskID string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	entry := &activeTaskCancel{cancel: cancel}
	r.mu.Lock()
	r.cancels[taskID] = entry
	r.mu.Unlock()

	cleanup := func() {
		r.mu.Lock()
		if current, ok := r.cancels[taskID]; ok && current == entry {
			delete(r.cancels, taskID)
		}
		r.mu.Unlock()
	}
	return ctx, cleanup
}

func (r *activeTaskRegistry) cancel(taskID string) bool {
	r.mu.Lock()
	entry, ok := r.cancels[taskID]
	r.mu.Unlock()
	if ok {
		entry.cancel()
	}
	return ok
}

func (r *continuationRegistry) register(taskID string) chan a2a.ControlMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan a2a.ControlMessage, 1)
	r.channels[taskID] = ch
	return ch
}

func (r *continuationRegistry) unregister(taskID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.channels, taskID)
}

func (r *continuationRegistry) deliver(taskID string, msg a2a.ControlMessage) bool {
	r.mu.Lock()
	ch, ok := r.channels[taskID]
	r.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- msg:
		return true
	default:
		return false
	}
}

func (r *continuationRegistry) exists(taskID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.channels[taskID]
	return ok
}
