package node

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

func (stubDriver) Reply(ctx context.Context, sessionID, text, model string) (<-chan protocol.Event, error) {
	return nil, nil
}

func (stubDriver) Send(ctx context.Context, cwd, text, resume, model string) (<-chan protocol.Event, error) {
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

func TestCatalogStream(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cli.Handshake(ctx, ""); err != nil {
		t.Fatal(err)
	}
	ch, err := cli.Stream(ctx, protocol.Request{ID: "c1", Method: protocol.MethodCatalog, Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for ev := range ch {
		seen[ev.Type] = true
		if seen["sessions"] && seen["engines"] && seen["models"] && seen["dirs"] {
			cancel()
			break
		}
	}
	for _, want := range []string{"sessions", "engines", "models", "dirs"} {
		if !seen[want] {
			t.Fatalf("missing catalog part %q; got %#v", want, seen)
		}
	}
}

type recDriver struct {
	name     string
	sessions []string
	mu       sync.Mutex
	replies  []string
	models   []string
}

func (d *recDriver) Name() string { return d.name }

func (d *recDriver) List(ctx context.Context, all bool) ([]protocol.Session, error) {
	out := make([]protocol.Session, 0, len(d.sessions))
	for _, id := range d.sessions {
		out = append(out, protocol.Session{Engine: d.name, ID: id, Title: d.name, Live: true})
	}
	return out, nil
}

func (d *recDriver) History(ctx context.Context, sessionID string, tail int) ([]protocol.Event, error) {
	for _, id := range d.sessions {
		if id == sessionID {
			return []protocol.Event{{Type: "user", Text: "from-" + d.name, Session: sessionID, Engine: d.name}}, nil
		}
	}
	return nil, fmt.Errorf("session %s has no jsonl", sessionID)
}

func (d *recDriver) Watch(ctx context.Context, sessionID string) (<-chan protocol.Event, error) {
	ch := make(chan protocol.Event)
	close(ch)
	return ch, nil
}

func (d *recDriver) Reply(ctx context.Context, sessionID, text, model string) (<-chan protocol.Event, error) {
	d.mu.Lock()
	d.replies = append(d.replies, sessionID)
	d.models = append(d.models, model)
	d.mu.Unlock()
	for _, id := range d.sessions {
		if id == sessionID {
			ch := make(chan protocol.Event, 1)
			ch <- protocol.Event{Type: "assistant", Text: "ok-" + d.name, Session: sessionID, Engine: d.name, Done: true}
			close(ch)
			return ch, nil
		}
	}
	return nil, fmt.Errorf("session %s not unique or missing", sessionID)
}

func (d *recDriver) Send(ctx context.Context, cwd, text, resume, model string) (<-chan protocol.Event, error) {
	d.mu.Lock()
	d.models = append(d.models, model)
	d.mu.Unlock()
	ch := make(chan protocol.Event, 1)
	ch <- protocol.Event{Type: "assistant", Text: "send-" + d.name, Session: resume, Engine: d.name, Done: true}
	close(ch)
	return ch, nil
}

func (d *recDriver) Probe(ctx context.Context) protocol.EngineInfo {
	return protocol.EngineInfo{Name: d.name, Title: d.name, Available: true, Enabled: true}
}

func (d *recDriver) Models(ctx context.Context) ([]protocol.Model, error) {
	return []protocol.Model{{ID: d.name + "/demo", Engine: d.name, Name: "demo", Default: true}}, nil
}

func TestHandleChatUsesSessionEngine(t *testing.T) {
	ident, err := e2e.Generate()
	if err != nil {
		t.Fatal(err)
	}
	claude := &recDriver{name: "claude"}
	cursor := &recDriver{name: "cursor", sessions: []string{"54b217af-e7ad-40f6-b27c-9a0d06097078"}}
	set := driver.NewSet()
	set.Register(claude)
	set.Register(cursor)
	srv := New(ident, "unused", "test", set, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `{"model":"claude/group/never-stuck[1m]","stream":false,"messages":[{"role":"user","content":"ls"}],"session":"54b217af-e7ad-40f6-b27c-9a0d06097078","cwd":"/tmp"}`
	resp, err := ts.Client().Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d %s", resp.StatusCode, raw)
	}
	if strings.Contains(string(raw), "not unique or missing") {
		t.Fatalf("still missing: %s", raw)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	mesh, _ := out["agent_mesh"].(map[string]any)
	if mesh["engine"] != "cursor" {
		t.Fatalf("engine %#v body %s", mesh, raw)
	}
	cursor.mu.Lock()
	n := len(cursor.replies)
	cursor.mu.Unlock()
	if n != 1 {
		t.Fatalf("cursor replies %d claude %#v", n, claude.replies)
	}
}

func TestHandleChatPassesModel(t *testing.T) {
	ident, err := e2e.Generate()
	if err != nil {
		t.Fatal(err)
	}
	cur := &recDriver{name: "cursor"}
	set := driver.NewSet()
	set.Register(cur)
	srv := New(ident, "unused", "test", set, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `{"model":"cursor/composer-2.5","stream":false,"messages":[{"role":"user","content":"hi"}],"cwd":"/tmp"}`
	resp, err := ts.Client().Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d %s", resp.StatusCode, raw)
	}
	cur.mu.Lock()
	got := append([]string{}, cur.models...)
	cur.mu.Unlock()
	if len(got) != 1 || got[0] != "composer-2.5" {
		t.Fatalf("models %#v", got)
	}
}

func TestHandleHistoryUsesSessionEngine(t *testing.T) {
	ident, err := e2e.Generate()
	if err != nil {
		t.Fatal(err)
	}
	sid := "01752a28-273d-4f4d-af51-e88787148c70"
	set := driver.NewSet()
	set.Register(&recDriver{name: "pi"})
	set.Register(&recDriver{name: "cursor", sessions: []string{sid}})
	srv := New(ident, "unused", "test", set, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/v1/openai/history?engine=pi&session=" + sid + "&tail=20&turns=20")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d %s", resp.StatusCode, raw)
	}
	var evs []protocol.Event
	if err := json.Unmarshal(raw, &evs); err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Engine != "cursor" {
		t.Fatalf("events %#v", evs)
	}

	resp2, err := ts.Client().Get(ts.URL + "/v1/openai/history?engine=pi&session=missing-session")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	raw2, _ := io.ReadAll(resp2.Body)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("missing status %d %s", resp2.StatusCode, raw2)
	}
	if strings.TrimSpace(string(raw2)) != "[]\n" && strings.TrimSpace(string(raw2)) != "[]" {
		var empty []protocol.Event
		if json.Unmarshal(raw2, &empty) != nil || empty == nil {
			t.Fatalf("missing body %s", raw2)
		}
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

type hangWatchDriver struct {
	stubDriver
	cancelled atomic.Bool
}

func (d *hangWatchDriver) Name() string { return "cursor" }

func (d *hangWatchDriver) List(ctx context.Context, all bool) ([]protocol.Session, error) {
	return []protocol.Session{{Engine: "cursor", ID: "s1", Title: "demo", Live: true}}, nil
}

func (d *hangWatchDriver) Reply(ctx context.Context, sessionID, text, model string) (<-chan protocol.Event, error) {
	ch := make(chan protocol.Event, 2)
	go func() {
		ch <- protocol.Event{Type: "assistant", Text: "ok", Session: sessionID, Engine: "cursor", Done: true}
		<-ctx.Done()
		d.cancelled.Store(true)
		close(ch)
	}()
	return ch, nil
}

func TestHandleChatEndsWatchTurn(t *testing.T) {
	ident, err := e2e.Generate()
	if err != nil {
		t.Fatal(err)
	}
	d := &hangWatchDriver{}
	set := driver.NewSet()
	set.Register(d)
	srv := New(ident, "unused", "test", set, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `{"model":"cursor","stream":true,"messages":[{"role":"user","content":"hi"}],"session":"s1"}`
	resp, err := ts.Client().Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	done := make(chan struct{})
	go func() {
		_, _ = io.ReadAll(resp.Body)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE did not finish after Done")
	}
}

type choiceDriver struct {
	stubDriver
	alive atomic.Bool
}

func (d *choiceDriver) Name() string { return "cursor" }

func (d *choiceDriver) Reply(ctx context.Context, sessionID, text, model string) (<-chan protocol.Event, error) {
	ch := make(chan protocol.Event, 1)
	go func() {
		time.Sleep(200 * time.Millisecond)
		d.alive.Store(ctx.Err() == nil)
		ch <- protocol.Event{Type: "assistant", Text: "ok", Session: sessionID, Engine: "cursor", Done: true}
		close(ch)
	}()
	return ch, nil
}

func TestHandleChoiceKeepsEngine(t *testing.T) {
	ident, err := e2e.Generate()
	if err != nil {
		t.Fatal(err)
	}
	d := &choiceDriver{}
	set := driver.NewSet()
	set.Register(d)
	srv := New(ident, "unused", "test", set, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `{"engine":"cursor","session":"s1","text":"Allow"}`
	resp, err := ts.Client().Post(ts.URL+"/v1/openai/choice", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d %s", resp.StatusCode, raw)
	}
	time.Sleep(350 * time.Millisecond)
	if !d.alive.Load() {
		t.Fatal("engine ctx was not canceled after Ask")
	}
}

func TestRPCCommands(t *testing.T) {
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
	resp, err := cli.Call(ctx, protocol.Request{
		ID:     "1",
		Method: protocol.MethodCommands,
		Params: protocol.MustJSON(protocol.CommandsParams{Engine: "stub"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("rpc %#v", resp)
	}
}
