package pi

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParsePiModels(t *testing.T) {
	out := `provider                    model                                          context  max-out  thinking  images
anthropic                   claude-fable-5                                 1M       128K     yes       yes
anthropic                   claude-haiku-4-5                               200K     64K      yes       yes
openai                      gpt-4o                                         128K     16K      no        yes
`
	rows := parsePiModels(out)
	if len(rows) != 3 {
		t.Fatalf("len %#v", rows)
	}
	if rows[0].ID != "pi/anthropic/claude-fable-5" || !rows[0].Default {
		t.Fatalf("first %#v", rows[0])
	}
	if rows[2].Name != "openai/gpt-4o" {
		t.Fatalf("openai %#v", rows[2])
	}
}

func TestFindBinPrefersEnvAndNpmGlobal(t *testing.T) {
	t.Setenv("PI_BIN", "/tmp/custom-pi")
	if got := findBin(); got != "/tmp/custom-pi" {
		t.Fatalf("env %q", got)
	}
	t.Setenv("PI_BIN", "")
	t.Setenv("PATH", "/nonexistent")
	root := t.TempDir()
	t.Setenv("HOME", root)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", root)
	}
	npm := filepath.Join(root, ".npm-global", "bin")
	if err := os.MkdirAll(npm, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(npm, "pi")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := findBin(); got != p {
		t.Fatalf("npm %q", got)
	}
}

func TestListReadsSessionHeader(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", root)
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "sessions"))
	t.Setenv("PI_BIN", filepath.Join(root, "missing-pi"))
	dir := filepath.Join(root, "sessions", "tmp-work")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "abc123de-0000-4000-8000-000000000001"
	p := filepath.Join(dir, "2024-01-01T00-00-00-000Z_"+id+".jsonl")
	body := `{"type":"session","version":3,"id":"` + id + `","cwd":"/tmp/work","name":"demo"}` + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	d := New()
	rows, err := d.List(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != id || rows[0].CWD != "/tmp/work" {
		t.Fatalf("%#v", rows)
	}
	got, err := d.History(context.Background(), id, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].Session != id {
		t.Fatalf("history %#v", got)
	}
}
