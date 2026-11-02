package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func localUIURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://127.0.0.1"
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func waitUI(uiURL string) {
	cli := &http.Client{Timeout: 300 * time.Millisecond}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := cli.Get(strings.TrimRight(uiURL, "/") + "/healthz")
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(80 * time.Millisecond)
	}
}

func findChrome() string {
	if runtime.GOOS == "windows" {
		for _, root := range []string{
			os.Getenv("ProgramFiles"),
			os.Getenv("ProgramFiles(x86)"),
			os.Getenv("LOCALAPPDATA"),
		} {
			if root == "" {
				continue
			}
			for _, rel := range []string{
				filepath.Join("Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join("Microsoft", "Edge", "Application", "msedge.exe"),
			} {
				p := filepath.Join(root, rel)
				if st, err := os.Stat(p); err == nil && !st.IsDir() {
					return p
				}
			}
		}
		return ""
	}
	for _, name := range []string{
		"google-chrome",
		"google-chrome-stable",
		"chromium-browser",
		"chromium",
		"microsoft-edge",
		"microsoft-edge-stable",
	} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

func startChromeApp(uiURL string) error {
	bin := findChrome()
	if bin == "" {
		return fmt.Errorf("desktop ui: chrome not found")
	}
	url := strings.TrimRight(uiURL, "/") + "/"
	args := []string{
		"--app=" + url,
		"--new-window",
		"--window-size=1360,860",
	}
	if runtime.GOOS != "windows" {
		args = append(args, "--class=AgentMesh")
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("desktop ui: start chrome: %w", err)
	}
	return nil
}
