package openai

import (
	"testing"

	"agent-mesh/internal/protocol"
)

func TestFromEventDelta(t *testing.T) {
	ch := FromEvent("id1", "cursor/auto", "n1", "box", protocol.Event{
		Type: "delta", Engine: "cursor", Session: "s1", Text: "hello",
	})
	if ch.Object != "chat.completion.chunk" {
		t.Fatalf("object %s", ch.Object)
	}
	if ch.Choices[0].Delta.Content != "hello" {
		t.Fatalf("content %#v", ch.Choices[0].Delta)
	}
	if ch.Mesh.Session != "s1" {
		t.Fatalf("mesh %#v", ch.Mesh)
	}
}

func TestFromEventTool(t *testing.T) {
	ch := FromEvent("id1", "claude", "n", "box", protocol.Event{
		Type: "tool_call", Role: "assistant", Tool: "Bash", Text: "Bash ls", Engine: "claude",
	})
	if ch.Mesh.Type != "tool_call" || ch.Mesh.Tool != "Bash" {
		t.Fatalf("mesh %#v", ch.Mesh)
	}
}

func TestFromEventDone(t *testing.T) {
	ch := FromEvent("id1", "claude", "", "", protocol.Event{Type: "log", Text: "end", Done: true})
	if ch.Choices[0].FinishReason == nil || *ch.Choices[0].FinishReason != "stop" {
		t.Fatalf("want stop, got %#v", ch.Choices[0].FinishReason)
	}
}

func TestSplitModel(t *testing.T) {
	known := []string{"cursor", "claude", "workbuddy", "codex", "qoder-cn"}
	e, n := SplitModel("claude/auto/qwen3.7-plus[1m]", known)
	if e != "claude" || n != "auto/qwen3.7-plus[1m]" {
		t.Fatalf("got %s %s", e, n)
	}
	e, n = SplitModel("cursor", known)
	if e != "cursor" || n != "" {
		t.Fatalf("got %s %s", e, n)
	}
	e, n = SplitModel("qoder-cn/auto", known)
	if e != "qoder-cn" || n != "auto" {
		t.Fatalf("got %s %s", e, n)
	}
}

func TestEngineModel(t *testing.T) {
	known := []string{"cursor", "claude", "workbuddy", "codex", "qoder-cn"}
	cases := []struct {
		model, field, engine, name string
	}{
		{"cursor/composer-2.5", "", "cursor", "composer-2.5"},
		{"cursor/composer-2.5", "cursor", "cursor", "composer-2.5"},
		{"composer-2.5", "cursor", "cursor", "composer-2.5"},
		{"claude/auto/qwen", "cursor", "cursor", ""},
		{"cursor", "", "cursor", ""},
		{"qoder-cn/auto", "qoder-cn", "qoder-cn", "auto"},
	}
	for _, tc := range cases {
		e, n := EngineModel(tc.model, tc.field, known)
		if e != tc.engine || n != tc.name {
			t.Fatalf("EngineModel(%q, %q)=%s %s want %s %s", tc.model, tc.field, e, n, tc.engine, tc.name)
		}
	}
}

func TestLastUserText(t *testing.T) {
	got := LastUserText([]ChatMessage{
		{Role: "system", Content: "s"},
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "ok"},
	})
	if got != "hi" {
		t.Fatalf("got %q", got)
	}
}
