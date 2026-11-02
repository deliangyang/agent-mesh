//go:build !windows

package workbuddy

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func sniffWorkBuddyInstallDirs() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = filepath.Clean(strings.TrimSpace(p))
		if p == "" || seen[p] {
			return
		}
		if !looksLikeWorkBuddyInstall(p) {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, k := range []string{"WORKBUDDY_HOME", "WORKBUDDY_INSTALL_DIR", "WORKBUDDY_PATH"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			add(v)
		}
	}
	for _, ent := range filepath.SplitList(os.Getenv("PATH")) {
		ent = filepath.Clean(strings.TrimSpace(ent))
		if ent == "" || !strings.Contains(strings.ToLower(ent), "workbuddy") {
			continue
		}
		if strings.EqualFold(filepath.Base(ent), "bin") {
			add(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(ent)))))
		}
		add(ent)
	}
	switch runtime.GOOS {
	case "darwin":
		add("/Applications/WorkBuddy.app/Contents")
		add(filepath.Join(os.Getenv("HOME"), "Applications/WorkBuddy.app/Contents"))
	default:
		add("/opt/WorkBuddy")
		add("/usr/share/workbuddy")
	}
	return out
}

func looksLikeWorkBuddyInstall(dir string) bool {
	if st, err := os.Stat(filepath.Join(dir, "WorkBuddy.exe")); err == nil && !st.IsDir() {
		return true
	}
	if st, err := os.Stat(filepath.Join(dir, "MacOS", "WorkBuddy")); err == nil && !st.IsDir() {
		return true
	}
	cli := filepath.Join(dir, "resources", "app.asar.unpacked", "cli", "bin", "codebuddy")
	if st, err := os.Stat(cli); err == nil && !st.IsDir() {
		return true
	}
	return false
}
