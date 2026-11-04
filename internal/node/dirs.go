package node

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"agent-mesh/internal/protocol"
)

// listDirs browses one directory level. An empty path opens the node user's
// home; "~" and relative paths resolve against it too.
func listDirs(path string) (protocol.DirListing, error) {
	home, _ := os.UserHomeDir()
	p := strings.TrimSpace(path)
	if p == "" {
		p = home
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(home, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return protocol.DirListing{}, fmt.Errorf("dirs: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return protocol.DirListing{}, fmt.Errorf("dirs %s: %w", abs, err)
	}
	if !info.IsDir() {
		return protocol.DirListing{}, fmt.Errorf("dirs %s: not a directory", abs)
	}
	ents, err := os.ReadDir(abs)
	if err != nil {
		return protocol.DirListing{}, fmt.Errorf("dirs %s: %w", abs, err)
	}
	dirs := make([]protocol.DirEntry, 0, len(ents))
	for _, ent := range ents {
		full := filepath.Join(abs, ent.Name())
		isDir := ent.IsDir()
		if !isDir && ent.Type()&os.ModeSymlink != 0 {
			if target, err := os.Stat(full); err == nil && target.IsDir() {
				isDir = true
			}
		}
		if !isDir {
			continue
		}
		dirs = append(dirs, protocol.DirEntry{Name: ent.Name(), Path: full})
	}
	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})
	parent := filepath.Dir(abs)
	if parent == abs {
		parent = ""
	}
	return protocol.DirListing{Path: abs, Parent: parent, Home: home, Dirs: dirs}, nil
}

func (s *Server) dispatchDirs(req protocol.Request) protocol.Response {
	var p protocol.DirsParams
	_ = json.Unmarshal(req.Params, &p)
	listing, err := listDirs(p.Path)
	if err != nil {
		return protocol.Response{ID: req.ID, Error: err.Error()}
	}
	return protocol.Response{ID: req.ID, OK: true, Body: protocol.MustJSON(listing)}
}

func (s *Server) handleDirs(w http.ResponseWriter, r *http.Request) {
	listing, err := listDirs(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, listing)
}
