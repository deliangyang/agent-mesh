package node

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListDirs(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"beta", "alpha"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := listDirs(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != root {
		t.Fatalf("path = %q", got.Path)
	}
	if len(got.Dirs) != 2 || got.Dirs[0].Name != "alpha" || got.Dirs[1].Name != "beta" {
		t.Fatalf("dirs = %#v", got.Dirs)
	}
	if got.Dirs[0].Path != filepath.Join(root, "alpha") {
		t.Fatalf("entry path = %q", got.Dirs[0].Path)
	}
}

func TestListDirsRejectsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := listDirs(path); err == nil {
		t.Fatal("expected an error for a file path")
	}
}
