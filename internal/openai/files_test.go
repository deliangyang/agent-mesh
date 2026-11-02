package openai

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyFilesWritesInbox(t *testing.T) {
	cwd := t.TempDir()
	raw := []byte("hello")
	got, err := ApplyFiles(cwd, "look at this", []ChatFile{{
		Name: "../x.png",
		MIME: "image/png",
		Data: base64.StdEncoding.EncodeToString(raw),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "look at this") || !strings.Contains(got, "<<MESH_ATTACH>>") {
		t.Fatalf("prompt %q", got)
	}
	if strings.Contains(got, "..") {
		t.Fatalf("path traversal in %q", got)
	}
	matches, _ := filepath.Glob(filepath.Join(cwd, ".agent-mesh", "inbox", "*-x.png"))
	if len(matches) != 1 {
		t.Fatalf("wrote %#v", matches)
	}
	b, err := os.ReadFile(matches[0])
	if err != nil || string(b) != "hello" {
		t.Fatalf("content %q %v", b, err)
	}
}

func TestApplyFilesNeedsCwd(t *testing.T) {
	_, err := ApplyFiles("", "hi", []ChatFile{{Name: "a.txt", Data: base64.StdEncoding.EncodeToString([]byte("a"))}})
	if err == nil {
		t.Fatal("want cwd error")
	}
}

func TestStripAttachBlock(t *testing.T) {
	got := StripAttachBlock("body\n\n<<MESH_ATTACH>>\n- /tmp/a.png\n<</MESH_ATTACH>>")
	if got != "body" {
		t.Fatalf("got %q", got)
	}
}

func TestCollectFilesFromParts(t *testing.T) {
	req := ChatRequest{
		Messages: []ChatMessage{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "text", "text": "hi"},
				map[string]any{
					"type": "image",
					"source": map[string]any{
						"type":     "data",
						"value":    "abc",
						"mimeType": "image/png",
					},
					"metadata": map[string]any{"filename": "a.png"},
				},
			},
		}},
	}
	files := CollectFiles(req)
	if len(files) != 1 || files[0].Name != "a.png" || files[0].Data != "abc" {
		t.Fatalf("%#v", files)
	}
}
