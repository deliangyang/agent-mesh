package driver

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"agent-mesh/internal/protocol"
)

const maxSlashItems = 300

type slashDir struct {
	Path   string
	Source string
	Tree   bool
}

// ScanSlash 扫描引擎在用户目录与工作目录中的 command / skill。
func ScanSlash(engine, cwd, home string) []protocol.SlashItem {
	engine = strings.TrimSpace(engine)
	if engine == "" {
		return []protocol.SlashItem{}
	}
	seen := map[string]bool{}
	out := make([]protocol.SlashItem, 0, 32)
	add := func(item protocol.SlashItem) {
		item.Name = cleanSlashName(item.Name)
		if item.Name == "" || seen[item.Name] || len(out) >= maxSlashItems {
			return
		}
		item.Description = Truncate(item.Description, 80)
		if item.Kind == "" {
			item.Kind = "command"
		}
		seen[item.Name] = true
		out = append(out, item)
	}

	cmds, skills := slashRoots(engine, cwd, home)
	for _, dir := range cmds {
		scanCommandDir(dir.Path, dir.Source, add)
	}
	for _, dir := range skills {
		if dir.Tree {
			scanSkillTree(dir.Path, dir.Source, add, 0)
		} else {
			scanSkillRoot(dir.Path, dir.Source, add)
		}
	}
	for _, item := range builtinSlash(engine) {
		add(item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == "command"
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func slashRoots(engine, cwd, home string) (commands, skills []slashDir) {
	project := func(rel ...string) string {
		if cwd == "" {
			return ""
		}
		return filepath.Join(append([]string{cwd}, rel...)...)
	}
	user := func(rel ...string) string {
		if home == "" {
			return ""
		}
		return filepath.Join(append([]string{home}, rel...)...)
	}
	switch engine {
	case "claude":
		commands = []slashDir{
			{Path: project(".claude", "commands"), Source: "project"},
			{Path: user(".claude", "commands"), Source: "user"},
		}
		skills = []slashDir{
			{Path: project(".claude", "skills"), Source: "project"},
			{Path: user(".claude", "skills"), Source: "user"},
			{Path: project(".agents", "skills"), Source: "project"},
			{Path: user(".agents", "skills"), Source: "user"},
			{Path: user(".claude", "plugins"), Source: "plugin", Tree: true},
		}
	case "cursor":
		commands = []slashDir{
			{Path: project(".cursor", "commands"), Source: "project"},
			{Path: user(".cursor", "commands"), Source: "user"},
		}
		skills = []slashDir{
			{Path: project(".cursor", "skills"), Source: "project"},
			{Path: user(".cursor", "skills"), Source: "user"},
			{Path: user(".cursor", "skills-cursor"), Source: "user"},
			{Path: project(".agents", "skills"), Source: "project"},
			{Path: user(".agents", "skills"), Source: "user"},
			{Path: user(".cursor", "plugins"), Source: "plugin", Tree: true},
		}
	case "codex":
		commands = []slashDir{
			{Path: project(".codex", "prompts"), Source: "project"},
			{Path: user(".codex", "prompts"), Source: "user"},
			{Path: project(".codex", "commands"), Source: "project"},
			{Path: user(".codex", "commands"), Source: "user"},
		}
		skills = []slashDir{
			{Path: project(".codex", "skills"), Source: "project"},
			{Path: user(".codex", "skills"), Source: "user"},
			{Path: project(".agents", "skills"), Source: "project"},
			{Path: user(".agents", "skills"), Source: "user"},
		}
	case "workbuddy":
		commands = []slashDir{
			{Path: project(".workbuddy", "commands"), Source: "project"},
			{Path: project(".codebuddy", "commands"), Source: "project"},
			{Path: user(".workbuddy", "commands"), Source: "user"},
			{Path: user(".codebuddy", "commands"), Source: "user"},
		}
		skills = []slashDir{
			{Path: project(".workbuddy", "skills"), Source: "project"},
			{Path: project(".codebuddy", "skills"), Source: "project"},
			{Path: user(".workbuddy", "skills"), Source: "user"},
			{Path: user(".codebuddy", "skills"), Source: "user"},
			{Path: project(".agents", "skills"), Source: "project"},
			{Path: user(".agents", "skills"), Source: "user"},
		}
	case "opencode":
		commands = []slashDir{
			{Path: project(".opencode", "commands"), Source: "project"},
			{Path: user(".config", "opencode", "commands"), Source: "user"},
			{Path: user(".opencode", "commands"), Source: "user"},
		}
		skills = []slashDir{
			{Path: project(".opencode", "skills"), Source: "project"},
			{Path: user(".config", "opencode", "skills"), Source: "user"},
			{Path: user(".opencode", "skills"), Source: "user"},
			{Path: project(".agents", "skills"), Source: "project"},
			{Path: user(".agents", "skills"), Source: "user"},
		}
	case "pi":
		commands = []slashDir{
			{Path: project(".pi", "commands"), Source: "project"},
			{Path: user(".pi", "agent", "prompts"), Source: "user"},
		}
		skills = []slashDir{
			{Path: project(".pi", "skills"), Source: "project"},
			{Path: user(".pi", "agent", "skills"), Source: "user"},
			{Path: project(".agents", "skills"), Source: "project"},
			{Path: user(".agents", "skills"), Source: "user"},
		}
	default:
		skills = []slashDir{
			{Path: project(".agents", "skills"), Source: "project"},
			{Path: user(".agents", "skills"), Source: "user"},
		}
	}
	return filterSlashDirs(commands), filterSlashDirs(skills)
}

func filterSlashDirs(in []slashDir) []slashDir {
	out := make([]slashDir, 0, len(in))
	for _, d := range in {
		if d.Path != "" {
			out = append(out, d)
		}
	}
	return out
}

func scanCommandDir(root, source string, add func(protocol.SlashItem)) {
	if root == "" {
		return
	}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirName(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.EqualFold(filepath.Ext(name), ".md") {
			return nil
		}
		if strings.EqualFold(name, "readme.md") || strings.EqualFold(name, "skill.md") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		slashName := strings.TrimSuffix(rel, filepath.Ext(rel))
		slashName = strings.ReplaceAll(slashName, string(os.PathSeparator), ":")
		item := readSlashFile(path, slashName, "command", source)
		add(item)
		return nil
	})
}

func scanSkillRoot(root, source string, add func(protocol.SlashItem)) {
	if root == "" {
		return
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range ents {
		if !e.IsDir() || skipDirName(e.Name()) {
			continue
		}
		addSkillDir(filepath.Join(root, e.Name()), source, add)
	}
}

func scanSkillTree(root, source string, add func(protocol.SlashItem), depth int) {
	if root == "" || depth > 8 {
		return
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return
	}
	if skillPath := skillFile(root); skillPath != "" {
		addSkillDir(root, source, add)
		return
	}
	for _, e := range ents {
		if !e.IsDir() || skipDirName(e.Name()) {
			continue
		}
		next := filepath.Join(root, e.Name())
		if strings.EqualFold(e.Name(), "skills") {
			scanSkillRoot(next, source, add)
			continue
		}
		scanSkillTree(next, source, add, depth+1)
	}
}

func addSkillDir(dir, source string, add func(protocol.SlashItem)) {
	path := skillFile(dir)
	if path == "" {
		return
	}
	item := readSlashFile(path, filepath.Base(dir), "skill", source)
	add(item)
}

func skillFile(dir string) string {
	for _, name := range []string{"SKILL.md", "skill.md"} {
		p := filepath.Join(dir, name)
		if FileExists(p) {
			return p
		}
	}
	return ""
}

func readSlashFile(path, fallback, kind, source string) protocol.SlashItem {
	item := protocol.SlashItem{Name: fallback, Kind: kind, Source: source}
	raw, err := os.ReadFile(path)
	if err != nil {
		return item
	}
	if len(raw) > 16*1024 {
		raw = cutByteSlice(raw, 16*1024)
	}
	meta, body := parseFrontMatter(raw)
	if name := meta["name"]; name != "" {
		item.Name = name
	}
	if desc := meta["description"]; desc != "" && desc != ">" && desc != ">-" && desc != "|" && desc != "|-" && desc != "|+" {
		item.Description = desc
	} else {
		item.Description = firstPlainLine(body)
	}
	if hint := meta["argument-hint"]; hint != "" {
		item.ArgumentHint = hint
	} else if hint := meta["argument_hint"]; hint != "" {
		item.ArgumentHint = hint
	}
	return item
}

func parseFrontMatter(raw []byte) (map[string]string, string) {
	s := string(raw)
	trim := strings.TrimSpace(s)
	if !strings.HasPrefix(trim, "---") {
		return map[string]string{}, s
	}
	rest := strings.TrimPrefix(trim, "---")
	rest = strings.TrimLeft(rest, "\r\n")
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return map[string]string{}, s
	}
	head := rest[:idx]
	body := strings.TrimSpace(rest[idx+4:])
	meta := map[string]string{}
	for _, line := range strings.Split(head, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if k != "" && v != "" {
			meta[k] = v
		}
	}
	return meta, body
}

func firstPlainLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "```") {
			continue
		}
		return line
	}
	return ""
}

func skipDirName(name string) bool {
	switch strings.ToLower(name) {
	case "node_modules", ".git", "dist", "vendor", ".venv", "__pycache__":
		return true
	default:
		return false
	}
}

func cleanSlashName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "/")
	s = strings.ToLower(s)
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case r == ':' || r == '_' || r == '.':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '/' || unicode.IsSpace(r):
			if b.Len() == 0 || lastDash {
				continue
			}
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-:")
}

func builtinSlash(engine string) []protocol.SlashItem {
	switch engine {
	case "claude":
		return []protocol.SlashItem{
			{Name: "compact", Kind: "command", Description: "压缩对话上下文", Source: "builtin"},
			{Name: "clear", Kind: "command", Description: "清空当前对话", Source: "builtin"},
			{Name: "memory", Kind: "command", Description: "查看或编辑记忆", Source: "builtin"},
			{Name: "review", Kind: "command", Description: "审查未提交改动", Source: "builtin"},
			{Name: "init", Kind: "command", Description: "生成项目说明", Source: "builtin"},
			{Name: "plan", Kind: "command", Description: "进入计划模式", Source: "builtin"},
			{Name: "context", Kind: "command", Description: "查看上下文用量", Source: "builtin"},
			{Name: "skills", Kind: "command", Description: "列出可用技能", Source: "builtin"},
			{Name: "help", Kind: "command", Description: "帮助", Source: "builtin"},
		}
	case "cursor":
		return []protocol.SlashItem{
			{Name: "compact", Kind: "command", Description: "压缩对话上下文", Source: "builtin"},
			{Name: "model", Kind: "command", Description: "切换模型", ArgumentHint: "[model]", Source: "builtin"},
			{Name: "rules", Kind: "command", Description: "查看规则", Source: "builtin"},
			{Name: "mcp", Kind: "command", Description: "MCP 服务器", Source: "builtin"},
			{Name: "commands", Kind: "command", Description: "自定义命令", Source: "builtin"},
			{Name: "new", Kind: "command", Description: "新对话", Source: "builtin"},
		}
	case "codex":
		return []protocol.SlashItem{
			{Name: "compact", Kind: "command", Description: "压缩对话上下文", Source: "builtin"},
			{Name: "review", Kind: "command", Description: "代码审查", Source: "builtin"},
			{Name: "init", Kind: "command", Description: "生成项目说明", Source: "builtin"},
			{Name: "status", Kind: "command", Description: "会话状态", Source: "builtin"},
			{Name: "diff", Kind: "command", Description: "查看改动", Source: "builtin"},
			{Name: "undo", Kind: "command", Description: "撤销上一轮", Source: "builtin"},
			{Name: "new", Kind: "command", Description: "新会话", Source: "builtin"},
			{Name: "model", Kind: "command", Description: "切换模型", ArgumentHint: "[model]", Source: "builtin"},
		}
	case "workbuddy":
		return []protocol.SlashItem{
			{Name: "compact", Kind: "command", Description: "压缩对话上下文", Source: "builtin"},
			{Name: "clear", Kind: "command", Description: "清空当前对话", Source: "builtin"},
		}
	case "opencode":
		return []protocol.SlashItem{
			{Name: "compact", Kind: "command", Description: "压缩对话上下文", Source: "builtin"},
			{Name: "undo", Kind: "command", Description: "撤销上一轮改动", Source: "builtin"},
			{Name: "new", Kind: "command", Description: "新会话", Source: "builtin"},
			{Name: "models", Kind: "command", Description: "列出模型", Source: "builtin"},
		}
	case "pi":
		return []protocol.SlashItem{
			{Name: "compact", Kind: "command", Description: "压缩对话上下文", Source: "builtin"},
			{Name: "new", Kind: "command", Description: "新会话", Source: "builtin"},
			{Name: "model", Kind: "command", Description: "切换模型", ArgumentHint: "[model]", Source: "builtin"},
			{Name: "resume", Kind: "command", Description: "恢复会话", Source: "builtin"},
		}
	default:
		return nil
	}
}
