//go:build !linux && !windows && !darwin

package workbuddy

func processEnviron(pid int) map[string]string {
	return nil
}
