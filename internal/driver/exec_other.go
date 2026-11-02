//go:build !windows

package driver

import "os/exec"

func hideWindow(*exec.Cmd) {}
