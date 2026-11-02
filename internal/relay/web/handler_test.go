package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gorilla/websocket"
)

func testAssets() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("UI_INDEX")},
		"assets/app.js": {Data: []byte("JS_OK")},
		"favicon.svg":   {Data: []byte("<svg/>")},
	}
}

func TestHandlerServesVueUI(t *testing.T) {
	h, err := newHandler("", "", testAssets())
	if err != nil {
		t.Fatalf("newHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	cases := []struct {
		name string
		path string
		want string
	}{
		{name: "root", path: "/", want: "UI_INDEX"},
		{name: "login", path: "/login", want: "UI_INDEX"},
		{name: "spa fallback", path: "/missing", want: "UI_INDEX"},
		{name: "asset", path: "/assets/app.js", want: "JS_OK"},
		{name: "favicon", path: "/favicon.svg", want: "<svg/>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + tc.path)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if !strings.Contains(string(body), tc.want) {
				t.Fatalf("body %q, want substring %q", body, tc.want)
			}
		})
	}
}

func TestHandlerProxiesAPIWithBearer(t *testing.T) {
	var gotAuth, gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(upstream.Close)

	h, err := newHandler(upstream.URL, "secret", testAssets())
	if err != nil {
		t.Fatalf("newHandler: %v", err)
	}
	front := httptest.NewServer(h)
	t.Cleanup(front.Close)

	resp, err := http.Get(front.URL + "/v1/models")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if gotPath != "/v1/models" {
		t.Fatalf("upstream path %q", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("authorization %q", gotAuth)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("body %q", body)
	}

	gotAuth, gotPath = "", ""
	resp, err = http.Post(front.URL+"/login", "application/json", strings.NewReader(`{"password":"x"}`))
	if err != nil {
		t.Fatalf("post login: %v", err)
	}
	defer resp.Body.Close()
	if gotPath != "/login" {
		t.Fatalf("login path %q", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("login authorization %q", gotAuth)
	}
}

func TestHandlerAPIWithoutRelay(t *testing.T) {
	h, err := newHandler("", "", testAssets())
	if err != nil {
		t.Fatalf("newHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	resp, err := http.Get(ts.URL + "/v1/models")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestHandlerWebSocketProxy(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"nodes"}`))
		_, _, _ = conn.ReadMessage()
	}))
	t.Cleanup(upstream.Close)

	h, err := newHandler(upstream.URL, "secret", testAssets())
	if err != nil {
		t.Fatalf("newHandler: %v", err)
	}
	front := httptest.NewServer(h)
	t.Cleanup(front.Close)

	wsURL := "ws" + strings.TrimPrefix(front.URL, "http") + "/v1/openai/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(msg) != `{"type":"nodes"}` {
		t.Fatalf("message %q", msg)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("authorization %q", gotAuth)
	}
}

func TestNewHandlerRejectsBadRelay(t *testing.T) {
	_, err := newHandler("ftp://example.com", "", testAssets())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAttachRoutesAPIAndUI(t *testing.T) {
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/ping" {
			_, _ = w.Write([]byte("pong"))
			return
		}
		http.NotFound(w, r)
	})
	h, err := Attach(api)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/v1/ping")
	if err != nil {
		t.Fatalf("get api: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read api: %v", err)
	}
	if string(body) != "pong" {
		t.Fatalf("api body %q", body)
	}

	resp, err = http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("get ui: %v", err)
	}
	defer resp.Body.Close()
	page, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read ui: %v", err)
	}
	if !strings.Contains(string(page), "Agent Mesh") {
		t.Fatalf("ui %q", page)
	}
}
