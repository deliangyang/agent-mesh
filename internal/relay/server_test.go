package relay

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"agent-mesh/internal/e2e"
	"agent-mesh/internal/protocol"
)

func TestRegisterAndList(t *testing.T) {
	ident, err := e2e.Generate()
	if err != nil {
		t.Fatal(err)
	}
	pub := ident.Public()
	srv := New("unused", NewMemory(), nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	unix := time.Now().Unix()
	info := protocol.NodeInfo{
		ID:      pub.ID,
		Listen:  "http://127.0.0.1:7700",
		Engines: []string{"claude"},
		SignPub: pub.SignPub,
		X25519:  pub.X25519Pub,
	}
	msg := []byte(info.ID + "|" + info.Listen + "|" + strconv.FormatInt(unix, 10))
	req := protocol.RegisterRequest{Info: info, Unix: unix, Signature: ident.SignBytes(msg)}
	raw, _ := json.Marshal(req)
	resp, err := http.Post(ts.URL+"/v1/register", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("register %d", resp.StatusCode)
	}
	resp, err = http.Get(ts.URL + "/v1/nodes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var rows []protocol.NodeInfo
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != pub.ID {
		t.Fatalf("nodes %#v", rows)
	}
}

func TestModelsEmptyListNotNull(t *testing.T) {
	srv := New("unused", NewMemory(), nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if raw["object"] != "list" {
		t.Fatalf("object %#v", raw["object"])
	}
	if raw["data"] == nil {
		t.Fatal("data must not be null")
	}
}

func TestSnapshotModelsWhenNodeUnreachable(t *testing.T) {
	ident, err := e2e.Generate()
	if err != nil {
		t.Fatal(err)
	}
	pub := ident.Public()
	srv := New("unused", NewMemory(), nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	unix := time.Now().Unix()
	info := protocol.NodeInfo{
		ID:      pub.ID,
		Listen:  "http://192.0.2.1:9",
		Engines: []string{"stub"},
		SignPub: pub.SignPub,
		X25519:  pub.X25519Pub,
	}
	msg := []byte(info.ID + "|" + info.Listen + "|" + strconv.FormatInt(unix, 10))
	req := protocol.RegisterRequest{
		Info:      info,
		Unix:      unix,
		Signature: ident.SignBytes(msg),
		Snapshot: protocol.NodeSnapshot{
			Models: []protocol.Model{{ID: "stub/demo", Engine: "stub", Name: "demo", Default: true}},
		},
	}
	raw, _ := json.Marshal(req)
	resp, err := http.Post(ts.URL+"/v1/register", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("register %d", resp.StatusCode)
	}
	resp, err = http.Get(ts.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || list.Data[0].ID != pub.ID+"/stub/demo" {
		t.Fatalf("models %#v", list.Data)
	}
}

func TestDiffEventsAppendAndGrow(t *testing.T) {
	old := []protocol.Event{{Type: "user", Text: "hi"}, {Type: "assistant", Text: "ok"}}
	neu := []protocol.Event{{Type: "user", Text: "hi"}, {Type: "assistant", Text: "ok"}, {Type: "assistant", Text: " more"}}
	added, reset := diffEvents(old, neu)
	if reset || len(added) != 1 || added[0].Text != " more" {
		t.Fatalf("append %#v reset %v", added, reset)
	}
	grown := []protocol.Event{{Type: "user", Text: "hi"}, {Type: "assistant", Text: "okay"}}
	added, reset = diffEvents(old, grown)
	if reset || len(added) != 1 || added[0].Text != "ay" {
		t.Fatalf("grow %#v reset %v", added, reset)
	}
}

func TestLastTurnsKeepsLatestUserRounds(t *testing.T) {
	evs := []protocol.Event{
		{Type: "user", Text: "u1"}, {Type: "tool_call", Tool: "Bash", Text: "ls"}, {Type: "assistant", Text: "a1"},
		{Type: "user", Text: "u2"}, {Type: "assistant", Text: "a2"},
		{Type: "user", Text: "u3"}, {Type: "assistant", Text: "a3"},
	}
	got := lastTurns(evs, 2)
	if len(got) != 4 || got[0].Text != "u2" || got[len(got)-1].Text != "a3" {
		t.Fatalf("%#v", got)
	}
	if got := lastTurns(evs, 20); len(got) != len(evs) {
		t.Fatalf("keep all %#v", got)
	}
}

func TestRankSessionsLatestFirst(t *testing.T) {
	rows := []protocol.Session{
		{ID: "old", Title: "a", UpdatedAt: 10},
		{ID: "new", Title: "b", UpdatedAt: 30, Live: true},
		{ID: "mid", Title: "c", UpdatedAt: 20},
	}
	got := rankSessions(rows, 1)
	if len(got) != 2 || got[0].ID != "new" || got[1].ID != "mid" {
		t.Fatalf("%#v", got)
	}
}

func TestRankSessionsUpdatedAtBeatsLive(t *testing.T) {
	rows := []protocol.Session{
		{ID: "live-old", Title: "old live", UpdatedAt: 10, Live: true},
		{ID: "fresh", Title: "fresh", UpdatedAt: 40},
		{ID: "mid", Title: "mid", UpdatedAt: 20, Busy: true},
	}
	got := rankSessions(rows, 10)
	if len(got) != 3 || got[0].ID != "fresh" || got[1].ID != "mid" || got[2].ID != "live-old" {
		t.Fatalf("%#v", got)
	}
}

func TestClientAPIRequiresPassword(t *testing.T) {
	srv := New("unused", NewMemory(), nil)
	srv.Password = "secret"
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/v1/nodes")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 got %d", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/nodes", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("bearer %d", resp.StatusCode)
	}
	resp, err = http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("healthz %d", resp.StatusCode)
	}
}

func TestLoginPageServesWhenAuthorized(t *testing.T) {
	// Authorized clients must not bounce to "/". dist/ is gitignored, so clean CI
	// may 404 on index.html; that is still fine — a 302 would reintroduce the vault loop.
	srv := New("unused", NewMemory(), nil)
	srv.Password = "secret"
	srv.Web = true
	srv.initAuth()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/login", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: cookieName, Value: srv.sessionToken()})
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
		t.Fatalf("GET /login with session redirected (%d); vault unlock needs the login SPA", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /login with session want 200 or 404 got %d", resp.StatusCode)
	}
}

func TestSPAPublicPaths(t *testing.T) {
	s := New("unused", NewMemory(), nil)
	if !s.publicPath("/assets/index-abc.js") {
		t.Fatal("assets should be public")
	}
	if !s.publicPath("/login") || !s.publicPath("/favicon.svg") {
		t.Fatal("login static assets should be public")
	}
	if !s.publicPath("/manifest.webmanifest") || !s.publicPath("/sw.js") {
		t.Fatal("PWA shell files should be public")
	}
	if s.publicPath("/style.css") || s.publicPath("/app.js") || s.publicPath("/") {
		t.Fatal("chat page must not be anonymous")
	}
}

func TestWebPWAStaticFiles(t *testing.T) {
	// dist/ is gitignored; clean CI trees only embed .gitkeep. Auth must still
	// treat PWA shell paths as public (404 from empty embed is fine; 302 /login is not).
	srv := New("unused", NewMemory(), nil)
	srv.Password = "secret"
	srv.Web = true
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	for _, path := range []string{"/manifest.webmanifest", "/sw.js", "/favicon.svg"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusFound {
			t.Fatalf("%s redirected to login; should be public", path)
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s want 200 or 404 got %d", path, resp.StatusCode)
		}
	}
}
