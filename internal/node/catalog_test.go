package node

import (
	"context"
	"encoding/json"
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

func TestLocalModelID(t *testing.T) {
	ident, err := e2e.Generate()
	if err != nil {
		t.Fatal(err)
	}
	srv := New(ident, "unused", "test", driver.NewSet(), nil)
	prefixed := ident.ID + "/workbuddy/fast"
	if got := srv.localModelID(prefixed); got != "workbuddy/fast" {
		t.Fatalf("strip = %q", got)
	}
	if got := srv.localModelID("workbuddy/fast"); got != "workbuddy/fast" {
		t.Fatalf("plain = %q", got)
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

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/openai/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	seen := map[string]bool{}
	gotSession := false
	gotPrefixedModel := false
	for {
		if seen["nodes"] && seen["engines"] && seen["sessions"] && seen["models"] && gotSession && gotPrefixedModel {
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
			Models   []struct {
				ID string `json:"id"`
			} `json:"models"`
		}
		if json.Unmarshal(raw, &msg) != nil {
			continue
		}
		seen[msg.Type] = true
		if msg.Type == "nodes" {
			if len(msg.Nodes) != 1 {
				t.Fatalf("nodes %d", len(msg.Nodes))
			}
			if msg.Nodes[0].OS == "" || msg.Nodes[0].Arch == "" {
				t.Fatalf("node platform %#v", msg.Nodes[0])
			}
		}
		if msg.Type == "models" {
			prefix := ident.ID + "/"
			for _, m := range msg.Models {
				if strings.HasPrefix(m.ID, prefix) {
					gotPrefixedModel = true
					break
				}
			}
			if len(msg.Models) == 0 {
				t.Fatal("expected catalog models")
			}
			if !gotPrefixedModel {
				t.Fatalf("models missing node prefix %q: %#v", prefix, msg.Models)
			}
		}
		if msg.Type == "sessions" {
			for _, sess := range msg.Sessions {
				if sess.ID == "s1" {
					gotSession = true
				}
			}
		}
	}
	if !seen["nodes"] || !seen["engines"] || !seen["sessions"] || !seen["models"] {
		t.Fatalf("parts %v", seen)
	}
	if !gotSession {
		t.Fatal("missing session s1")
	}
	if !gotPrefixedModel {
		t.Fatal("catalog models must be nodeID/engine/… like the relay")
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
