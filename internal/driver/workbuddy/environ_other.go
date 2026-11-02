//go:build !linux && !windows

package workbuddy

func processEnviron(pid int) map[string]string {
	return nil
}
