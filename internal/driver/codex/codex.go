package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/protocol"
	"agent-mesh/internal/watchfile"
)

var rolloutSessionRe = regexp.MustCompile(`(?i)([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`)

// Driver drives the local Codex CLI.
type Driver struct {
	root string
	bin  string
}

// New creates the driver.
func New() *Driver {
	root := os.Getenv("CODEX_HOME")
	if root == "" {
		root = filepath.Join(driver.Home(), ".codex")
	}
	bin := driver.LookCLI([]string{"CODEX_BIN"}, []string{"codex"}, nil)
	return &Driver{root: root, bin: bin}
}

func (d *Driver) Name() string { return "codex" }

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
	catalog := d.catalogPath()
	raw, err := os.ReadFile(catalog)
	if err != nil {
		if cur == "" {
			return nil, err
		}
		return []protocol.Model{{ID: d.Name() + "/" + cur, Engine: d.Name(), Name: cur, Title: cur, Default: true}}, nil
	}
	var wrap struct {
		Models []struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
			Visibility  string `json:"visibility"`
		} `json:"models"`
	}
	if json.Unmarshal(raw, &wrap) != nil || len(wrap.Models) == 0 {
		var arr []struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
			Visibility  string `json:"visibility"`
		}
		if json.Unmarshal(raw, &arr) == nil {
			wrap.Models = arr
		}
	}
	var rows []protocol.Model
	for _, m := range wrap.Models {
		if m.Slug == "" || m.Visibility == "hide" {
			continue
		}
		title := m.DisplayName
		if title == "" {
			title = m.Slug
		}
		rows = append(rows, protocol.Model{
			ID:      d.Name() + "/" + m.Slug,
			Engine:  d.Name(),
			Name:    m.Slug,
			Title:   title,
			Default: m.Slug == cur,
		})
	}
	return rows, nil
}

func (d *Driver) catalogPath() string {
	cfg := filepath.Join(d.root, "config.toml")
	raw, err := os.ReadFile(cfg)
	if err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") {
				continue
			}
			if strings.HasPrefix(line, "model_catalog_json") {
				_, val, ok := strings.Cut(line, "=")
				if ok {
					return strings.Trim(strings.TrimSpace(val), `"`)
				}
			}
		}
	}
	for _, name := range []string{"magpie-models.json", "models_cache.json"} {
		p := filepath.Join(d.root, name)
		if driver.FileExists(p) {
			return p
		}
	}
	return filepath.Join(d.root, "models_cache.json")
}

func (d *Driver) configModel() string {
	raw, err := os.ReadFile(filepath.Join(d.root, "config.toml"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "model ") || strings.HasPrefix(line, "model=") {
			_, val, ok := strings.Cut(line, "=")
			if ok {
				return strings.Trim(strings.TrimSpace(val), `"`)
			}
		}
	}
	return ""
}

func (d *Driver) sessionDirs() []string {
	cands := []string{
		filepath.Join(d.root, "sessions"),
		filepath.Join(d.root, "archived_sessions"),
	}
	var out []string
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			out = append(out, c)
		}
	}
	return out
}

func (d *Driver) List(ctx context.Context, all bool) ([]protocol.Session, error) {
	seen := map[string]protocol.Session{}
	if d.bin != "" {
		cmd := driver.CommandContext(ctx, d.bin, "agents")
		if out, err := cmd.Output(); err == nil {
			var rows []map[string]any
			if json.Unmarshal(out, &rows) == nil {
				for _, row := range rows {
					id, _ := row["id"].(string)
					if id == "" {
						id, _ = row["sessionId"].(string)
					}
					if id == "" {
						continue
					}
					cwd, _ := row["cwd"].(string)
					title, _ := row["title"].(string)
					seen[id] = protocol.Session{Engine: d.Name(), ID: id, Title: title, CWD: cwd, Live: true, Status: "online"}
				}
			}
		}
	}
	if all || len(seen) == 0 {
		for _, dir := range d.sessionDirs() {
			_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				name := info.Name()
				if !strings.HasSuffix(name, ".jsonl") && name != "session.json" && name != "meta.json" {
					return nil
				}
				id, cwd := rolloutMeta(path)
				if id == "" {
					id = sessionIDFromName(name)
				}
				if id == "" {
					if strings.HasSuffix(name, ".jsonl") {
						id = strings.TrimSuffix(name, ".jsonl")
					} else {
						id = filepath.Base(filepath.Dir(path))
					}
				}
				if id == "" {
					return nil
				}
				if _, ok := seen[id]; ok {
					s := seen[id]
					s.JSONL = path
					s.UpdatedAt = info.ModTime().Unix()
					if s.CWD == "" {
						s.CWD = cwd
					}
					seen[id] = s
					return nil
				}
				if !all {
					return nil
				}
				seen[id] = protocol.Session{
					Engine:    d.Name(),
					ID:        id,
					CWD:       cwd,
					Status:    "history",
					JSONL:     path,
					UpdatedAt: info.ModTime().Unix(),
				}
				return nil
			})
		}
	}
	out := make([]protocol.Session, 0, len(seen))
	for _, s := range seen {
		if !all && !s.Live {
			continue
		}
		out = append(out, driver.WithJSONLTitle(s))
	}
	return out, nil
}

func sessionIDFromName(name string) string {
	stem := strings.TrimSuffix(name, ".jsonl")
	return rolloutSessionRe.FindString(stem)
}

func matchRollout(stem, id string) bool {
	if id == "" || stem == "" {
		return false
	}
	if stem == id || sessionIDFromName(stem) == id {
		return true
	}
	return strings.HasSuffix(stem, "-"+id)
}

func rolloutMeta(path string) (id, cwd string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !sc.Scan() {
		return "", ""
	}
	var obj map[string]any
	if json.Unmarshal(sc.Bytes(), &obj) != nil {
		return "", ""
	}
	payload, _ := obj["payload"].(map[string]any)
	if payload == nil {
		payload = obj
	}
	id, _ = payload["id"].(string)
	if id == "" {
		id, _ = payload["session_id"].(string)
	}
	cwd, _ = payload["cwd"].(string)
	return id, cwd
}

func (d *Driver) findJSONL(id string) string {
	if id == "" {
		return ""
	}
	var best string
	var bestM time.Time
	for _, dir := range d.sessionDirs() {
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".jsonl") {
				return nil
			}
			stem := strings.TrimSuffix(info.Name(), ".jsonl")
			if !matchRollout(stem, id) {
				return nil
			}
			if best == "" || info.ModTime().After(bestM) {
				best, bestM = path, info.ModTime()
			}
			return nil
		})
	}
	return best
}

func (d *Driver) History(ctx context.Context, sessionID string, tail int) ([]protocol.Event, error) {
	path := d.findJSONL(sessionID)
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
	path := d.findJSONL(sessionID)
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

func (d *Driver) Reply(ctx context.Context, sessionID, text, model string) (<-chan protocol.Event, error) {
	cmd := driver.CommandContext(ctx, d.bin, "queue", sessionID, text)
	if err := cmd.Run(); err == nil {
		return d.Watch(ctx, sessionID)
	}
	return d.runExec(ctx, "", text, sessionID, model)
}

func (d *Driver) Send(ctx context.Context, cwd, text, resume, model string) (<-chan protocol.Event, error) {
	return d.runExec(ctx, cwd, text, resume, model)
}

func (d *Driver) runExec(ctx context.Context, cwd, text, resume, model string) (<-chan protocol.Event, error) {
	args := []string{"exec", "--json"}
	args = driver.WithModelFlag(args, driver.CLIModel(model, d.Name()))
	if resume != "" {
		args = append(args, "resume", resume)
	}
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
		for sc.Scan() {
			line := sc.Text()
			var obj map[string]any
			if json.Unmarshal([]byte(line), &obj) == nil {
				parsed := driver.EventsFromJSONL(obj, d.Name(), resume)
				if len(parsed) > 0 {
					for _, ev := range parsed {
						out <- ev
					}
					continue
				}
			}
			out <- protocol.Event{Type: "log", Engine: d.Name(), Text: line}
		}
		out <- protocol.Event{Type: "log", Engine: d.Name(), Done: true}
	}()
	return out, nil
}
