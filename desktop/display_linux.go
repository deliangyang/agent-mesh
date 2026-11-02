//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func ensureGUIEnv() error {
	prepareWebKitEnv()
	if os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		if os.Getenv("GDK_BACKEND") == "" && os.Getenv("DISPLAY") != "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			_ = os.Setenv("GDK_BACKEND", "x11")
		}
		return nil
	}
	if adoptFromSession() {
		return nil
	}
	if adoptX11Fallback() {
		return nil
	}
	return fmt.Errorf("DISPLAY / WAYLAND_DISPLAY was not found. Start from a graphical desktop, or pass -headless to run the node only")
}

func prepareWebKitEnv() {
	// DMA-BUF opens /dev/dri before the WebView is created; Wails GpuPolicyNever cannot stop it.
	setenvDefault("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
	setenvDefault("WEBKIT_DISABLE_COMPOSITING_MODE", "1")
	setenvDefault("LIBGL_DRI3_DISABLE", "1")
	setenvDefault("GDK_GL", "disable")
	if driAccessible() {
		return
	}
	// the current user is not in video/render, but Mesa still probes DRM nodes. Use software rendering and quiet the probe logs.
	setenvDefault("LIBGL_ALWAYS_SOFTWARE", "1")
	setenvDefault("MESA_LOADER_DRIVER_OVERRIDE", "llvmpipe")
	setenvDefault("GALLIUM_DRIVER", "llvmpipe")
	setenvDefault("EGL_LOG_LEVEL", "fatal")
}

func driAccessible() bool {
	ents, err := os.ReadDir("/dev/dri")
	if err != nil {
		return false
	}
	for _, e := range ents {
		name := e.Name()
		if !strings.HasPrefix(name, "card") && !strings.HasPrefix(name, "renderD") {
			continue
		}
		f, err := os.OpenFile(filepath.Join("/dev/dri", name), os.O_RDWR, 0)
		if err != nil {
			continue
		}
		_ = f.Close()
		return true
	}
	return false
}

func setenvDefault(k, v string) {
	if os.Getenv(k) == "" {
		_ = os.Setenv(k, v)
	}
}

func adoptFromSession() bool {
	uid := strconv.Itoa(os.Getuid())
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	var best map[string]string
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		pid := e.Name()
		if pid[0] < '0' || pid[0] > '9' {
			continue
		}
		st, err := os.Stat(filepath.Join("/proc", pid))
		if err != nil || st.Sys() == nil {
			continue
		}
		env := readProcEnv(pid)
		if env["DISPLAY"] == "" && env["WAYLAND_DISPLAY"] == "" {
			continue
		}
		// only keep the current user's session environment
		status := readProcStatus(pid)
		uids := strings.Fields(status["Uid"])
		if len(uids) > 0 && uids[0] != uid {
			continue
		}
		cmd := readProcComm(pid)
		score := 1
		switch cmd {
		case "gnome-session-b", "gnome-session-binary", "plasmashell", "xfce4-session", "cinnamon-sessio", "Hyprland", "sway":
			score = 3
		}
		if env["XDG_SESSION_TYPE"] == "x11" || env["XDG_SESSION_TYPE"] == "wayland" {
			score++
		}
		if best == nil || score > atoi(best["_score"]) {
			env["_score"] = strconv.Itoa(score)
			best = env
		}
	}
	if best == nil {
		return false
	}
	applyGUIEnv(best)
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

func adoptX11Fallback() bool {
	if _, err := os.Stat("/tmp/.X11-unix/X0"); err != nil {
		return false
	}
	_ = os.Setenv("DISPLAY", ":0")
	if os.Getenv("XAUTHORITY") == "" {
		if home, _ := os.UserHomeDir(); home != "" {
			p := filepath.Join(home, ".Xauthority")
			if _, err := os.Stat(p); err == nil {
				_ = os.Setenv("XAUTHORITY", p)
			}
		}
	}
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		_ = os.Setenv("XDG_RUNTIME_DIR", "/run/user/"+strconv.Itoa(os.Getuid()))
	}
	_ = os.Setenv("GDK_BACKEND", "x11")
	_ = os.Setenv("XDG_SESSION_TYPE", "x11")
	return true
}

func applyGUIEnv(env map[string]string) {
	for _, k := range []string{"DISPLAY", "WAYLAND_DISPLAY", "XAUTHORITY", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS", "XDG_SESSION_TYPE"} {
		if v := env[k]; v != "" && os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
	if os.Getenv("GDK_BACKEND") == "" && os.Getenv("DISPLAY") != "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		_ = os.Setenv("GDK_BACKEND", "x11")
	}
}

func readProcEnv(pid string) map[string]string {
	raw, err := os.ReadFile(filepath.Join("/proc", pid, "environ"))
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, part := range strings.Split(string(raw), "\x00") {
		k, v, ok := strings.Cut(part, "=")
		if ok {
			out[k] = v
		}
	}
	return out
}

func readProcComm(pid string) string {
	b, _ := os.ReadFile(filepath.Join("/proc", pid, "comm"))
	return strings.TrimSpace(string(b))
}

func readProcStatus(pid string) map[string]string {
	raw, err := os.ReadFile(filepath.Join("/proc", pid, "status"))
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if ok {
			out[k] = strings.TrimSpace(v)
		}
	}
	return out
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
