// Package gitdiff finds git repos under a workspace and reads the working-tree diff.
package gitdiff

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"agent-mesh/internal/driver"
)

const (
	maxDepth   = 3
	maxRepos   = 24
	maxPatch   = 400 * 1024
	gitTimeout = 8 * time.Second
)

// Repo is a git worktree.
type Repo struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Branch string `json:"branch,omitempty"`
	Dirty  bool   `json:"dirty"`
	Files  int    `json:"files,omitempty"`
}

// File is a changed file.
type File struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Patch  string `json:"patch,omitempty"`
}

// Bundle is a full diff for one repo.
type Bundle struct {
	Repo  Repo   `json:"repo"`
	Files []File `json:"files"`
}

// List walks git repos under root. If root itself is a repo it is included, and nested repos are still scanned.
func List(ctx context.Context, root string) ([]Repo, error) {
	root, err := cleanRoot(root)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	var out []Repo
	seen := map[string]bool{}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if ctx.Err() != nil || len(out) >= maxRepos {
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(root, path)
		depth := 0
		if rel != "." {
			depth = strings.Count(rel, string(os.PathSeparator)) + 1
		}
		if depth > maxDepth {
			return filepath.SkipDir
		}
		if skipDir(d.Name()) && path != root {
			return filepath.SkipDir
		}
		if isGitDir(path) && !seen[path] {
			seen[path] = true
			out = append(out, inspect(ctx, path))
		}
		return nil
	})
	return out, nil
}

// Diff reads working-tree changes versus HEAD, including untracked files.
func Diff(ctx context.Context, repo string) (Bundle, error) {
	repo, err := cleanRoot(repo)
	if err != nil {
		return Bundle{}, err
	}
	if !isGitDir(repo) {
		return Bundle{}, os.ErrNotExist
	}
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	b := Bundle{Repo: inspect(ctx, repo)}
	status := gitOut(ctx, repo, "status", "--porcelain=v1", "-uall")
	tracked := gitOut(ctx, repo, "diff", "HEAD", "--no-color", "--")
	patches := splitPatches(tracked)
	for _, line := range strings.Split(status, "\n") {
		if len(line) < 4 {
			continue
		}
		code := strings.TrimSpace(line[:2])
		name := strings.TrimSpace(line[3:])
		if i := strings.Index(name, " -> "); i >= 0 {
			name = name[i+4:]
		}
		name = strings.Trim(name, `"`)
		if name == "" {
			continue
		}
		f := File{Path: name, Status: statusLabel(code)}
		if code == "??" {
			f.Patch = untrackedPatch(ctx, repo, name)
		} else if p := patches[name]; p != "" {
			f.Patch = p
		} else if p := patches["/"+name]; p != "" {
			f.Patch = p
		}
		b.Files = append(b.Files, f)
	}
	if len(b.Files) == 0 {
		for _, p := range patches {
			b.Files = append(b.Files, File{Path: patchPath(p), Status: "modified", Patch: p})
		}
	}
	b.Repo.Files = len(b.Files)
	b.Repo.Dirty = len(b.Files) > 0
	return b, nil
}

func inspect(ctx context.Context, path string) Repo {
	r := Repo{Path: path, Name: filepath.Base(path)}
	r.Branch = firstLine(gitOut(ctx, path, "rev-parse", "--abbrev-ref", "HEAD"))
	st := gitOut(ctx, path, "status", "--porcelain=v1")
	n := 0
	for _, line := range strings.Split(st, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	r.Files = n
	r.Dirty = n > 0
	return r
}

func isGitDir(path string) bool {
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		return true
	}
	return false
}

func skipDir(name string) bool {
	switch strings.ToLower(name) {
	case "node_modules", ".git", "dist", "vendor", ".venv", "__pycache__", "target", "build", ".next", ".output":
		return true
	default:
		return false
	}
}

func cleanRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		root = wd
	}
	return filepath.Abs(root)
}

func gitOut(ctx context.Context, dir string, args ...string) string {
	cmd := driver.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = nil
	_ = cmd.Run()
	return strings.TrimRight(buf.String(), "\n")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func statusLabel(code string) string {
	switch strings.TrimSpace(code) {
	case "??":
		return "untracked"
	case "A", "AM", "AD":
		return "added"
	case "D", "MD":
		return "deleted"
	case "R", "RM":
		return "renamed"
	case "C":
		return "copied"
	case "UU", "AA", "DD":
		return "conflict"
	default:
		return "modified"
	}
}

func splitPatches(raw string) map[string]string {
	out := map[string]string{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	parts := strings.Split(raw, "\ndiff --git ")
	for i, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		if i > 0 {
			part = "diff --git " + part
		} else if !strings.HasPrefix(part, "diff --git ") {
			part = "diff --git " + part
		}
		if len(part) > maxPatch {
			part = cutBytes(part, maxPatch) + "\n…(truncated)"
		}
		out[patchPath(part)] = part
	}
	return out
}

func patchPath(patch string) string {
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			return strings.TrimPrefix(line, "+++ b/")
		}
		if strings.HasPrefix(line, "diff --git ") {
			fields := strings.Fields(line)
			if len(fields) >= 4 {
				return strings.TrimPrefix(fields[3], "b/")
			}
		}
	}
	return ""
}

func untrackedPatch(ctx context.Context, repo, rel string) string {
	abs := filepath.Join(repo, rel)
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return ""
	}
	if info.Size() > maxPatch {
		return "untracked file too large to expand"
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return ""
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		return "binary file"
	}
	text := string(raw)
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	var b strings.Builder
	b.WriteString("diff --git a/" + rel + " b/" + rel + "\n")
	b.WriteString("new file mode 100644\n")
	b.WriteString("--- /dev/null\n")
	b.WriteString("+++ b/" + rel + "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	b.WriteString("@@ -0,0 +1," + itoa(len(lines)) + " @@\n")
	n := 0
	for _, line := range lines {
		n += len(line) + 2
		if n > maxPatch {
			b.WriteString("…(truncated)\n")
			break
		}
		b.WriteString("+" + line + "\n")
	}
	return b.String()
}

func cutBytes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
