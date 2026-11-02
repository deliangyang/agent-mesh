package driver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanSlashCommandsAndSkills(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(cwd, ".claude", "commands", "frontend", "review.md"), `---
description: 审查前端改动
argument-hint: "[files]"
---
# Review
`)
	write(filepath.Join(home, ".claude", "skills", "code-review", "SKILL.md"), `---
name: code-review
description: 按仓库规范做代码审查
---
# Code Review
`)
	write(filepath.Join(home, ".agents", "skills", "tapd-cli", "SKILL.md"), `---
name: tapd-cli
description: 操作 TAPD
---
`)
	rows := ScanSlash("claude", cwd, home)
	got := map[string]string{}
	for _, row := range rows {
		got[row.Name] = row.Kind + "|" + row.Source + "|" + row.Description + "|" + row.ArgumentHint
	}
	if got["frontend:review"] != "command|project|审查前端改动|[files]" {
		t.Fatalf("command %#v", got["frontend:review"])
	}
	if got["code-review"] != "skill|user|按仓库规范做代码审查|" {
		t.Fatalf("skill %#v", got["code-review"])
	}
	if got["tapd-cli"] != "skill|user|操作 TAPD|" {
		t.Fatalf("agents skill %#v", got["tapd-cli"])
	}
	if got["compact"] != "command|builtin|压缩对话上下文|" {
		t.Fatalf("builtin %#v", got["compact"])
	}
	if rows[0].Kind != "command" {
		t.Fatalf("commands should come first: %#v", rows[0])
	}
}

func TestScanSlashProjectOverridesUser(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude", "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cwd, ".claude", "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(home, ".claude", "commands", "review.md"), []byte("---\ndescription: user\n---\n"), 0o644)
	_ = os.WriteFile(filepath.Join(cwd, ".claude", "commands", "review.md"), []byte("---\ndescription: project\n---\n"), 0o644)
	rows := ScanSlash("claude", cwd, home)
	for _, row := range rows {
		if row.Name == "review" {
			if row.Source != "project" || row.Description != "project" {
				t.Fatalf("%#v", row)
			}
			return
		}
	}
	t.Fatal("missing review")
}

func TestScanSlashQoderCN(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	cmd := filepath.Join(cwd, ".qoder", "commands", "git-commit.md")
	if err := os.MkdirAll(filepath.Dir(cmd), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cmd, []byte("---\ndescription: 生成提交信息\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(home, ".qoder-cn", "skills", "code-review", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skill, []byte("---\nname: code-review\ndescription: 按仓库规范做代码审查\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rows := ScanSlash("qoder-cn", cwd, home)
	got := map[string]string{}
	for _, row := range rows {
		got[row.Name] = row.Kind + "|" + row.Source + "|" + row.Description
	}
	if got["git-commit"] != "command|project|生成提交信息" {
		t.Fatalf("command %#v", got["git-commit"])
	}
	if got["code-review"] != "skill|user|按仓库规范做代码审查" {
		t.Fatalf("skill %#v", got["code-review"])
	}
	if got["compact"] != "command|builtin|压缩对话上下文" {
		t.Fatalf("builtin %#v", got["compact"])
	}
}

func TestCleanSlashName(t *testing.T) {
	if got := cleanSlashName("/Foo Bar"); got != "foo-bar" {
		t.Fatal(got)
	}
	if got := cleanSlashName("frontend:component"); got != "frontend:component" {
		t.Fatal(got)
	}
	if got := cleanSlashName("Code_Review"); got != "code_review" {
		t.Fatal(got)
	}
}
