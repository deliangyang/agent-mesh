package workbuddy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseWorkBuddyModelsSkipsEmpty(t *testing.T) {
	for _, raw := range []string{"", "[]", "{}", "null", "   "} {
		if rows := parseWorkBuddyModels("workbuddy", []byte(raw)); len(rows) != 0 {
			t.Fatalf("%q => %#v", raw, rows)
		}
	}
}

func TestParseWorkBuddyModelsArrayAndWrap(t *testing.T) {
	rows := parseWorkBuddyModels("workbuddy", []byte(`[
		{"id":"gpt-5","name":"GPT-5"},
		{"id":"claude","title":"Claude"}
	]`))
	if len(rows) != 2 || rows[0].ID != "workbuddy/gpt-5" || !rows[0].Default || rows[1].Title != "Claude" {
		t.Fatalf("%#v", rows)
	}
	wrap := parseWorkBuddyModels("workbuddy", []byte(`{
		"default":"b",
		"data":[{"id":"a"},{"id":"b","owned_by":"x"}]
	}`))
	if len(wrap) != 2 || !wrap[1].Default || wrap[0].Default {
		t.Fatalf("%#v", wrap)
	}
}

func TestModelsFallsBackWhenFileEmpty(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "models.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &Driver{root: root}
	rows, err := d.Models(t.Context())
	if err == nil || len(rows) != 0 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	if got := err.Error(); got != "no workbuddy models.json, product config, or live gateway" {
		t.Fatalf("err %q", got)
	}
}

func TestModelsFromProductConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `{
		"models":[
			{"id":"fast-model","isDefault":true,"name":"Fast"},
			{"id":"deep-model","name":"Deep"},
			{"id":"hy3","name":"HY3"}
		],
		"agents":[{"models":["hy3","fast-model"]}]
	}`
	if err := os.WriteFile(filepath.Join(root, "cache", "acc-product-config-v3.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "models.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &Driver{root: root}
	rows, err := d.Models(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// Agent model ids filter/order the catalog.
	if len(rows) != 2 || rows[0].Name != "hy3" || rows[1].Name != "fast-model" || !rows[1].Default {
		t.Fatalf("%#v", rows)
	}
}
