package openai

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const (
	maxChatFiles    = 8
	maxChatFileB    = 20 << 20
	attachOpenMark  = "<<MESH_ATTACH>>"
	attachCloseMark = "<</MESH_ATTACH>>"
)

// ChatFile 对话附件，data 为标准 Base64。
type ChatFile struct {
	Name string `json:"name"`
	MIME string `json:"mime"`
	Data string `json:"data"`
}

// CollectFiles 优先用请求里的 files，否则从最后一条用户内容块抽出。
func CollectFiles(req ChatRequest) []ChatFile {
	if len(req.Files) > 0 {
		return req.Files
	}
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if !strings.EqualFold(req.Messages[i].Role, "user") {
			continue
		}
		return filesFromContent(req.Messages[i].Content)
	}
	return nil
}

// ApplyFiles 把附件写到 cwd/.agent-mesh/inbox，并在提示末尾附上路径。
func ApplyFiles(cwd, text string, files []ChatFile) (string, error) {
	if len(files) == 0 {
		return text, nil
	}
	if len(files) > maxChatFiles {
		return "", fmt.Errorf("最多 %d 个附件", maxChatFiles)
	}
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "", fmt.Errorf("发送附件需要工作目录")
	}
	dir := filepath.Join(cwd, ".agent-mesh", "inbox")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	stamp := time.Now().Format("20060102-150405")
	lines := make([]string, 0, len(files))
	for i, f := range files {
		raw, err := decodeFileData(f.Data)
		if err != nil {
			return "", fmt.Errorf("附件 %s: %w", safeFileName(f.Name), err)
		}
		if len(raw) > maxChatFileB {
			return "", fmt.Errorf("附件 %s 超过 20MB", safeFileName(f.Name))
		}
		name := safeFileName(f.Name)
		path := filepath.Join(dir, fmt.Sprintf("%s-%d-%s", stamp, i+1, name))
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			return "", err
		}
		mime := strings.TrimSpace(f.MIME)
		if mime == "" {
			mime = "application/octet-stream"
		}
		lines = append(lines, "- "+path+" ("+mime+")")
	}
	body := strings.TrimSpace(text)
	if body == "" {
		body = "请查看附件。"
	}
	return body + "\n\n" + attachOpenMark + "\n用户上传了以下附件，请用 Read 工具查看：\n" + strings.Join(lines, "\n") + "\n" + attachCloseMark, nil
}

// StripAttachBlock 去掉提示里的附件路径块，供历史展示。
func StripAttachBlock(text string) string {
	s := text
	for {
		i := strings.Index(s, attachOpenMark)
		if i < 0 {
			break
		}
		j := strings.Index(s[i:], attachCloseMark)
		if j < 0 {
			s = strings.TrimSpace(s[:i])
			break
		}
		end := i + j + len(attachCloseMark)
		s = strings.TrimSpace(s[:i] + s[end:])
	}
	return s
}

func filesFromContent(v any) []ChatFile {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]ChatFile, 0)
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := m["type"].(string)
		if kind == "text" {
			continue
		}
		src, _ := m["source"].(map[string]any)
		if src == nil {
			continue
		}
		srcType, _ := src["type"].(string)
		data, _ := src["value"].(string)
		if srcType != "data" || data == "" {
			continue
		}
		mime, _ := src["mimeType"].(string)
		name := ""
		if meta, ok := m["metadata"].(map[string]any); ok {
			name, _ = meta["filename"].(string)
			if name == "" {
				name, _ = meta["name"].(string)
			}
		}
		out = append(out, ChatFile{Name: name, MIME: mime, Data: data})
	}
	return out
}

func decodeFileData(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "base64,"); i >= 0 {
		s = s[i+7:]
	}
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
	raw, err := base64.StdEncoding.DecodeString(s)
	if err == nil {
		return raw, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}

func safeFileName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "..", "_")
	var b strings.Builder
	for _, r := range name {
		if r < 32 {
			continue
		}
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" || out == "." || out == "_" {
		return "file.bin"
	}
	return out
}
