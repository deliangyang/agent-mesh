//go:build !windows

package main

import (
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

func withGUILog(log *slog.Logger) *slog.Logger { return log }

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
