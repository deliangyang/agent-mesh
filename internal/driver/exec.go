package driver

import (
	"context"
	"os/exec"
)

// CommandContext starts an engine CLI. On Windows the console window is hidden.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	hideWindow(cmd)
	return cmd
}
