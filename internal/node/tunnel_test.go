package node

import (
	"bufio"
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
	"agent-mesh/internal/openai"
	"agent-mesh/internal/protocol"
	"agent-mesh/internal/relay"
)

func TestRelayModelsViaTunnel(t *testing.T) {
	ident, err := e2e.Generate()
	if err != nil {
		t.Fatal(err)
	}
	set := driver.NewSet()
	set.Register(stubDriver{})
	nodeSrv := New(ident, "unused", "box", set, nil)

	relaySrv := relay.New("unused", relay.NewMemory(), nil)
	relayTS := httptest.NewServer(relaySrv.Handler())
	defer relayTS.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- nodeSrv.serveTunnel(ctx, relayTS.URL, "http://192.0.2.1:1")
	}()

	deadline := time.Now().Add(3 * time.Second)
	var rows []struct {
		ID string `json:"id"`
	}
	for time.Now().Before(deadline) {
		resp, err := http.Get(relayTS.URL + "/v1/nodes")
		if err != nil {
			t.Fatal(err)
		}
		_ = json.NewDecoder(resp.Body).Decode(&rows)
		resp.Body.Close()
		if len(rows) == 1 {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	if len(rows) != 1 {
		t.Fatalf("node not registered via tunnel: %#v", rows)
	}

	resp, err := http.Get(relayTS.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Data) == 0 {
		t.Fatal("models empty, tunnel 未把节点模型带上来")
	}
	want := ident.ID + "/stub/demo"
	if list.Data[0].ID != want {
		t.Fatalf("got %s want %s", list.Data[0].ID, want)
	}
}

func TestIngestViaTunnel(t *testing.T) {
	ident, err := e2e.Generate()
	if err != nil {
		t.Fatal(err)
	}
	set := driver.NewSet()
	set.Register(stubDriver{})
	nodeSrv := New(ident, "unused", "box", set, nil)

	relaySrv := relay.New("unused", relay.NewMemory(), nil)
	relayTS := httptest.NewServer(relaySrv.Handler())
	defer relayTS.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = nodeSrv.serveTunnel(ctx, relayTS.URL, "http://192.0.2.1:1") }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(relayTS.URL + "/v1/nodes")
		if err != nil {
			t.Fatal(err)
		}
		var rows []struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&rows)
		resp.Body.Close()
		if len(rows) == 1 {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}

	got := make(chan string, 1)
	go func() {
		resp, err := http.Get(relayTS.URL + "/v1/openai/events")
		if err != nil {
			return
		}
		defer resp.Body.Close()
		r := bufio.NewReader(resp.Body)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					return
				}
				return
			}
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "data:") {
				got <- strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				return
			}
		}
	}()
	time.Sleep(80 * time.Millisecond)
	nodeSrv.Hub.Broadcast(openai.FromEvent("id1", "stub", ident.ID, "box", protocol.Event{
		Type: "assistant", Text: "via-tunnel", Session: "s1", Engine: "stub",
	}))
	select {
	case raw := <-got:
		if !strings.Contains(raw, "s1") {
			t.Fatalf("%s", raw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("隧道 ingest 未到达中继")
	}
}
