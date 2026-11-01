package factory

import (
	"strings"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/driver/claude"
	"agent-mesh/internal/driver/codex"
	"agent-mesh/internal/driver/cursor"
	"agent-mesh/internal/driver/opencode"
	"agent-mesh/internal/driver/pi"
	"agent-mesh/internal/driver/workbuddy"
)

// AllNames 本机支持的全部引擎。
func AllNames() []string {
	return []string{"workbuddy", "claude", "cursor", "codex", "opencode", "pi"}
}

// Attach 按名称注册引擎。
func Attach(set *driver.Set, names []string) {
	for _, name := range names {
		switch strings.TrimSpace(strings.ToLower(name)) {
		case "workbuddy":
			set.Register(workbuddy.New())
		case "claude":
			set.Register(claude.New())
		case "cursor":
			set.Register(cursor.New())
		case "codex":
			set.Register(codex.New())
		case "opencode":
			set.Register(opencode.New())
		case "pi":
			set.Register(pi.New())
		}
	}
}

// Default 注册全部引擎。
func Default() *driver.Set {
	set := driver.NewSet()
	Attach(set, AllNames())
	return set
}
