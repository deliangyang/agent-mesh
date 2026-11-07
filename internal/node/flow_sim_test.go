package node

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agent-mesh/internal/client"
	"agent-mesh/internal/driver"
	"agent-mesh/internal/e2e"
	"agent-mesh/internal/protocol"
)

// flowDriver is a mutable session list for the sealed bootstrap simulation.
type flowDriver struct {
	stubDriver
	rows []protocol.Session
}

func (d *flowDriver) Name() string { return "flow" }

func (d *flowDriver) List(ctx context.Context, all bool) ([]protocol.Session, error) {
	out := make([]protocol.Session, len(d.rows))
	copy(out, d.rows)
	return out, nil
}

func TestSealedFlow(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	plain := filepath.Join(root, "plain")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(plain, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	drv := &flowDriver{rows: []protocol.Session{
		{Engine: "flow", ID: "s-git", Title: "git", CWD: repo, Live: true, UpdatedAt: 30},
		{Engine: "flow", ID: "s-plain", Title: "plain", CWD: plain, Live: true, UpdatedAt: 20},
		{Engine: "flow", ID: "s-old", Title: "old", CWD: plain, UpdatedAt: 10},
	}}
	ident, err := e2e.Generate()
	if err != nil {
		t.Fatal(err)
	}
	set := driver.NewSet()
	set.Register(drv)
	srv := New(ident, "unused", "test", set, nil)
	srv.refreshSnapshot(context.Background())
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

	engResp, err := cli.Call(ctx, protocol.Request{ID: "engines", Method: protocol.MethodEngines})
	if err != nil {
		t.Fatal(err)
	}
	var engines []protocol.EngineInfo
	if err := json.Unmarshal(engResp.Body, &engines); err != nil {
		t.Fatal(err)
	}
	foundEngine := false
	for _, e := range engines {
		if e.Name == "flow" {
			foundEngine = true
		}
	}
	if !foundEngine {
		t.Fatalf("engines %#v", engines)
	}

	page1 := callPage(t, cli, protocol.ListParams{Engine: "flow", Limit: 2})
	if !page1.More || len(page1.Sessions) != 2 {
		t.Fatalf("page1 %#v", page1)
	}
	if page1.Sessions[0].ID != "s-git" || page1.Sessions[1].ID != "s-plain" {
		t.Fatalf("page1 order %#v", page1.Sessions)
	}
	page2 := callPage(t, cli, protocol.ListParams{
		Engine:   "flow",
		Limit:    2,
		Before:   page1.Sessions[1].UpdatedAt,
		BeforeID: page1.Sessions[1].ID,
	})
	if page2.More || len(page2.Sessions) != 1 || page2.Sessions[0].ID != "s-old" {
		t.Fatalf("page2 %#v", page2)
	}
	seen := map[string]bool{}
	for _, row := range append(append([]protocol.Session{}, page1.Sessions...), page2.Sessions...) {
		if seen[row.ID] {
			t.Fatalf("overlap %s", row.ID)
		}
		seen[row.ID] = true
	}

	bare, err := cli.Call(ctx, protocol.Request{
		ID:     "list-all",
		Method: protocol.MethodList,
		Params: protocol.MustJSON(protocol.ListParams{Engine: "flow"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	var live []protocol.Session
	if err := json.Unmarshal(bare.Body, &live); err != nil {
		t.Fatalf("limit 0 must stay an array: %v body %s", err, bare.Body)
	}
	if len(live) != 2 {
		t.Fatalf("live rows %#v", live)
	}

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	frames, err := cli.StreamJSON(streamCtx, protocol.Request{ID: "cat", Method: protocol.MethodCatalog, Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	if msg, ok := readCat(frames, 250*time.Millisecond); ok {
		t.Fatalf("catalog stream pushed before a change: %#v", msg)
	}

	drv.rows[0].Title = "git renamed"
	srv.refreshSnapshot(context.Background())
	msg, ok := readCat(frames, 2*time.Second)
	if !ok {
		t.Fatal("missing incremental session")
	}
	assertSessionFrame(t, msg, "s-git", true)
	if extra, ok := readCat(frames, 200*time.Millisecond); ok {
		t.Fatalf("extra frame after git session: %#v", extra)
	}

	drv.rows[1].Title = "plain renamed"
	srv.refreshSnapshot(context.Background())
	msg, ok = readCat(frames, 2*time.Second)
	if !ok {
		t.Fatal("missing plain incremental")
	}
	assertSessionFrame(t, msg, "s-plain", false)

	dirResp, err := cli.Call(ctx, protocol.Request{
		ID:     "dirs",
		Method: protocol.MethodDirs,
		Params: protocol.MustJSON(protocol.DirsParams{Path: root}),
	})
	if err != nil {
		t.Fatal(err)
	}
	var listing protocol.DirListing
	if err := json.Unmarshal(dirResp.Body, &listing); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, d := range listing.Dirs {
		names[d.Name] = true
	}
	if !names["repo"] || !names["plain"] {
		t.Fatalf("dirs %#v", listing.Dirs)
	}
	if names["sub"] {
		t.Fatalf("listed a nested dir: %#v", listing.Dirs)
	}
}

func callPage(t *testing.T, cli *client.Client, p protocol.ListParams) protocol.SessionPage {
	t.Helper()
	resp, err := cli.Call(context.Background(), protocol.Request{
		ID:     "list",
		Method: protocol.MethodList,
		Params: protocol.MustJSON(p),
	})
	if err != nil {
		t.Fatal(err)
	}
	var page protocol.SessionPage
	if err := json.Unmarshal(resp.Body, &page); err != nil {
		t.Fatalf("page: %v body %s", err, resp.Body)
	}
	return page
}

type catFrame struct {
	Type     string             `json:"type"`
	Reset    bool               `json:"reset"`
	Sessions []protocol.Session `json:"sessions"`
}

func readCat(ch <-chan json.RawMessage, wait time.Duration) (catFrame, bool) {
	select {
	case raw, ok := <-ch:
		if !ok {
			return catFrame{}, false
		}
		var msg catFrame
		if err := json.Unmarshal(raw, &msg); err != nil {
			return catFrame{}, false
		}
		return msg, true
	case <-time.After(wait):
		return catFrame{}, false
	}
}

func assertSessionFrame(t *testing.T, msg catFrame, id string, git bool) {
	t.Helper()
	if msg.Type != "sessions" || msg.Reset {
		t.Fatalf("frame %#v", msg)
	}
	if len(msg.Sessions) != 1 || msg.Sessions[0].ID != id {
		t.Fatalf("sessions %#v", msg.Sessions)
	}
	if msg.Sessions[0].Git == nil || *msg.Sessions[0].Git != git {
		t.Fatalf("git marker %#v", msg.Sessions[0].Git)
	}
}
