package factory

import (
	"strings"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/driver/claude"
	"agent-mesh/internal/driver/codex"
	"agent-mesh/internal/driver/cursor"
	"agent-mesh/internal/driver/opencode"
	"agent-mesh/internal/driver/pi"
	"agent-mesh/internal/driver/qodercn"
	"agent-mesh/internal/driver/workbuddy"
)

// AllNames all engines this machine supports.
func AllNames() []string {
	return []string{"workbuddy", "claude", "cursor", "codex", "opencode", "pi", "qoder-cn"}
}

// Attach registers engines by name.
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
		case "qoder-cn", "qodercn":
			set.Register(qodercn.New())
		}
	}
}

// Default registers every engine.
func Default() *driver.Set {
	set := driver.NewSet()
	Attach(set, AllNames())
	return set
}
