package followup

import (
	"testing"
)

func TestPushPopUpdateDelete(t *testing.T) {
	SetRoot(t.TempDir())
	f, err := Push("cursor", "sess-1", "/tmp/app", "fix scaling first")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Items) != 1 || f.Items[0].Text != "fix scaling first" || f.CWD != "/tmp/app" {
		t.Fatalf("push %#v", f)
	}
	id1 := f.Items[0].ID
	f, err = Push("cursor", "sess-1", "", "then change the command")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Items) != 2 {
		t.Fatalf("len %d", len(f.Items))
	}
	f, err = Update("cursor", "sess-1", f.Items[1].ID, "then change commands and skills")
	if err != nil {
		t.Fatal(err)
	}
	if f.Items[1].Text != "then change commands and skills" {
		t.Fatalf("update %#v", f.Items[1])
	}
	got, err := List("cursor", "sess-1")
	if err != nil || len(got.Items) != 2 {
		t.Fatalf("list %#v %v", got, err)
	}
	it, rest, ok, err := Pop("cursor", "sess-1")
	if err != nil || !ok || it.ID != id1 || len(rest.Items) != 1 {
		t.Fatalf("pop %#v %#v %v %v", it, rest, ok, err)
	}
	if _, err := Delete("cursor", "sess-1", rest.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	empty, err := List("cursor", "sess-1")
	if err != nil || len(empty.Items) != 0 {
		t.Fatalf("empty %#v %v", empty, err)
	}
	all, err := ListAll()
	if err != nil || len(all) != 0 {
		t.Fatalf("listall %#v %v", all, err)
	}
}

func TestUnshiftAndListAll(t *testing.T) {
	SetRoot(t.TempDir())
	if _, err := Push("claude", "abc", "/work", "one"); err != nil {
		t.Fatal(err)
	}
	it, _, ok, err := Pop("claude", "abc")
	if err != nil || !ok {
		t.Fatalf("pop %v %v", ok, err)
	}
	if err := Unshift("claude", "abc", "/work", it); err != nil {
		t.Fatal(err)
	}
	all, err := ListAll()
	if err != nil || len(all) != 1 || all[0].Engine != "claude" || len(all[0].Items) != 1 {
		t.Fatalf("listall %#v %v", all, err)
	}
}
