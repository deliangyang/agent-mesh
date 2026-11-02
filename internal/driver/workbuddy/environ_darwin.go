//go:build darwin

package workbuddy

import (
	"golang.org/x/sys/unix"
)

func processEnviron(pid int) map[string]string {
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil || len(raw) < 4 {
		return nil
	}
	return parseProcArgs2Env(raw)
}
