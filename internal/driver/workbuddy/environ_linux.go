//go:build linux

package workbuddy

import (
	"fmt"
	"os"
)

func processEnviron(pid int) map[string]string {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		return nil
	}
	return parseEnvironNUL(raw)
}
