package driver

import (
	"context"
	"reflect"
	"testing"
)

func TestThinkingArgs(t *testing.T) {
	cases := []struct {
		engine, level string
		want          []string
	}{
		{"claude", "high", []string{"--effort", "high"}},
		{"claude", "", nil},
		{"claude", "bogus", nil},
		{"codex", "low", []string{"-c", "model_reasoning_effort=low"}},
		{"codex", "max", nil},
		{"cursor", "high", nil},
	}
	for _, tc := range cases {
		t.Run(tc.engine+"/"+tc.level, func(t *testing.T) {
			if got := ThinkingArgs(tc.engine, tc.level); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ThinkingArgs(%q, %q) = %#v, want %#v", tc.engine, tc.level, got, tc.want)
			}
		})
	}
}

func TestThinkingRoundTrip(t *testing.T) {
	ctx := WithThinking(context.Background(), "medium")
	if got := ThinkingOf(ctx); got != "medium" {
		t.Fatalf("ThinkingOf = %q", got)
	}
	if got := ThinkingOf(context.Background()); got != "" {
		t.Fatalf("empty context = %q", got)
	}
}
