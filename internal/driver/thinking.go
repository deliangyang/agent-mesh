package driver

import (
	"context"
	"slices"
	"strings"
)

type thinkingKey struct{}

// ThinkingLevels maps an engine to the effort levels it accepts for new turns.
// Engines absent here ignore thinking, and their models advertise none.
var ThinkingLevels = map[string][]string{
	"claude": {"low", "medium", "high", "xhigh", "max"},
	"codex":  {"low", "medium", "high"},
}

// WithThinking scopes a thinking level to one turn.
func WithThinking(ctx context.Context, level string) context.Context {
	if strings.TrimSpace(level) == "" {
		return ctx
	}
	return context.WithValue(ctx, thinkingKey{}, strings.TrimSpace(level))
}

// ThinkingOf returns the turn's thinking level, if any.
func ThinkingOf(ctx context.Context) string {
	level, _ := ctx.Value(thinkingKey{}).(string)
	return level
}

// ThinkingArgs returns engine CLI flags for a thinking level.
// An empty or unsupported level yields nil.
func ThinkingArgs(engine, level string) []string {
	level = strings.ToLower(strings.TrimSpace(level))
	if level == "" || !slices.Contains(ThinkingLevels[engine], level) {
		return nil
	}
	switch engine {
	case "claude":
		return []string{"--effort", level}
	case "codex":
		return []string{"-c", "model_reasoning_effort=" + level}
	}
	return nil
}

// ModelThinking lists the thinking levels models of engine advertise.
func ModelThinking(engine string) []string {
	return ThinkingLevels[engine]
}
