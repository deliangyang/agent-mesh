//go:build unix

package driver

import "syscall"

// PIDAlive reports whether a process is still running.
func PIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil
}
