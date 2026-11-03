package node

import (
	"context"
	"testing"
)

func TestTurnCancelsStop(t *testing.T) {
	tc := newTurnCancels()
	ctx, cancel := context.WithCancel(context.Background())
	h := &turnHandle{cancel: cancel}
	key := turnKey("claude", "s1", "/tmp")
	tc.register(key, h)
	if !tc.stop(key) {
		t.Fatal("want stop")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("cancel not called")
	}
	if tc.stop(key) {
		t.Fatal("second stop should miss")
	}
}

func TestTurnCancelsClearKeepsForeign(t *testing.T) {
	tc := newTurnCancels()
	_, cancel := context.WithCancel(context.Background())
	h1 := &turnHandle{cancel: cancel}
	h2 := &turnHandle{cancel: func() {}}
	key := turnKey("cursor", "abc", "")
	tc.register(key, h1)
	tc.clear(key, h2)
	if !tc.stop(key) {
		t.Fatal("h1 should still be registered")
	}
}
