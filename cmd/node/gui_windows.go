//go:build windows

package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"agent-mesh/internal/driver"

	"golang.org/x/sys/windows"
)

func withGUILog(log *slog.Logger) *slog.Logger {
	path := filepath.Join(driver.MeshHome(), "node.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return log
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return log
	}
	return slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, f), nil))
}

func runWindowsTray(ctx context.Context, cancel context.CancelFunc, uiURL string) bool {
	hideOwnConsole()
	go func() {
		waitHealth(uiURL)
		openUI(uiURL)
	}()
	if err := runTray(ctx, cancel, uiURL); err != nil {
		return false
	}
	return true
}

func hideOwnConsole() {
	hwnd, _, _ := getConsoleWindow.Call()
	if hwnd == 0 {
		return
	}
	var pids [8]uint32
	n, _, _ := getConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	if n == 1 {
		showWindow.Call(hwnd, 0) // SW_HIDE
	}
}

func openUI(uiURL string) {
	url := strings.TrimRight(uiURL, "/") + "/"
	if bin := findBrowser(); bin != "" {
		cmd := exec.Command(bin, "--app="+url, "--new-window", "--window-size=1360,860")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		if err := cmd.Start(); err == nil {
			return
		}
	}
	if p, err := windows.UTF16PtrFromString(url); err == nil {
		_ = windows.ShellExecute(0, nil, p, nil, nil, windows.SW_SHOWNORMAL)
	}
}

func findBrowser() string {
	var cands []string
	for _, root := range []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
		os.Getenv("LOCALAPPDATA"),
	} {
		if root == "" {
			continue
		}
		cands = append(cands,
			filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe"),
		)
	}
	for _, p := range cands {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}
