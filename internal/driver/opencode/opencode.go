package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/protocol"
	"agent-mesh/internal/watchfile"
)

// Driver 接管本机 OpenCode CLI。
type Driver struct {
	root string
	bin  string
}

// New 创建驱动。
func New() *Driver {
	root := os.Getenv("OPENCODE_DATA_DIR")
	if root == "" {
		root = filepath.Join(driver.DataHome(), "opencode")
	}
	bin := os.Getenv("OPENCODE_BIN")
	if bin == "" {
		bin, _ = exec.LookPath("opencode")
	}
	if bin == "" {
		bin = filepath.Join(driver.Home(), ".opencode", "bin", "opencode")
	}
	if !driver.FileExists(bin) {
		if p, err := exec.LookPath("opencode"); err == nil {
			bin = p
		}
	}
	return &Driver{root: root, bin: bin}
}

func (d *Driver) Name() string { return "opencode" }

func (d *Driver) Probe(ctx context.Context) protocol.EngineInfo {
	info := protocol.EngineInfo{
		Name:   d.Name(),
		Title:  driver.TitleOf(d.Name()),
		Binary: d.bin,
		Detail: d.root,
	}
	info.Available = driver.FileExists(d.bin)
	if info.Available {
		info.Version = driver.BinVersion(ctx, d.bin)
	}
	return info
}

func (d *Driver) Models(ctx context.Context) ([]protocol.Model, error) {
	out, err := driver.RunOutput(ctx, 12*time.Second, d.bin, "models")
	if err != nil && out == "" {
		return defaultModel(d.Name()), err
	}
	rows := parseModelLines(d.Name(), out)
	if len(rows) == 0 {
		return defaultModel(d.Name()), nil
	}
	return rows, nil
}

func (d *Driver) List(ctx context.Context, all bool) ([]protocol.Session, error) {
	n := 40
	if all {
		n = 200
	}
	q := fmt.Sprintf("select id, directory, title, time_updated from session order by time_updated desc limit %d", n)
	out, err := driver.RunOutput(ctx, 12*time.Second, d.bin, "db", "--format", "json", q)
	rows := parseSessionList(d.Name(), out)
	if len(rows) == 0 {
		cliN := "20"
		if all {
			cliN = "80"
		}
		out, err = driver.RunOutput(ctx, 12*time.Second, d.bin, "session", "list", "--format", "json", "--max-count", cliN)
		rows = parseSessionList(d.Name(), out)
		if len(rows) == 0 && err != nil {
			return []protocol.Session{}, err
		}
	}
	if !all {
		live := []protocol.Session{}
		for _, s := range rows {
			if s.Live || s.Busy {
				live = append(live, s)
			}
		}
		return live, nil
	}
	return rows, nil
}

func (d *Driver) findJSONL(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	roots := []string{d.root}
	if home := driver.DataHome(); home != "" {
		roots = append(roots, filepath.Join(home, "opencode"))
	}
	if h := driver.Home(); h != "" {
		roots = append(roots, filepath.Join(h, ".local", "share", "opencode"))
	}
	names := []string{sessionID + ".jsonl", sessionID + ".json"}
	subs := []string{"", "session", "sessions", filepath.Join("storage", "sessions")}
	seen := map[string]bool{}
	for _, root := range roots {
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		for _, sub := range subs {
			dir := root
			if sub != "" {
				dir = filepath.Join(root, sub)
			}
			for _, name := range names {
				p := filepath.Join(dir, name)
				if driver.FileExists(p) {
					return p
				}
			}
		}
	}
	return ""
}

func (d *Driver) History(ctx context.Context, sessionID string, tail int) ([]protocol.Event, error) {
	if path := d.findJSONL(sessionID); path != "" {
		var evs []protocol.Event
		err := watchfile.TailJSONL(path, tail, func(obj map[string]any) {
			evs = append(evs, driver.EventsFromJSONL(obj, d.Name(), sessionID)...)
		})
		if evs == nil {
			evs = []protocol.Event{}
		}
		return evs, err
	}
	out, err := driver.RunOutput(ctx, 2*time.Second, d.bin, "export", sessionID)
	evs := parseExport(out, d.Name(), sessionID)
	if len(evs) == 0 && err != nil {
		return []protocol.Event{}, fmt.Errorf("session %s has no jsonl", sessionID)
	}
	if tail > 0 && len(evs) > tail {
		evs = evs[len(evs)-tail:]
	}
	if evs == nil {
		evs = []protocol.Event{}
	}
	return evs, nil
}

func (d *Driver) Watch(ctx context.Context, sessionID string) (<-chan protocol.Event, error) {
	if path := d.findJSONL(sessionID); path != "" {
		rows, err := watchfile.PollJSONL(ctx, path, true, 300*time.Millisecond)
		if err != nil {
			return nil, err
		}
		out := make(chan protocol.Event, 256)
		go func() {
			defer close(out)
			for obj := range rows {
				for _, ev := range driver.EventsFromJSONL(obj, d.Name(), sessionID) {
					out <- ev
				}
			}
		}()
		return out, nil
	}
	out := make(chan protocol.Event, 32)
	go d.pollExport(ctx, sessionID, out)
	return out, nil
}

func (d *Driver) pollExport(ctx context.Context, sessionID string, out chan protocol.Event) {
	defer close(out)
	seen := -1
	tick := time.NewTicker(800 * time.Millisecond)
	defer tick.Stop()
	for {
		evs, err := d.History(ctx, sessionID, 0)
		if err == nil {
			if seen < 0 {
				seen = len(evs)
			} else if len(evs) > seen {
				for _, ev := range evs[seen:] {
					select {
					case <-ctx.Done():
						return
					case out <- ev:
					}
				}
				seen = len(evs)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (d *Driver) Reply(ctx context.Context, sessionID, text string) (<-chan protocol.Event, error) {
	return d.run(ctx, "", text, sessionID)
}

func (d *Driver) Send(ctx context.Context, cwd, text, resume string) (<-chan protocol.Event, error) {
	return d.run(ctx, cwd, text, resume)
}

func (d *Driver) run(ctx context.Context, cwd, text, resume string) (<-chan protocol.Event, error) {
	args := []string{"run", "--format", "json", "--auto"}
	title := ""
	if resume != "" {
		args = append(args, "--session", resume)
	} else {
		title = driver.Truncate(text, 80)
		if title != "" {
			args = append(args, "--title", title)
		}
	}
	if cwd != "" {
		args = append(args, "--dir", cwd)
	}
	args = append(args, text)
	cmd := exec.CommandContext(ctx, d.bin, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	out := make(chan protocol.Event, 32)
	go func() {
		defer close(out)
		defer cmd.Wait()
		sid := resume
		if sid == "" && title != "" {
			out <- protocol.Event{Type: "title", Engine: d.Name(), Text: title}
		}
		driver.StreamJSONL(stdout, d.Name(), resume, func(ev protocol.Event) {
			if ev.Session != "" {
				sid = ev.Session
			}
			if ev.Type == "assistant" {
				ev.Type = "delta"
			}
			out <- ev
		})
		out <- protocol.Event{Type: "log", Engine: d.Name(), Session: sid, Done: true}
	}()
	return out, nil
}

func defaultModel(engine string) []protocol.Model {
	return []protocol.Model{{
		ID: engine, Engine: engine, Name: "default", Title: "默认", Default: true,
	}}
}

func parseModelLines(engine, out string) []protocol.Model {
	rows := []protocol.Model{}
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "─") || strings.HasPrefix(strings.ToLower(line), "provider") {
			continue
		}
		name := line
		if i := strings.IndexAny(line, " \t"); i > 0 {
			name = strings.TrimSpace(line[:i])
		}
		name = strings.Trim(name, "│|")
		if name == "" || !strings.Contains(name, "/") || seen[name] {
			continue
		}
		seen[name] = true
		rows = append(rows, protocol.Model{
			ID:     engine + "/" + name,
			Engine: engine,
			Name:   name,
			Title:  name,
		})
	}
	if len(rows) > 0 {
		rows[0].Default = true
	}
	return rows
}

func parseSessionList(engine, out string) []protocol.Session {
	raw := extractJSON(out)
	if raw == "" {
		return []protocol.Session{}
	}
	var arr []map[string]any
	if json.Unmarshal([]byte(raw), &arr) != nil {
		var wrap struct {
			Sessions []map[string]any `json:"sessions"`
			Data     []map[string]any `json:"data"`
		}
		if json.Unmarshal([]byte(raw), &wrap) == nil {
			arr = wrap.Sessions
			if len(arr) == 0 {
				arr = wrap.Data
			}
		}
	}
	if len(arr) == 0 {
		for _, line := range strings.Split(raw, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "{") {
				continue
			}
			var row map[string]any
			if json.Unmarshal([]byte(line), &row) == nil {
				arr = append(arr, row)
			}
		}
	}
	outRows := make([]protocol.Session, 0, len(arr))
	for _, row := range arr {
		id, _ := row["id"].(string)
		if id == "" {
			id, _ = row["sessionID"].(string)
		}
		if id == "" {
			continue
		}
		title, _ := row["title"].(string)
		cwd, _ := row["directory"].(string)
		if cwd == "" {
			cwd, _ = row["cwd"].(string)
		}
		tm := row["time"]
		if m, ok := tm.(map[string]any); ok {
			tm = m["updated"]
			if tm == nil {
				tm = m["updatedAt"]
			}
		}
		if tm == nil {
			tm = row["updatedAt"]
		}
		if tm == nil {
			tm = row["time_updated"]
		}
		s := protocol.Session{
			Engine:    engine,
			ID:        id,
			Title:     title,
			CWD:       cwd,
			Status:    "历史",
			UpdatedAt: driver.Epoch(tm),
		}
		outRows = append(outRows, driver.WithJSONLTitle(s))
	}
	return outRows
}

func extractJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	i := strings.IndexAny(raw, "[{")
	if i > 0 {
		raw = strings.TrimSpace(raw[i:])
	}
	return raw
}

func parseExport(raw, engine, session string) []protocol.Event {
	raw = extractJSON(raw)
	if raw == "" {
		return nil
	}
	var obj map[string]any
	if json.Unmarshal([]byte(raw), &obj) == nil {
		return flattenExport(obj, engine, session)
	}
	var arr []any
	if json.Unmarshal([]byte(raw), &arr) == nil {
		var evs []protocol.Event
		for _, item := range arr {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			evs = append(evs, driver.EventsFromJSONL(m, engine, session)...)
		}
		return evs
	}
	var evs []protocol.Event
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) != nil {
			continue
		}
		evs = append(evs, driver.EventsFromJSONL(m, engine, session)...)
	}
	return evs
}

func flattenExport(obj map[string]any, engine, session string) []protocol.Event {
	if msgs, ok := obj["messages"].([]any); ok {
		var evs []protocol.Event
		for _, item := range msgs {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			info, _ := m["info"].(map[string]any)
			role := ""
			if info != nil {
				role, _ = info["role"].(string)
			}
			if role == "" {
				role, _ = m["role"].(string)
			}
			parts, _ := m["parts"].([]any)
			if len(parts) == 0 {
				evs = append(evs, driver.EventsFromJSONL(m, engine, session)...)
				continue
			}
			for _, p := range parts {
				part, ok := p.(map[string]any)
				if !ok {
					continue
				}
				switch str := fmt.Sprint(part["type"]); str {
				case "text":
					text, _ := part["text"].(string)
					if text == "" {
						continue
					}
					typ := "assistant"
					if role == "user" {
						typ = "user"
					}
					evs = append(evs, protocol.Event{Type: typ, Role: role, Text: driver.FullText(text), Engine: engine, Session: session})
				case "tool":
					name, _ := part["tool"].(string)
					state, _ := part["state"].(map[string]any)
					input := any(nil)
					output := ""
					if state != nil {
						input = state["input"]
						output, _ = state["output"].(string)
					}
					evs = append(evs, protocol.Event{Type: "tool_call", Role: "assistant", Tool: name, Text: fmt.Sprint(input), Engine: engine, Session: session})
					if output != "" {
						evs = append(evs, protocol.Event{Type: "tool_result", Role: "tool", Tool: name, Text: output, Engine: engine, Session: session})
					}
				case "reasoning", "thinking":
					text, _ := part["text"].(string)
					if text != "" {
						evs = append(evs, protocol.Event{Type: "thinking", Role: "assistant", Text: driver.FullText(text), Engine: engine, Session: session})
					}
				}
			}
		}
		return evs
	}
	return driver.EventsFromJSONL(obj, engine, session)
}
