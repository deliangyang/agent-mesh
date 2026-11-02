package driver

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// MeshHome is ~/.agent-mesh, or AGENT_MESH_HOME when set.
func MeshHome() string {
	if env := strings.TrimSpace(os.Getenv("AGENT_MESH_HOME")); env != "" {
		return env
	}
	return filepath.Join(Home(), ".agent-mesh")
}

// DataHome user data directory (XDG on Unix, %LOCALAPPDATA% on Windows).
func DataHome() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return x
	}
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return d
		}
		return filepath.Join(Home(), "AppData", "Local")
	}
	return filepath.Join(Home(), ".local", "share")
}

// EncodeProject turns a workspace path into the hyphenated project dir name
// Claude Code and Qoder CN use under ~/.claude/projects and ~/.qoder-cn/projects.
func EncodeProject(cwd string) string {
	s := strings.ReplaceAll(filepath.ToSlash(cwd), "\\", "/")
	return strings.ReplaceAll(s, "/", "-")
}

// PathHasDir reports whether path contains a directory segment.
func PathHasDir(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	slash := strings.ReplaceAll(filepath.ToSlash(path), "\\", "/")
	return strings.Contains(slash, "/"+dir+"/")
}

// BinExists reports whether a CLI path exists, including Windows PATHEXT.
func BinExists(path string) bool {
	if path == "" {
		return false
	}
	return FileExists(existingBin(path))
}

// LookCLI finds an engine CLI: env override, PATH, extra paths, then well-known dirs.
func LookCLI(envKeys, names, extra []string) string {
	for _, k := range envKeys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	var cands []string
	cands = append(cands, extra...)
	for _, name := range names {
		cands = append(cands, localBinCandidates(name)...)
	}
	for _, p := range cands {
		if p == "" {
			continue
		}
		if found := existingBin(p); FileExists(found) {
			return found
		}
	}
	if len(names) == 0 {
		return ""
	}
	return LocalBin(names[0])
}

// LocalBin is the default ~/.local/bin/<name> fallback (.exe on Windows).
func LocalBin(name string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(Home(), ".local", "bin", withExe(name))
	}
	return filepath.Join(Home(), ".local", "bin", name)
}

func withExe(name string) string {
	if name == "" || strings.Contains(name, ".") {
		return name
	}
	return name + ".exe"
}

func existingBin(path string) string {
	if FileExists(path) {
		return path
	}
	if runtime.GOOS != "windows" {
		return path
	}
	if filepath.Ext(path) != "" {
		return path
	}
	for _, ext := range []string{".exe", ".cmd", ".bat", ".com"} {
		p := path + ext
		if FileExists(p) {
			return p
		}
	}
	return path
}

func localBinCandidates(name string) []string {
	home := Home()
	out := []string{filepath.Join(home, ".local", "bin", name)}
	if runtime.GOOS != "windows" {
		return out
	}
	out = append(out, filepath.Join(home, ".local", "bin", name+".exe"))
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		out = append(out,
			filepath.Join(appdata, "npm", name+".cmd"),
			filepath.Join(appdata, "npm", name+".exe"),
			filepath.Join(appdata, "npm", name),
		)
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		out = append(out,
			filepath.Join(local, name+".exe"),
			filepath.Join(local, "Programs", name, name+".exe"),
		)
	}
	return out
}
