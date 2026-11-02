package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"agent-mesh/internal/e2e"
	"agent-mesh/internal/protocol"
)

func TestInfoDirectIncludesPlatform(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/info" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(protocol.NodeInfo{
			ID: "n1", Name: "box", OS: "windows", Arch: "amd64", Engines: []string{"claude"},
		})
	}))
	t.Cleanup(ts.Close)

	ident, err := e2e.Generate()
	if err != nil {
		t.Fatal(err)
	}
	cli := New(ident, "", ts.URL)
	info, err := cli.Info(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if info.OS != "windows" || info.Arch != "amd64" || info.ID != "n1" {
		t.Fatalf("%#v", info)
	}
}
