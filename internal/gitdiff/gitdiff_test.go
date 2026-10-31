package gitdiff

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestListAndDiff(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "app")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	run("git", "init", "-b", "main")
	run("git", "config", "user.email", "t@t")
	run("git", "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", "a.txt")
	run("git", "commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	rows, err := List(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "app" || !rows[0].Dirty {
		t.Fatalf("list %#v", rows)
	}
	bundle, err := Diff(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Files) < 2 {
		t.Fatalf("files %#v", bundle.Files)
	}
	var sawEdit, sawNew bool
	for _, f := range bundle.Files {
		if f.Path == "a.txt" && f.Patch != "" {
			sawEdit = true
		}
		if f.Path == "new.txt" && f.Status == "未跟踪" {
			sawNew = true
		}
	}
	if !sawEdit || !sawNew {
		t.Fatalf("diff %#v", bundle.Files)
	}
}
