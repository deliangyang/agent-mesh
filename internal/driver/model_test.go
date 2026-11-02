package driver

import (
	"reflect"
	"testing"
)

func TestCLIModel(t *testing.T) {
	cases := []struct {
		id, engine, want string
	}{
		{"cursor/composer-2.5", "cursor", "composer-2.5"},
		{"composer-2.5", "cursor", "composer-2.5"},
		{"cursor", "cursor", ""},
		{"", "cursor", ""},
		{"claude/auto/qwen3.7-plus[1m]", "claude", "auto/qwen3.7-plus[1m]"},
		{"qoder-cn/auto", "qoder-cn", "auto"},
		{"pi/openai/gpt-5", "pi", "openai/gpt-5"},
	}
	for _, tc := range cases {
		got := CLIModel(tc.id, tc.engine)
		if got != tc.want {
			t.Fatalf("CLIModel(%q, %q)=%q want %q", tc.id, tc.engine, got, tc.want)
		}
	}
}

func TestWithModelFlag(t *testing.T) {
	base := []string{"-p", "--trust"}
	got := WithModelFlag(base, "composer-2.5")
	want := []string{"-p", "--trust", "--model", "composer-2.5"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
	if got := WithModelFlag(base, "  "); !reflect.DeepEqual(got, base) {
		t.Fatalf("empty model %#v", got)
	}
}
