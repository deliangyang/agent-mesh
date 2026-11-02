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

// ScanSlash scans engine commands and skills in the user dir and workspace.
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
	case "qoder-cn":
		commands = []slashDir{
			{Path: project(".qoder", "commands"), Source: "project"},
			{Path: user(".qoder-cn", "commands"), Source: "user"},
		}
		skills = []slashDir{
			{Path: project(".qoder", "skills"), Source: "project"},
			{Path: user(".qoder-cn", "skills"), Source: "user"},
			{Path: project(".agents", "skills"), Source: "project"},
			{Path: user(".agents", "skills"), Source: "user"},
			{Path: user(".qoder-cn", "plugins"), Source: "plugin", Tree: true},
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
			{Name: "compact", Kind: "command", Description: "Compact the conversation", Source: "builtin"},
			{Name: "clear", Kind: "command", Description: "Clear the current chat", Source: "builtin"},
			{Name: "memory", Kind: "command", Description: "View or edit memory", Source: "builtin"},
			{Name: "review", Kind: "command", Description: "Review uncommitted changes", Source: "builtin"},
			{Name: "init", Kind: "command", Description: "Generate project notes", Source: "builtin"},
			{Name: "plan", Kind: "command", Description: "Enter plan mode", Source: "builtin"},
			{Name: "context", Kind: "command", Description: "Show context usage", Source: "builtin"},
			{Name: "skills", Kind: "command", Description: "List skills", Source: "builtin"},
			{Name: "help", Kind: "command", Description: "Help", Source: "builtin"},
		}
	case "cursor":
		return []protocol.SlashItem{
			{Name: "compact", Kind: "command", Description: "Compact the conversation", Source: "builtin"},
			{Name: "model", Kind: "command", Description: "Switch model", ArgumentHint: "[model]", Source: "builtin"},
			{Name: "rules", Kind: "command", Description: "Show rules", Source: "builtin"},
			{Name: "mcp", Kind: "command", Description: "MCP servers", Source: "builtin"},
			{Name: "commands", Kind: "command", Description: "Custom commands", Source: "builtin"},
			{Name: "new", Kind: "command", Description: "New chat", Source: "builtin"},
		}
	case "codex":
		return []protocol.SlashItem{
			{Name: "compact", Kind: "command", Description: "Compact the conversation", Source: "builtin"},
			{Name: "review", Kind: "command", Description: "Code review", Source: "builtin"},
			{Name: "init", Kind: "command", Description: "Generate project notes", Source: "builtin"},
			{Name: "status", Kind: "command", Description: "Session status", Source: "builtin"},
			{Name: "diff", Kind: "command", Description: "Show diff", Source: "builtin"},
			{Name: "undo", Kind: "command", Description: "Undo last turn", Source: "builtin"},
			{Name: "new", Kind: "command", Description: "New chat", Source: "builtin"},
			{Name: "model", Kind: "command", Description: "Switch model", ArgumentHint: "[model]", Source: "builtin"},
		}
	case "workbuddy":
		return []protocol.SlashItem{
			{Name: "compact", Kind: "command", Description: "Compact the conversation", Source: "builtin"},
			{Name: "clear", Kind: "command", Description: "Clear the current chat", Source: "builtin"},
		}
	case "opencode":
		return []protocol.SlashItem{
			{Name: "compact", Kind: "command", Description: "Compact the conversation", Source: "builtin"},
			{Name: "undo", Kind: "command", Description: "Undo the last turn", Source: "builtin"},
			{Name: "new", Kind: "command", Description: "New chat", Source: "builtin"},
			{Name: "models", Kind: "command", Description: "List models", Source: "builtin"},
		}
	case "pi":
		return []protocol.SlashItem{
			{Name: "compact", Kind: "command", Description: "Compact the conversation", Source: "builtin"},
			{Name: "new", Kind: "command", Description: "New chat", Source: "builtin"},
			{Name: "model", Kind: "command", Description: "Switch model", ArgumentHint: "[model]", Source: "builtin"},
			{Name: "resume", Kind: "command", Description: "Resume session", Source: "builtin"},
		}
	case "qoder-cn":
		return []protocol.SlashItem{
			{Name: "compact", Kind: "command", Description: "Compact the conversation", Source: "builtin"},
			{Name: "clear", Kind: "command", Description: "Clear the current chat", Source: "builtin"},
			{Name: "review", Kind: "command", Description: "Review uncommitted changes", Source: "builtin"},
			{Name: "init", Kind: "command", Description: "Generate project notes", Source: "builtin"},
			{Name: "plan", Kind: "command", Description: "Enter plan mode", Source: "builtin"},
			{Name: "new", Kind: "command", Description: "New chat", Source: "builtin"},
			{Name: "model", Kind: "command", Description: "Switch model", ArgumentHint: "[model]", Source: "builtin"},
			{Name: "skills", Kind: "command", Description: "List skills", Source: "builtin"},
			{Name: "help", Kind: "command", Description: "Help", Source: "builtin"},
		}
	default:
		return nil
	}
}
