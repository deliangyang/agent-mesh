package node

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/e2e"
)

func TestMeshAPISaveAndProbe(t *testing.T) {
	t.Setenv("AGENT_MESH_HOME", t.TempDir())
	ident, err := e2e.Generate()
	if err != nil {
		t.Fatal(err)
	}
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		case "/v1/register":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(relay.Close)

	set := driver.NewSet()
	srv := New(ident, "127.0.0.1:0", "test", set, nil)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/v1/openai/mesh")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var st MeshStatus
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if !st.Local {
		t.Fatal("expected local mesh settings")
	}

	body := `{"relay":"` + relay.URL + `","password":"secret","advertise":"http://127.0.0.1:9"}`
	resp, err = http.Post(ts.URL+"/v1/openai/mesh/test", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("test status %d", resp.StatusCode)
	}

	req, err := http.NewRequest(http.MethodPut, ts.URL+"/v1/openai/mesh", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save status %d", resp.StatusCode)
	}
	st = srv.MeshStatus()
	if !st.Linked || len(st.Relays) != 1 || st.Relays[0] != strings.TrimRight(relay.URL, "/") {
		t.Fatalf("mesh status %+v", st)
	}
}

func TestNormalizeRelays(t *testing.T) {
	got := normalizeRelays(" https://a.example/ ", "https://a.example", "https://b.example,", "")
	if len(got) != 2 || got[0] != "https://a.example" || got[1] != "https://b.example" {
		t.Fatalf("%v", got)
	}
}
