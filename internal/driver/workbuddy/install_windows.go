//go:build windows

package workbuddy

import (
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func sniffWorkBuddyInstallDirs() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		p = filepath.Clean(p)
		if !isWorkBuddyInstallDir(p) {
			// DisplayIcon may point at WorkBuddy.exe directly.
			if strings.EqualFold(filepath.Base(p), "WorkBuddy.exe") {
				p = filepath.Dir(p)
			}
			if !isWorkBuddyInstallDir(p) {
				return
			}
		}
		key := strings.ToLower(p)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, p)
	}

	for _, k := range []string{"WORKBUDDY_HOME", "WORKBUDDY_INSTALL_DIR", "WORKBUDDY_PATH"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			add(v)
		}
	}
	for _, p := range sniffWorkBuddyFromPATH() {
		add(p)
	}
	for _, p := range sniffWorkBuddyFromProcess() {
		add(p)
	}
	for _, p := range sniffWorkBuddyFromRegistry() {
		add(p)
	}
	for _, p := range []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "WorkBuddy"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "WorkBuddy"),
		filepath.Join(os.Getenv("ProgramFiles"), "WorkBuddy"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "WorkBuddy"),
	} {
		add(p)
	}
	return out
}

func isWorkBuddyInstallDir(dir string) bool {
	if dir == "" {
		return false
	}
	if st, err := os.Stat(filepath.Join(dir, "WorkBuddy.exe")); err == nil && !st.IsDir() {
		return true
	}
	cli := filepath.Join(dir, "resources", "app.asar.unpacked", "cli", "bin", "codebuddy")
	if st, err := os.Stat(cli); err == nil && !st.IsDir() {
		return true
	}
	return false
}

func sniffWorkBuddyFromPATH() []string {
	var out []string
	for _, ent := range filepath.SplitList(os.Getenv("PATH")) {
		ent = filepath.Clean(strings.TrimSpace(ent))
		if ent == "" || !strings.Contains(strings.ToLower(ent), "workbuddy") {
			continue
		}
		// ...\workbuddy\resources\app.asar.unpacked\cli\bin
		if strings.EqualFold(filepath.Base(ent), "bin") {
			cli := filepath.Dir(ent)                                 // cli
			unpacked := filepath.Dir(cli)                            // app.asar.unpacked
			resources := filepath.Dir(unpacked)                      // resources
			root := filepath.Dir(resources)                          // install root
			out = append(out, root, ent)
			continue
		}
		out = append(out, ent)
	}
	return out
}

func sniffWorkBuddyFromProcess() []string {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snap, &pe); err != nil {
		return nil
	}
	var out []string
	for {
		name := windows.UTF16ToString(pe.ExeFile[:])
		if strings.EqualFold(name, "WorkBuddy.exe") {
			if p := processImagePath(pe.ProcessID); p != "" {
				out = append(out, filepath.Dir(p))
			}
		}
		if err := windows.Process32Next(snap, &pe); err != nil {
			break
		}
	}
	return out
}

func processImagePath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	var buf [windows.MAX_PATH]uint16
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}

func sniffWorkBuddyFromRegistry() []string {
	var out []string
	roots := []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE}
	subKeys := []string{
		`Software\Microsoft\Windows\CurrentVersion\Uninstall`,
		`Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`,
		`Software\Microsoft\Windows\CurrentVersion\App Paths\WorkBuddy.exe`,
	}
	for _, root := range roots {
		for _, sub := range subKeys {
			if strings.HasSuffix(strings.ToLower(sub), `workbuddy.exe`) {
				k, err := registry.OpenKey(root, sub, registry.QUERY_VALUE)
				if err != nil {
					continue
				}
				if v, _, err := k.GetStringValue(""); err == nil {
					out = append(out, v)
				}
				if v, _, err := k.GetStringValue("Path"); err == nil {
					out = append(out, v)
				}
				k.Close()
				continue
			}
			k, err := registry.OpenKey(root, sub, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			names, _ := k.ReadSubKeyNames(-1)
			for _, name := range names {
				sk, err := registry.OpenKey(k, name, registry.QUERY_VALUE)
				if err != nil {
					continue
				}
				display, _, _ := sk.GetStringValue("DisplayName")
				if !strings.Contains(strings.ToLower(display), "workbuddy") &&
					!strings.Contains(strings.ToLower(name), "workbuddy") {
					sk.Close()
					continue
				}
				if v, _, err := sk.GetStringValue("InstallLocation"); err == nil && strings.TrimSpace(v) != "" {
					out = append(out, v)
				}
				if v, _, err := sk.GetStringValue("DisplayIcon"); err == nil && strings.TrimSpace(v) != "" {
					out = append(out, strings.Split(v, ",")[0])
				}
				if v, _, err := sk.GetStringValue("UninstallString"); err == nil && strings.TrimSpace(v) != "" {
					if dir := uninstallExeDir(v); dir != "" {
						out = append(out, dir)
					}
				}
				sk.Close()
			}
			k.Close()
		}
	}
	return out
}

func uninstallExeDir(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	exe := v
	if strings.HasPrefix(v, `"`) {
		if i := strings.Index(v[1:], `"`); i >= 0 {
			exe = v[1 : 1+i]
		}
	} else if fields := strings.Fields(v); len(fields) > 0 {
		exe = fields[0]
	}
	dir := filepath.Dir(exe)
	if dir == "." || dir == string(filepath.Separator) {
		return ""
	}
	return dir
}
