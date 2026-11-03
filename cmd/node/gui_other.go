//go:build !windows && !linux

package main

import "context"

// runWindowsTray on non-Linux Unix opens the local UI when a graphical session is present.
// Linux uses StatusNotifierItem in tray_linux.go.
func runWindowsTray(ctx context.Context, cancel context.CancelFunc, uiURL string) bool {
	if !hasDisplay() {
		return false
	}
	go func() {
		waitHealth(uiURL)
		openUI(uiURL)
	}()
	<-ctx.Done()
	_ = cancel
	return true
}
