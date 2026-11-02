package driver

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEncodeProject(t *testing.T) {
	t.Run("unix", func(t *testing.T) {
		got := EncodeProject("/tmp/proj")
		if got != "-tmp-proj" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("windows", func(t *testing.T) {
		got := EncodeProject(`C:\Users\foo\proj`)
		if got != "C:-Users-foo-proj" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestPathHasDir(t *testing.T) {
	if !PathHasDir("/home/x/.claude/projects/a/subagents/b.jsonl", "subagents") {
		t.Fatalf("unix subagents")
	}
	if !PathHasDir(`C:\Users\x\.claude\projects\a\subagents\b.jsonl`, "subagents") {
		t.Fatalf("windows subagents")
	}
	if PathHasDir("/home/x/.claude/projects/a.jsonl", "subagents") {
		t.Fatalf("false positive")
	}
}

func TestMeshHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENT_MESH_HOME", root)
	if got := MeshHome(); got != root {
		t.Fatalf("got %q", got)
	}
}

func TestLookCLIEnvAndExtra(t *testing.T) {
	t.Setenv("MESH_TEST_BIN", "/tmp/custom-engine")
	if got := LookCLI([]string{"MESH_TEST_BIN"}, []string{"engine"}, nil); got != "/tmp/custom-engine" {
		t.Fatalf("env %q", got)
	}
	t.Setenv("MESH_TEST_BIN", "")
	t.Setenv("PATH", "/nonexistent")
	root := t.TempDir()
	t.Setenv("HOME", root)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", root)
	}
	extra := filepath.Join(root, "opt", "engine")
	if err := os.MkdirAll(filepath.Dir(extra), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extra, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := LookCLI([]string{"MESH_TEST_BIN"}, []string{"engine"}, []string{extra}); got != extra {
		t.Fatalf("extra %q", got)
	}
}

func TestBinExistsWindowsExt(t *testing.T) {
	if runtime.GOOS != "windows" {
		root := t.TempDir()
		p := filepath.Join(root, "tool")
		if BinExists(p) {
			t.Fatalf("missing file")
		}
		if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
		if !BinExists(p) {
			t.Fatalf("present file")
		}
		return
	}
	root := t.TempDir()
	p := filepath.Join(root, "tool")
	if err := os.WriteFile(p+".exe", []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !BinExists(p) {
		t.Fatalf("exe suffix")
	}
	if existingBin(p) != p+".exe" {
		t.Fatalf("existing %q", existingBin(p))
	}
}
