package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionIDFromName(t *testing.T) {
	id := "01a17b9e-55a2-7953-af98-b9a36cd6bf93"
	name := "rollout-2026-10-27T12-28-56-" + id + ".jsonl"
	if got := sessionIDFromName(name); got != id {
		t.Fatalf("got %q", got)
	}
	if !matchRollout(name[:len(name)-6], id) {
		t.Fatal("uuid should match rollout stem")
	}
	if matchRollout("rollout-other", "missing") {
		t.Fatal("missing should not match")
	}
}

func TestFindJSONLNestedRollout(t *testing.T) {
	root := t.TempDir()
	id := "01a17b9e-55a2-7953-af98-b9a36cd6bf93"
	dir := filepath.Join(root, "sessions", "2026", "10", "27")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-10-27T12-28-56-"+id+".jsonl")
	body := `{"type":"session_meta","payload":{"id":"` + id + `","cwd":"/tmp/work"}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello codex"}]}}
{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}}
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &Driver{root: root, bin: filepath.Join(root, "missing-codex")}
	if got := d.findJSONL(id); got != path {
		t.Fatalf("find uuid got %q want %q", got, path)
	}
	if got := d.findJSONL("rollout-2026-10-27T12-28-56-" + id); got != path {
		t.Fatalf("find stem got %q", got)
	}

	rows, err := d.List(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != id || rows[0].CWD != "/tmp/work" || rows[0].JSONL != path {
		t.Fatalf("list %#v", rows)
	}
	if rows[0].Title != "hello codex" {
		t.Fatalf("title %q", rows[0].Title)
	}

	evs, err := d.History(context.Background(), id, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) < 2 {
		t.Fatalf("history %#v", evs)
	}
	if evs[0].Type != "session" || evs[0].Session != id {
		t.Fatalf("meta %#v", evs[0])
	}
	if evs[1].Type != "user" || evs[1].Text != "hello codex" {
		t.Fatalf("user %#v", evs[1])
	}
	if evs[2].Type != "assistant" || evs[2].Text != "hi" {
		t.Fatalf("assistant %#v", evs[2])
	}
}
