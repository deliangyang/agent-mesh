package node

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/e2e"
	"agent-mesh/internal/protocol"

	"github.com/gorilla/websocket"
)

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

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("get ui: %v", err)
	}
	page, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read ui: %v", err)
	}
	if !strings.Contains(string(page), "Agent Mesh") {
		t.Fatalf("ui %q", page)
	}

	resp, err = http.Post(ts.URL+"/login", "application/json", strings.NewReader(`{"password":""}`))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status %d", resp.StatusCode)
	}

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/openai/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	seen := map[string]bool{}
	gotSession := false
	for {
		if seen["nodes"] && seen["engines"] && seen["sessions"] && gotSession {
			break
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("catalog read: %v parts %v", err, seen)
		}
		var msg struct {
			Type     string              `json:"type"`
			Nodes    []protocol.NodeInfo `json:"nodes"`
			Sessions []protocol.Session  `json:"sessions"`
		}
		if json.Unmarshal(raw, &msg) != nil {
			continue
		}
		seen[msg.Type] = true
		if msg.Type == "nodes" && len(msg.Nodes) != 1 {
			t.Fatalf("nodes %d", len(msg.Nodes))
		}
		if msg.Type == "sessions" {
			for _, sess := range msg.Sessions {
				if sess.ID == "s1" {
					gotSession = true
				}
			}
		}
	}
	if !seen["nodes"] || !seen["engines"] || !seen["sessions"] {
		t.Fatalf("parts %v", seen)
	}
	if !gotSession {
		t.Fatal("missing session s1")
	}

	if err := conn.WriteJSON(map[string]string{"type": "watch", "engine": "stub", "session": "s1"}); err != nil {
		t.Fatalf("watch: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	gotConv := false
	for i := 0; i < 8; i++ {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var msg struct {
			Type  string `json:"type"`
			Reset bool   `json:"reset"`
		}
		if json.Unmarshal(raw, &msg) != nil {
			continue
		}
		if msg.Type == "conversation" && msg.Reset {
			gotConv = true
			break
		}
	}
	if !gotConv {
		t.Fatal("expected conversation")
	}
}
