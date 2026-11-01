package opencode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseModelLines(t *testing.T) {
	rows := parseModelLines("opencode", "anthropic/claude-sonnet-4.5\nopenai/gpt-5\n")
	if len(rows) != 2 || rows[0].Name != "anthropic/claude-sonnet-4.5" || !rows[0].Default {
		t.Fatalf("%#v", rows)
	}
}

func TestParseSessionList(t *testing.T) {
	raw := `[{"id":"ses_1","title":"Fix login","directory":"/work","time":{"updated":1700000000000}}]`
	rows := parseSessionList("opencode", raw)
	if len(rows) != 1 || rows[0].ID != "ses_1" || rows[0].CWD != "/work" {
		t.Fatalf("%#v", rows)
	}
	db := `[{"id":"ses_2","directory":"/proj","title":"ui-components folder structure","time_updated":1793520556428}]`
	rows = parseSessionList("opencode", db)
	if len(rows) != 1 || rows[0].ID != "ses_2" || rows[0].Title != "ui-components folder structure" || rows[0].UpdatedAt == 0 {
		t.Fatalf("db %#v", rows)
	}
}

func TestParseExportMessages(t *testing.T) {
	raw := `{"messages":[{"info":{"role":"user"},"parts":[{"type":"text","text":"ls"}]}]}`
	evs := parseExport(raw, "opencode", "ses_1")
	if len(evs) != 1 || evs[0].Type != "user" || evs[0].Text != "ls" {
		t.Fatalf("%#v", evs)
	}
}

func TestFindJSONL(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sessions", "ses_1.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{\"type\":\"user\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &Driver{root: root}
	if got := d.findJSONL("ses_1"); got != path {
		t.Fatalf("got %s want %s", got, path)
	}
	if got := d.findJSONL("missing"); got != "" {
		t.Fatalf("missing %s", got)
	}
}
