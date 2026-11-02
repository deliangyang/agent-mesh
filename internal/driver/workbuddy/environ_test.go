package workbuddy

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

func TestParseEnvironNUL(t *testing.T) {
	raw := []byte("FOO=bar\x00CODEBUDDY_GATEWAY_PASSWORD=secret\x00\x00")
	got := parseEnvironNUL(raw)
	if got["FOO"] != "bar" || got["CODEBUDDY_GATEWAY_PASSWORD"] != "secret" {
		t.Fatalf("%#v", got)
	}
	if parseEnvironNUL(nil) != nil {
		t.Fatalf("nil")
	}
}

func TestParseEnvironUTF16(t *testing.T) {
	entries := []string{"FOO=bar", "CODEBUDDY_GATEWAY_PASSWORD=secret"}
	var u16 []uint16
	for _, e := range entries {
		u16 = append(u16, utf16.Encode([]rune(e))...)
		u16 = append(u16, 0)
	}
	u16 = append(u16, 0) // terminating NUL
	raw := make([]byte, len(u16)*2)
	for i, v := range u16 {
		binary.LittleEndian.PutUint16(raw[i*2:], v)
	}
	got := parseEnvironUTF16(raw)
	if got["FOO"] != "bar" || got["CODEBUDDY_GATEWAY_PASSWORD"] != "secret" {
		t.Fatalf("%#v", got)
	}
}

func TestParseProcArgs2Env(t *testing.T) {
	// argc=1, exe="/bin/x", argv=["/bin/x"], env FOO=bar + CODEBUDDY_GATEWAY_PASSWORD=secret
	var b []byte
	b = binary.LittleEndian.AppendUint32(b, 1)
	b = append(b, "/bin/x\x00"...)
	b = append(b, 0, 0, 0) // padding
	b = append(b, "/bin/x\x00"...)
	b = append(b, "FOO=bar\x00CODEBUDDY_GATEWAY_PASSWORD=secret\x00\x00"...)
	got := parseProcArgs2Env(b)
	if got["FOO"] != "bar" || got["CODEBUDDY_GATEWAY_PASSWORD"] != "secret" {
		t.Fatalf("%#v", got)
	}
}

func TestProcessEnvironSelf(t *testing.T) {
	switch runtime.GOOS {
	case "windows", "linux", "darwin":
	default:
		t.Skip("process environ unsupported on " + runtime.GOOS)
	}
	got := processEnviron(os.Getpid())
	if len(got) == 0 {
		t.Fatalf("empty environ for self")
	}
}

func TestProcessEnvironLiveGatewayAuth(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows PEB path")
	}
	root := filepath.Join(os.Getenv("USERPROFILE"), ".workbuddy", "sessions")
	ents, err := os.ReadDir(root)
	if err != nil {
		t.Skip("no workbuddy sessions")
	}
	cli := &http.Client{Timeout: 3 * time.Second}
	for _, e := range ents {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, e.Name()))
		if err != nil {
			continue
		}
		var obj map[string]any
		if json.Unmarshal(raw, &obj) != nil {
			continue
		}
		pidF, _ := obj["pid"].(float64)
		pid := int(pidF)
		url, _ := obj["url"].(string)
		if pid == 0 || url == "" {
			continue
		}
		env := processEnviron(pid)
		pass := env["CODEBUDDY_GATEWAY_PASSWORD"]
		if pass == "" {
			pass = env["WORKBUDDY_GATEWAY_PASSWORD"]
		}
		if pass == "" {
			continue
		}
		req, err := http.NewRequest(http.MethodGet, strings.TrimRight(url, "/")+"/health", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+pass)
		resp, err := cli.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
			return
		}
		t.Fatalf("authed health status %d for pid %d", resp.StatusCode, pid)
	}
	t.Skip("no live interactive gateway with password in environ")
}
