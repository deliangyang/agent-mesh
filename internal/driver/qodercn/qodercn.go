package qodercn

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/protocol"
	"agent-mesh/internal/watchfile"
)

const engineName = "qoder-cn"

// Driver drives the local Qoder CLI CN.
type Driver struct {
	root string
	bin  string
}

func findBin() string {
	return driver.LookCLI([]string{"QODERCN_BIN", "QODERCN_PATH"}, []string{"qodercn", "qoderclicn"}, nil)
}

// New creates the driver.
func New() *Driver {
	root := os.Getenv("QODERCN_CONFIG_DIR")
	if root == "" {
		root = filepath.Join(driver.Home(), ".qoder-cn")
	}
	return &Driver{root: root, bin: findBin()}
}

func (d *Driver) Name() string { return engineName }

func (d *Driver) Probe(ctx context.Context) protocol.EngineInfo {
	info := protocol.EngineInfo{
		Name:   d.Name(),
		Title:  driver.TitleOf(d.Name()),
		Binary: d.bin,
		Detail: d.root,
		Caps:   driver.DefaultCaps(d.Name()),
	}
	info.Available = driver.BinExists(d.bin)
	if info.Available {
		info.Version = driver.BinVersion(ctx, d.bin)
	}
	return info
}

func (d *Driver) Models(ctx context.Context) ([]protocol.Model, error) {
	cur := d.configModel()
	out, err := driver.RunOutput(ctx, 12*time.Second, d.bin, "--list-models")
	rows := parseQoderModels(d.Name(), out, cur)
	if len(rows) == 0 {
		if err != nil && out == "" && cur == "" {
			return defaultModel(d.Name()), err
		}
		return defaultModel(d.Name()), nil
	}
	return rows, nil
}

func (d *Driver) configModel() string {
	obj, err := driver.ReadJSONFile(filepath.Join(d.root, "settings.json"))
	if err != nil {
		return ""
	}
	s, _ := obj["model"].(string)
	return strings.TrimSpace(s)
}

func encodeProject(cwd string) string {
	return driver.EncodeProject(cwd)
}

func (d *Driver) findJSONL(id, cwd string) string {
	if id == "" {
		return ""
	}
	if cwd != "" {
		p := filepath.Join(d.root, "projects", encodeProject(cwd), id+".jsonl")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	var best string
	_ = filepath.Walk(filepath.Join(d.root, "projects"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if info.Name() == id+".jsonl" && !driver.PathHasDir(path, "subagents") {
			best = path
		}
		return nil
	})
	return best
}

func (d *Driver) List(ctx context.Context, all bool) ([]protocol.Session, error) {
	seen := map[string]protocol.Session{}
	_ = filepath.Walk(filepath.Join(d.root, "projects"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".jsonl") {
			return nil
		}
		if driver.PathHasDir(path, "subagents") {
			return nil
		}
		id := strings.TrimSuffix(info.Name(), ".jsonl")
		if _, ok := seen[id]; ok {
			return nil
		}
		s := protocol.Session{
			Engine:    d.Name(),
			ID:        id,
			Status:    "history",
			JSONL:     path,
			UpdatedAt: info.ModTime().Unix(),
		}
		s.Busy = driver.Recent(path)
		if s.Busy {
			s.Status = "online"
		}
		cwd, title := sessionMeta(path)
		s.CWD, s.Title = cwd, title
		seen[id] = s
		return nil
	})
	out := make([]protocol.Session, 0, len(seen))
	for _, s := range seen {
		if !all && !s.Live && !s.Busy {
			continue
		}
		out = append(out, driver.WithJSONLTitle(s))
	}
	return out, nil
}

func (d *Driver) History(ctx context.Context, sessionID string, tail int) ([]protocol.Event, error) {
	path := d.findJSONL(sessionID, "")
	if path == "" {
		return nil, fmt.Errorf("session %s has no jsonl", sessionID)
	}
	var evs []protocol.Event
	err := watchfile.TailJSONL(path, tail, func(obj map[string]any) {
		evs = append(evs, driver.EventsFromJSONL(obj, d.Name(), sessionID)...)
	})
	return evs, err
}

func (d *Driver) Watch(ctx context.Context, sessionID string) (<-chan protocol.Event, error) {
	path := d.findJSONL(sessionID, "")
	if path == "" {
		return nil, fmt.Errorf("session %s has no jsonl", sessionID)
	}
	rows, err := watchfile.PollJSONL(ctx, path, true, 100*time.Millisecond)
	if err != nil {
		return nil, err
	}
	out := make(chan protocol.Event, 32)
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

func (d *Driver) lookup(ctx context.Context, sessionID string) (protocol.Session, error) {
	rows, err := d.List(ctx, true)
	if err != nil {
		return protocol.Session{}, err
	}
	sess, err := driver.MatchSession(rows, sessionID)
	if err == nil {
		return sess, nil
	}
	jsonl := d.findJSONL(sessionID, "")
	if jsonl == "" {
		return protocol.Session{}, err
	}
	cwd, _ := sessionMeta(jsonl)
	return protocol.Session{Engine: d.Name(), ID: sessionID, CWD: cwd, JSONL: jsonl, Status: "history"}, nil
}

func (d *Driver) Reply(ctx context.Context, sessionID, text, model string) (<-chan protocol.Event, error) {
	sess, err := d.lookup(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return d.runPrint(ctx, sess.CWD, text, sess.ID, model)
}

func (d *Driver) Send(ctx context.Context, cwd, text, resume, model string) (<-chan protocol.Event, error) {
	return d.runPrint(ctx, cwd, text, resume, model)
}

func (d *Driver) runPrint(ctx context.Context, cwd, text, resume, model string) (<-chan protocol.Event, error) {
	args := []string{"-p", "--output-format", "stream-json", "--permission-mode", "auto"}
	if resume != "" {
		args = append(args, "--resume", resume)
	} else if title := driver.Truncate(text, 80); title != "" {
		args = append(args, "--name", title)
	}
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	args = driver.WithModelFlag(args, driver.CLIModel(model, d.Name()))
	args = append(args, text)
	cmd := driver.CommandContext(ctx, d.bin, args...)
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
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		sid := resume
		for sc.Scan() {
			line := sc.Text()
			if skipPrintDiagnostic(line) {
				continue
			}
			var obj map[string]any
			if json.Unmarshal([]byte(line), &obj) != nil {
				out <- protocol.Event{Type: "log", Engine: d.Name(), Text: line}
				continue
			}
			if sid == "" {
				if s, _ := obj["session_id"].(string); s != "" {
					sid = s
				}
			}
			if obj["type"] == "stream_event" {
				if ev := streamDelta(obj, d.Name(), sid); ev != nil {
					out <- *ev
				}
				continue
			}
			parsed := driver.EventsFromJSONL(obj, d.Name(), sid)
			if len(parsed) == 0 {
				continue
			}
			for _, ev := range parsed {
				if ev.Session != "" {
					sid = ev.Session
				}
				out <- ev
			}
		}
		out <- protocol.Event{Type: "log", Engine: d.Name(), Session: sid, Done: true}
	}()
	return out, nil
}

func streamDelta(obj map[string]any, engine, sid string) *protocol.Event {
	ev, _ := obj["event"].(map[string]any)
	if ev == nil {
		return nil
	}
	delta, _ := ev["delta"].(map[string]any)
	if delta == nil {
		return nil
	}
	if t, _ := delta["text"].(string); t != "" {
		return &protocol.Event{Type: "delta", Role: "assistant", Engine: engine, Session: sid, Text: t}
	}
	if t, _ := delta["thinking"].(string); t != "" {
		return &protocol.Event{Type: "thinking", Role: "assistant", Engine: engine, Session: sid, Text: t}
	}
	return nil
}

func skipPrintDiagnostic(line string) bool {
	s := strings.TrimSpace(line)
	if s == "" {
		return true
	}
	low := strings.ToLower(s)
	return strings.HasPrefix(low, "[qoder") || strings.HasPrefix(low, "[qodercn")
}

func sessionMeta(path string) (cwd, title string) {
	if path == "" {
		return "", ""
	}
	state := filepath.Join(strings.TrimSuffix(path, filepath.Ext(path)), "state.json")
	if obj, err := driver.ReadJSONFile(state); err == nil {
		cwd, _ = obj["cwd"].(string)
		title, _ = obj["name"].(string)
		if title == "" {
			title, _ = obj["title"].(string)
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return cwd, title
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	n := 0
	for sc.Scan() && n < 40 {
		n++
		var obj map[string]any
		if json.Unmarshal(sc.Bytes(), &obj) != nil {
			continue
		}
		if cwd == "" {
			if s, _ := obj["cwd"].(string); s != "" {
				cwd = s
			}
		}
		if title == "" {
			if s, _ := obj["title"].(string); s != "" {
				title = s
			}
		}
	}
	return cwd, title
}

func defaultModel(engine string) []protocol.Model {
	return []protocol.Model{{
		ID: engine + "/auto", Engine: engine, Name: "auto", Title: "Auto", Default: true,
	}}
}

func parseQoderModels(engine, out, current string) []protocol.Model {
	rows := []protocol.Model{}
	seen := map[string]bool{}
	add := func(name, title string, def bool) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		if title == "" {
			title = name
		}
		if !def {
			low := strings.ToLower(title)
			def = name == current || strings.Contains(low, "default") || strings.Contains(low, "current")
		}
		rows = append(rows, protocol.Model{
			ID:      engine + "/" + name,
			Engine:  engine,
			Name:    name,
			Title:   title,
			Default: def,
		})
	}
	if !parseQoderModelsJSON(out, add) {
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "─") {
				continue
			}
			if strings.HasSuffix(line, ":") || strings.EqualFold(line, "available models") {
				continue
			}
			name, title, ok := strings.Cut(line, " - ")
			if ok {
				add(strings.TrimSpace(name), strings.TrimSpace(title), false)
				continue
			}
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			add(fields[0], strings.TrimSpace(strings.TrimPrefix(line, fields[0])), false)
		}
	}
	if current != "" {
		add(current, current, true)
	}
	hasDef := false
	for i := range rows {
		if rows[i].Default {
			hasDef = true
			break
		}
	}
	if !hasDef && len(rows) > 0 {
		rows[0].Default = true
	}
	return rows
}

func parseQoderModelsJSON(out string, add func(name, title string, def bool)) bool {
	raw := strings.TrimSpace(out)
	if raw == "" || (raw[0] != '[' && raw[0] != '{') {
		return false
	}
	var names []string
	if json.Unmarshal([]byte(raw), &names) == nil && len(names) > 0 {
		for _, n := range names {
			add(n, n, false)
		}
		return true
	}
	var objs []map[string]any
	if json.Unmarshal([]byte(raw), &objs) == nil && len(objs) > 0 {
		for _, o := range objs {
			add(modelNameOf(o), modelTitleOf(o), false)
		}
		return true
	}
	var wrap struct {
		Models []map[string]any `json:"models"`
	}
	if json.Unmarshal([]byte(raw), &wrap) == nil && len(wrap.Models) > 0 {
		for _, o := range wrap.Models {
			add(modelNameOf(o), modelTitleOf(o), false)
		}
		return true
	}
	return false
}

func modelNameOf(o map[string]any) string {
	for _, k := range []string{"name", "id", "model", "slug"} {
		if s, _ := o[k].(string); strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func modelTitleOf(o map[string]any) string {
	for _, k := range []string{"title", "label", "display_name", "displayName"} {
		if s, _ := o[k].(string); strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
