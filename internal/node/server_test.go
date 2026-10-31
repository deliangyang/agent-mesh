package node

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"agent-mesh/internal/client"
	"agent-mesh/internal/driver"
	"agent-mesh/internal/e2e"
	"agent-mesh/internal/protocol"
)

type stubDriver struct{}

func (stubDriver) Name() string { return "stub" }

func (stubDriver) List(ctx context.Context, all bool) ([]protocol.Session, error) {
	return []protocol.Session{{Engine: "stub", ID: "s1", Title: "demo", Live: true}}, nil
}

func (stubDriver) History(ctx context.Context, sessionID string, tail int) ([]protocol.Event, error) {
	return []protocol.Event{{Type: "log", Text: "hi", Session: sessionID}}, nil
}

func (stubDriver) Watch(ctx context.Context, sessionID string) (<-chan protocol.Event, error) {
	ch := make(chan protocol.Event, 1)
	ch <- protocol.Event{Type: "log", Text: "tick", Session: sessionID, Done: true}
	close(ch)
	return ch, nil
}

func (stubDriver) Reply(ctx context.Context, sessionID, text string) (<-chan protocol.Event, error) {
	return nil, nil
}

func (stubDriver) Send(ctx context.Context, cwd, text, resume string) (<-chan protocol.Event, error) {
	return nil, nil
}

func (stubDriver) Probe(ctx context.Context) protocol.EngineInfo {
	return protocol.EngineInfo{Name: "stub", Title: "Stub", Available: true}
}

func (stubDriver) Models(ctx context.Context) ([]protocol.Model, error) {
	return []protocol.Model{{ID: "stub/demo", Engine: "stub", Name: "demo", Default: true}}, nil
}

func TestDirectHandshakeList(t *testing.T) {
	ident, err := e2e.Generate()
	if err != nil {
		t.Fatal(err)
	}
	set := driver.NewSet()
	set.Register(stubDriver{})
	srv := New(ident, "unused", "test", set, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	cliIdent, err := e2e.Generate()
	if err != nil {
		t.Fatal(err)
	}
	cli := client.New(cliIdent, "", ts.URL)
	ctx := context.Background()
	if err := cli.Handshake(ctx, ""); err != nil {
		t.Fatal(err)
	}
	resp, err := cli.Call(ctx, protocol.Request{ID: "1", Method: protocol.MethodList})
	if err != nil {
		t.Fatal(err)
	}
	var rows []protocol.Session
	if err := json.Unmarshal(resp.Body, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "s1" {
		t.Fatalf("unexpected %#v", rows)
	}
}

func TestOpenAIModels(t *testing.T) {
	ident, err := e2e.Generate()
	if err != nil {
		t.Fatal(err)
	}
	set := driver.NewSet()
	set.Register(stubDriver{})
	srv := New(ident, "unused", "test", set, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := ts.Client().Get(ts.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}
