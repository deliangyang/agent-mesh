package cursor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestListIncludesTranscripts(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CURSOR_CONFIG_DIR", root)
	t.Setenv("CURSOR_DATA_DIR", root)
	t.Setenv("CURSOR_AGENT_BIN", filepath.Join(root, "missing-agent"))
	id := "54b217af-e7ad-40f6-b27c-9a0d06097078"
	dir := filepath.Join(root, "projects", "home-tmp-proj", "agent-transcripts", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	jsonl := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(jsonl, []byte("{\"type\":\"user\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := New()
	rows, err := d.List(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range rows {
		if s.ID == id {
			found = true
			if s.JSONL != jsonl {
				t.Fatalf("jsonl %s", s.JSONL)
			}
		}
	}
	if !found {
		t.Fatalf("session not listed: %#v", rows)
	}
	sess, err := d.lookup(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID != id {
		t.Fatalf("lookup %#v", sess)
	}
}

func TestListFillsTitleFromJSONL(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CURSOR_CONFIG_DIR", root)
	t.Setenv("CURSOR_DATA_DIR", root)
	t.Setenv("CURSOR_AGENT_BIN", filepath.Join(root, "missing-agent"))
	id := "64b217af-e7ad-40f6-b27c-9a0d06097078"
	dir := filepath.Join(root, "projects", "home-tmp-proj", "agent-transcripts", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	jsonl := filepath.Join(dir, id+".jsonl")
	raw := `{"type":"user","message":{"content":"修复登录超时"}}` + "\n"
	if err := os.WriteFile(jsonl, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	d := New()
	rows, err := d.List(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range rows {
		if s.ID == id {
			if s.Title != "修复登录超时" {
				t.Fatalf("title %#v", s)
			}
			return
		}
	}
	t.Fatalf("session not listed: %#v", rows)
}
