package qodercn

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFindBinPrefersEnv(t *testing.T) {
	t.Setenv("QODERCN_BIN", "/tmp/custom-qodercn")
	if got := findBin(); got != "/tmp/custom-qodercn" {
		t.Fatalf("bin %q", got)
	}
	t.Setenv("QODERCN_BIN", "")
	t.Setenv("QODERCN_PATH", "/opt/qoderclicn")
	if got := findBin(); got != "/opt/qoderclicn" {
		t.Fatalf("path %q", got)
	}
}

func TestListIncludesTranscripts(t *testing.T) {
	root := t.TempDir()
	t.Setenv("QODERCN_CONFIG_DIR", root)
	t.Setenv("QODERCN_BIN", filepath.Join(root, "missing-qodercn"))
	id := "54b217af-e7ad-40f6-b27c-9a0d06097078"
	dir := filepath.Join(root, "projects", "-tmp-proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	jsonl := filepath.Join(dir, id+".jsonl")
	body := `{"type":"system","subtype":"init","cwd":"/tmp/proj","session_id":"` + id + `"}` + "\n" +
		`{"type":"user","message":{"content":"fix login timeout"}}` + "\n"
	if err := os.WriteFile(jsonl, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	d := New()
	if d.Name() != "qoder-cn" {
		t.Fatalf("name %q", d.Name())
	}
	rows, err := d.List(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != id {
		t.Fatalf("list %#v", rows)
	}
	live, err := d.List(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 || live[0].ID != id || !live[0].Busy {
		t.Fatalf("busy %#v", live)
	}
	if rows[0].JSONL != jsonl {
		t.Fatalf("jsonl %s", rows[0].JSONL)
	}
	if rows[0].CWD != "/tmp/proj" {
		t.Fatalf("cwd %#v", rows[0])
	}
	if rows[0].Title != "fix login timeout" {
		t.Fatalf("title %#v", rows[0])
	}
	if got := d.findJSONL(id, "/tmp/proj"); got != jsonl {
		t.Fatalf("find %s", got)
	}
	sess, err := d.lookup(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID != id || sess.CWD != "/tmp/proj" {
		t.Fatalf("lookup %#v", sess)
	}
	evs, err := d.History(context.Background(), id, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) < 2 {
		t.Fatalf("history %#v", evs)
	}
}

func TestSessionMetaReadsState(t *testing.T) {
	root := t.TempDir()
	id := "64b217af-e7ad-40f6-b27c-9a0d06097078"
	dir := filepath.Join(root, "projects", "-tmp-work")
	if err := os.MkdirAll(filepath.Join(dir, id), 0o755); err != nil {
		t.Fatal(err)
	}
	jsonl := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(jsonl, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, id, "state.json")
	if err := os.WriteFile(state, []byte(`{"cwd":"/tmp/work","name":"login rewrite"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, title := sessionMeta(jsonl)
	if cwd != "/tmp/work" || title != "login rewrite" {
		t.Fatalf("meta %q %q", cwd, title)
	}
}

func TestModelsUsesSettings(t *testing.T) {
	root := t.TempDir()
	t.Setenv("QODERCN_CONFIG_DIR", root)
	t.Setenv("QODERCN_BIN", filepath.Join(root, "missing-qodercn"))
	if err := os.WriteFile(filepath.Join(root, "settings.json"), []byte(`{"model":"Qwen3.8-Max"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	d := New()
	rows, err := d.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "Qwen3.8-Max" || !rows[0].Default {
		t.Fatalf("models %#v", rows)
	}
}

func TestParseQoderModels(t *testing.T) {
	rows := parseQoderModels("qoder-cn", "Available models:\nauto (current)\nQwen3.8-Max - Qwen latest base\n", "auto")
	if len(rows) != 2 {
		t.Fatalf("len %#v", rows)
	}
	if rows[0].ID != "qoder-cn/auto" || !rows[0].Default {
		t.Fatalf("auto %#v", rows[0])
	}
	if rows[1].Name != "Qwen3.8-Max" || rows[1].Title != "Qwen latest base" {
		t.Fatalf("qwen %#v", rows[1])
	}
	jsonRows := parseQoderModels("qoder-cn", `["auto","GLM-5.2"]`, "")
	if len(jsonRows) != 2 || jsonRows[0].Name != "auto" || !jsonRows[0].Default || jsonRows[1].Name != "GLM-5.2" {
		t.Fatalf("json %#v", jsonRows)
	}
}

func TestSkipPrintDiagnostic(t *testing.T) {
	cases := []struct {
		line string
		skip bool
	}{
		{`[qoder-cn:unrecognized_model] {"model":"auto"}`, true},
		{`  [qodercn:debug] hello`, true},
		{`{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`, false},
		{"", true},
	}
	for _, c := range cases {
		if got := skipPrintDiagnostic(c.line); got != c.skip {
			t.Fatalf("skipPrintDiagnostic(%q)=%v want %v", c.line, got, c.skip)
		}
	}
}

func TestStreamDelta(t *testing.T) {
	obj := map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"delta": map[string]any{"type": "text_delta", "text": "hello"},
		},
	}
	ev := streamDelta(obj, "qoder-cn", "s1")
	if ev == nil || ev.Type != "delta" || ev.Text != "hello" || ev.Session != "s1" {
		t.Fatalf("%#v", ev)
	}
	think := map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"delta": map[string]any{"type": "thinking_delta", "thinking": "look at the directory first"},
		},
	}
	ev = streamDelta(think, "qoder-cn", "s1")
	if ev == nil || ev.Type != "thinking" || ev.Text != "look at the directory first" {
		t.Fatalf("think %#v", ev)
	}
}
