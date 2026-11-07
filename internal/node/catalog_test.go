package node

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/e2e"
	"agent-mesh/internal/protocol"

	"github.com/gorilla/websocket"
)

// flakyDriver fails List on demand so the snapshot can be checked against a
// scan that silently lost one engine.
type flakyDriver struct {
	stubDriver
	fail bool
	rows []protocol.Session
}

func (d *flakyDriver) Name() string { return "flaky" }

func (d *flakyDriver) List(ctx context.Context, all bool) ([]protocol.Session, error) {
	if d.fail {
		return nil, errors.New("engine scan failed")
	}
	return d.rows, nil
}

func sessionIDs(rows []protocol.Session) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.ID)
	}
	slices.Sort(out)
	return out
}

func TestSnapshotKeepsSessionsOfFailedEngine(t *testing.T) {
	ident, err := e2e.Generate()
	if err != nil {
		t.Fatal(err)
	}
	d := &flakyDriver{rows: []protocol.Session{{Engine: "flaky", ID: "f1", Title: "keep"}}}
	set := driver.NewSet()
	set.Register(d)
	srv := New(ident, "unused", "test", set, nil)
	ctx := context.Background()

	srv.refreshSnapshot(ctx)
	if got := sessionIDs(srv.peekSnapshot().Sessions); !slices.Equal(got, []string{"f1"}) {
		t.Fatalf("sessions = %v", got)
	}

	d.fail = true
	srv.refreshSnapshot(ctx)
	if got := sessionIDs(srv.peekSnapshot().Sessions); !slices.Equal(got, []string{"f1"}) {
		t.Fatalf("failed engine dropped its sessions: %v", got)
	}

	d.fail = false
	d.rows = nil
	srv.refreshSnapshot(ctx)
	if got := srv.peekSnapshot().Sessions; len(got) != 0 {
		t.Fatalf("removed sessions survived: %v", got)
	}
}

func TestLocalModelID(t *testing.T) {
	ident, err := e2e.Generate()
	if err != nil {
		t.Fatal(err)
	}
	set := driver.NewSet()
	set.Register(stubDriver{})
	srv := New(ident, "unused", "test", set, nil)
	prefixed := ident.ID + "/workbuddy/fast"
	if got := srv.localModelID(prefixed); got != "workbuddy/fast" {
		t.Fatalf("strip = %q", got)
	}
	if got := srv.localModelID("workbuddy/fast"); got != "workbuddy/fast" {
		t.Fatalf("plain = %q", got)
	}
	if got := srv.cliModelFor(ident.ID+"/stub/composer-2.5", "stub"); got != "composer-2.5" {
		t.Fatalf("cliModelFor prefixed = %q", got)
	}
	if got := srv.cliModelFor("stub/composer-2.5", "stub"); got != "composer-2.5" {
		t.Fatalf("cliModelFor engine/model = %q", got)
	}
	if got := srv.cliModelFor("composer-2.5", "stub"); got != "composer-2.5" {
		t.Fatalf("cliModelFor bare = %q", got)
	}
}

func TestNodeServesUIAndCatalogWS(t *testing.T) {
	ident, err := e2e.Generate()
	if err != nil {
		t.Fatal(err)
	}
	set := driver.NewSet()
	set.Register(stubDriver{})
	srv := New(ident, "unused", "test", set, nil)
	srv.refreshSnapshot(context.Background())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status %d", resp.StatusCode)
	}

	resp, err = http.Post(ts.URL+"/login", "application/json", strings.NewReader(`{"password":""}`))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status %d", resp.StatusCode)
	}

	_, _, models, _, sessions, _ := srv.webCatalog()
	if len(sessions) == 0 {
		t.Fatal("expected snapshot sessions")
	}
	prefix := ident.ID + "/"
	gotPrefixedModel := false
	for _, m := range models {
		if strings.HasPrefix(m.ID, prefix) {
			gotPrefixedModel = true
			break
		}
	}
	if len(models) == 0 || !gotPrefixedModel {
		t.Fatalf("models missing node prefix %q: %#v", prefix, models)
	}

	pageResp, err := http.Get(ts.URL + "/v1/openai/sessions?engine=stub&limit=40")
	if err != nil {
		t.Fatal(err)
	}
	var page protocol.SessionPage
	if err := json.NewDecoder(pageResp.Body).Decode(&page); err != nil {
		pageResp.Body.Close()
		t.Fatal(err)
	}
	pageResp.Body.Close()
	gotSession := false
	for _, sess := range page.Sessions {
		if sess.ID == "s1" {
			gotSession = true
		}
	}
	if !gotSession {
		t.Fatalf("session page %#v", page)
	}
	engResp, err := http.Get(ts.URL + "/v1/openai/engines")
	if err != nil {
		t.Fatal(err)
	}
	engResp.Body.Close()
	if engResp.StatusCode != http.StatusOK {
		t.Fatalf("engines status %d", engResp.StatusCode)
	}

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/openai/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]string{"type": "watch", "engine": "stub", "session": "s1"}); err != nil {
		t.Fatalf("watch: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	gotConv := false
	gotGit := false
	for i := 0; i < 8; i++ {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var msg struct {
			Type     string             `json:"type"`
			Reset    bool               `json:"reset"`
			Sessions []protocol.Session `json:"sessions"`
		}
		if json.Unmarshal(raw, &msg) != nil {
			continue
		}
		if msg.Type == "conversation" && msg.Reset {
			gotConv = true
		}
		if msg.Type == "sessions" && !msg.Reset {
			for _, sess := range msg.Sessions {
				if sess.ID == "s1" && sess.Git != nil && !*sess.Git {
					gotGit = true
				}
			}
		}
		if gotConv && gotGit {
			break
		}
	}
	if !gotConv {
		t.Fatal("expected conversation")
	}
	if !gotGit {
		t.Fatal("expected git:false marker on the opened session")
	}
}
