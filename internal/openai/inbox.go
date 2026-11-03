package openai

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// InboxRel is the workspace-relative inbox directory for chat attachments.
const InboxRel = ".agent-mesh/inbox"

// AttachFile is one path listed inside a <<MESH_ATTACH>> block.
type AttachFile struct {
	Path string `json:"path"`
	MIME string `json:"mime,omitempty"`
	Name string `json:"name,omitempty"`
}

// ParseAttachFiles extracts inbox paths from a prompt that still contains MESH_ATTACH.
func ParseAttachFiles(text string) []AttachFile {
	s := text
	var out []AttachFile
	for {
		i := strings.Index(s, attachOpenMark)
		if i < 0 {
			break
		}
		j := strings.Index(s[i:], attachCloseMark)
		if j < 0 {
			break
		}
		block := s[i+len(attachOpenMark) : i+j]
		s = s[i+j+len(attachCloseMark):]
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "- ") {
				continue
			}
			line = strings.TrimSpace(line[2:])
			mime := ""
			if p := strings.LastIndex(line, " ("); p > 0 && strings.HasSuffix(line, ")") {
				mime = strings.TrimSuffix(line[p+2:], ")")
				line = strings.TrimSpace(line[:p])
			}
			if line == "" || !IsInboxPath(line) {
				continue
			}
			out = append(out, AttachFile{
				Path: line,
				MIME: mime,
				Name: filepath.Base(line),
			})
		}
	}
	return out
}

// IsInboxPath reports whether path is under a workspace .agent-mesh/inbox directory.
func IsInboxPath(path string) bool {
	clean := filepath.Clean(strings.TrimSpace(path))
	if clean == "" || clean == "." {
		return false
	}
	slash := filepath.ToSlash(clean)
	marker := "/" + InboxRel + "/"
	if !strings.Contains(slash, marker) {
		return false
	}
	return true
}

// OpenInboxFile opens a path only when it resolves inside .agent-mesh/inbox.
func OpenInboxFile(path string) (*os.File, string, error) {
	clean := filepath.Clean(strings.TrimSpace(path))
	if !IsInboxPath(clean) {
		return nil, "", fmt.Errorf("forbidden path")
	}
	abs, err := filepath.Abs(clean)
	if err != nil {
		return nil, "", err
	}
	if !IsInboxPath(abs) {
		return nil, "", fmt.Errorf("forbidden path")
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, "", err
	}
	ctype := mime.TypeByExtension(filepath.Ext(abs))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	return f, ctype, nil
}

// WriteInboxFile serves an inbox attachment with Content-Type.
func WriteInboxFile(w http.ResponseWriter, r *http.Request, path string) error {
	f, ctype, err := OpenInboxFile(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, filepath.Base(path), st.ModTime(), f)
	return nil
}
