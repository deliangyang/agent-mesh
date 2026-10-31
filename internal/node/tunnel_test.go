package node

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/e2e"
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
