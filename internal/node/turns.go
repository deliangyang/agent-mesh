package node

import (
	"context"
	"sync"
)

// turnHandle is the cancel token for one in-flight chat turn.
type turnHandle struct {
	cancel context.CancelFunc
}

// turnCancels tracks cancel funcs for in-flight chat turns so Stop can kill the CLI.
type turnCancels struct {
	mu sync.Mutex
	m  map[string]*turnHandle
}

func newTurnCancels() *turnCancels {
	return &turnCancels{m: map[string]*turnHandle{}}
}

func turnKey(engine, session, cwd string) string {
	if session != "" {
		return engine + "\x00" + session
	}
	if cwd != "" {
		return engine + "\x00cwd:" + cwd
	}
	return engine + "\x00"
}

func (t *turnCancels) register(key string, h *turnHandle) {
	if key == "" || h == nil {
		return
	}
	t.mu.Lock()
	prev := t.m[key]
	t.m[key] = h
	t.mu.Unlock()
	if prev != nil && prev != h {
		prev.cancel()
	}
}

func (t *turnCancels) clear(key string, h *turnHandle) {
	if key == "" || h == nil {
		return
	}
	t.mu.Lock()
	if t.m[key] == h {
		delete(t.m, key)
	}
	t.mu.Unlock()
}

func (t *turnCancels) stop(key string) bool {
	if key == "" {
		return false
	}
	t.mu.Lock()
	h := t.m[key]
	delete(t.m, key)
	t.mu.Unlock()
	if h == nil {
		return false
	}
	h.cancel()
	return true
}
