package openai

import (
	"path/filepath"
	"testing"
)

func TestParseAttachFiles(t *testing.T) {
	raw := "look\n\n<<MESH_ATTACH>>\nThe user uploaded these attachments; inspect them with the Read tool:\n- /tmp/proj/.agent-mesh/inbox/a.png (image/png)\n- /tmp/proj/.agent-mesh/inbox/b.txt (text/plain)\n- /etc/passwd (text/plain)\n<</MESH_ATTACH>>"
	got := ParseAttachFiles(raw)
	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
	if got[0].Path != "/tmp/proj/.agent-mesh/inbox/a.png" || got[0].MIME != "image/png" {
		t.Fatalf("first %#v", got[0])
	}
	if got[1].Name != "b.txt" {
		t.Fatalf("second %#v", got[1])
	}
}

func TestIsInboxPath(t *testing.T) {
	ok := filepath.Join("/home/u", ".agent-mesh", "inbox", "x.png")
	if !IsInboxPath(ok) {
		t.Fatalf("want ok %q", ok)
	}
	if IsInboxPath("/etc/passwd") {
		t.Fatal("passwd")
	}
	if IsInboxPath("/tmp/.agent-mesh/inbox/../secret") {
		t.Fatal("dotdot")
	}
}
