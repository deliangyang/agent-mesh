package node

import (
	"context"
	"encoding/json"
	"testing"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/e2e"
	"agent-mesh/internal/protocol"
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

func TestDispatchStopCancelsTrackedTurn(t *testing.T) {
	ident, err := e2e.Generate()
	if err != nil {
		t.Fatal(err)
	}
	set := driver.NewSet()
	set.Register(stubDriver{})
	srv := New(ident, "unused", "test", set, nil)
	ctx, cancel := context.WithCancel(context.Background())
	h := &turnHandle{cancel: cancel}
	key := turnKey("stub", "s1", "/tmp/w")
	srv.turns.register(key, h)

	resp := srv.dispatchStop(context.Background(), protocol.Request{
		ID:     "1",
		Method: protocol.MethodStop,
		Params: protocol.MustJSON(protocol.StopParams{Engine: "stub", SessionID: "s1", CWD: "/tmp/w"}),
	})
	if !resp.OK {
		t.Fatalf("stop: %s", resp.Error)
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body["stopped"] != true {
		t.Fatalf("body = %#v", body)
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("turn cancel not called")
	}
}
