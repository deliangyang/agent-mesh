//go:build !windows

package main

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

func withGUILog(log *slog.Logger) *slog.Logger { return log }

// runWindowsTray on Unix opens the local UI when a graphical session is present.
// A full StatusNotifierItem tray icon is not wired yet (no extra C deps); this
// keeps desktop installs from staying headless after launch.
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

func hasDisplay() bool {
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

func openUI(uiURL string) {
	url := strings.TrimRight(uiURL, "/") + "/"
	for _, bin := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge", "brave-browser"} {
		if p, err := exec.LookPath(bin); err == nil {
			cmd := exec.Command(p, "--app="+url, "--new-window", "--window-size=1360,860")
			if err := cmd.Start(); err == nil {
				return
			}
		}
	}
	if p, err := exec.LookPath("xdg-open"); err == nil {
		_ = exec.Command(p, url).Start()
		return
	}
	if p, err := exec.LookPath("open"); err == nil { // macOS
		_ = exec.Command(p, "-n", "-a", "Google Chrome", "--args", "--app="+url).Start()
	}
}
