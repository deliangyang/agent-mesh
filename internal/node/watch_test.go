package node

import (
	"testing"

	"agent-mesh/internal/openai"
)

func chunk(engine, session string) openai.Chunk {
	return openai.Chunk{Mesh: openai.Mesh{Engine: engine, Session: session}}
}

func TestWatchAllows(t *testing.T) {
	want := watchTarget{node: "n1", engine: "claude", session: "s1"}
	cases := []struct {
		name   string
		target watchTarget
		chunk  openai.Chunk
		ok     bool
	}{
		{"watched session", want, chunk("claude", "s1"), true},
		{"other session on same engine", want, chunk("claude", "s2"), false},
		{"other engine", want, chunk("codex", "s1"), false},
		{"empty watch", watchTarget{}, chunk("claude", "s1"), false},
		{"engine only", watchTarget{engine: "claude"}, chunk("claude", "s2"), true},
		{"engine only, other engine", watchTarget{engine: "claude"}, chunk("codex", "s2"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := watchAllows(tc.chunk, tc.target); got != tc.ok {
				t.Fatalf("watchAllows = %v, want %v", got, tc.ok)
			}
		})
	}
}
