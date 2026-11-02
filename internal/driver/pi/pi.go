package pi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/protocol"
	"agent-mesh/internal/watchfile"
)

// Driver drives the local Pi coding agent CLI.
type Driver struct {
	root string
	bin  string
}

func findBin() string {
	home := driver.Home()
	extra := []string{filepath.Join(home, ".npm-global", "bin", "pi")}
	if runtime.GOOS != "windows" {
		extra = append(extra, "/usr/local/bin/pi")
	}
	return driver.LookCLI([]string{"PI_BIN"}, []string{"pi"}, extra)
}

// New creates the driver.
func New() *Driver {
	root := os.Getenv("PI_CODING_AGENT_DIR")
	if root == "" {
		root = filepath.Join(driver.Home(), ".pi", "agent")
	}
	return &Driver{root: root, bin: findBin()}
}

func (d *Driver) Name() string { return "pi" }

func (d *Driver) sessionRoot() string {
	if s := os.Getenv("PI_CODING_AGENT_SESSION_DIR"); s != "" {
		return s
	}
	return filepath.Join(d.root, "sessions")
}

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

func defaultModel() []protocol.Model {
	return []protocol.Model{{ID: "pi", Engine: "pi", Name: "default", Title: "default", Default: true}}
}

func parsePiModels(out string) []protocol.Model {
	rows := []protocol.Model{}
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "─") || strings.HasPrefix(line, "-") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		provider, name := fields[0], fields[1]
		if strings.EqualFold(provider, "provider") && strings.EqualFold(name, "model") {
			continue
		}
		id := name
		if provider != "" && !strings.Contains(name, "/") {
			id = provider + "/" + name
		}
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		rows = append(rows, protocol.Model{
			ID:     "pi/" + id,
			Engine: "pi",
			Name:   id,
			Title:  id,
		})
	}
	if len(rows) > 0 {
		rows[0].Default = true
	}
	return rows
}

func (d *Driver) Models(ctx context.Context) ([]protocol.Model, error) {
	out, err := driver.RunOutput(ctx, 12*time.Second, d.bin, "--list-models")
	if err != nil && out == "" {
		return defaultModel(), err
	}
	rows := parsePiModels(out)
	if len(rows) == 0 {
		return defaultModel(), nil
	}
	return rows, nil
}

func (d *Driver) List(ctx context.Context, all bool) ([]protocol.Session, error) {
	seen := map[string]protocol.Session{}
	root := d.sessionRoot()
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".jsonl") {
			return nil
		}
		id, cwd, title := sessionHeader(path)
		if id == "" {
			id = strings.TrimSuffix(info.Name(), ".jsonl")
			if i := strings.LastIndex(id, "_"); i >= 0 {
				id = id[i+1:]
			}
		}
		if _, ok := seen[id]; ok {
			return nil
		}
		s := protocol.Session{
			Engine:    d.Name(),
			ID:        id,
			Title:     title,
			CWD:       cwd,
			Status:    "history",
			JSONL:     path,
			UpdatedAt: info.ModTime().Unix(),
		}
		s.Busy = driver.Recent(path)
		seen[id] = driver.WithJSONLTitle(s)
		return nil
	})
	out := make([]protocol.Session, 0, len(seen))
	for _, s := range seen {
		if !all && !s.Live && !s.Busy {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func (d *Driver) findJSONL(sessionID string) string {
	var best string
	var bestM time.Time
	_ = filepath.Walk(d.sessionRoot(), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".jsonl") {
			return nil
		}
		id, _, _ := sessionHeader(path)
		if id == sessionID || strings.Contains(info.Name(), sessionID) {
			if info.ModTime().After(bestM) {
				best, bestM = path, info.ModTime()
			}
		}
		return nil
	})
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
	rows, err := watchfile.PollJSONL(ctx, path, true, 300*time.Millisecond)
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
	cwd := ""
	if p := d.findJSONL(sessionID); p != "" {
		_, cwd, _ = sessionHeader(p)
	}
	return d.run(ctx, cwd, text, sessionID, model)
}

func (d *Driver) Send(ctx context.Context, cwd, text, resume, model string) (<-chan protocol.Event, error) {
	return d.run(ctx, cwd, text, resume, model)
}

func (d *Driver) run(ctx context.Context, cwd, text, resume, model string) (<-chan protocol.Event, error) {
	if !driver.BinExists(d.bin) {
		return nil, fmt.Errorf("cannot find the pi binary (tried PATH and ~/.npm-global/bin/pi)")
	}
	args := []string{"-p", "--mode", "json"}
	if resume != "" {
		args = append(args, "--session", resume)
	} else if title := driver.Truncate(text, 80); title != "" {
		args = append(args, "--name", title)
	}
	args = driver.WithModelFlag(args, driver.CLIModel(model, d.Name()))
	args = append(args, "--", text)
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
		driver.StreamJSONL(stdout, d.Name(), resume, func(ev protocol.Event) {
			out <- ev
		})
		out <- protocol.Event{Type: "log", Engine: d.Name(), Session: resume, Done: true}
	}()
	return out, nil
}

func sessionHeader(path string) (id, cwd, title string) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", ""
	}
	defer f.Close()
	var line string
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	if n <= 0 {
		return "", "", ""
	}
	line, _, _ = strings.Cut(string(buf[:n]), "\n")
	var obj map[string]any
	if json.Unmarshal([]byte(line), &obj) != nil {
		return "", "", ""
	}
	id, _ = obj["id"].(string)
	cwd, _ = obj["cwd"].(string)
	title, _ = obj["name"].(string)
	return id, cwd, title
}
