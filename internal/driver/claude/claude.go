package claude

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/protocol"
	"agent-mesh/internal/watchfile"
)

// Driver 接管本机 Claude Code。
type Driver struct {
	root string
	bin  string
}

// New 创建驱动。
func New() *Driver {
	root := os.Getenv("CLAUDE_CONFIG_DIR")
	if root == "" {
		root = filepath.Join(driver.Home(), ".claude")
	}
	bin := os.Getenv("CLAUDE_BIN")
	if bin == "" {
		bin, _ = exec.LookPath("claude")
	}
	if bin == "" {
		bin = filepath.Join(driver.Home(), ".local", "bin", "claude")
	}
	return &Driver{root: root, bin: bin}
}

func (d *Driver) Name() string { return "claude" }

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
	settings := filepath.Join(d.root, "settings.json")
	obj, err := driver.ReadJSONFile(settings)
	if err != nil {
		return nil, err
	}
	cur, _ := obj["model"].(string)
	picker, _ := obj["modelPicker"].(map[string]any)
	opts, _ := picker["options"].([]any)
	var rows []protocol.Model
	seen := map[string]bool{}
	for _, raw := range opts {
		item, _ := raw.(map[string]any)
		if item == nil {
			continue
		}
		name, _ := item["model"].(string)
		if name == "" {
			continue
		}
		title, _ := item["label"].(string)
		rows = append(rows, protocol.Model{
			ID:      d.Name() + "/" + name,
			Engine:  d.Name(),
			Name:    name,
			Title:   title,
			Default: name == cur,
		})
		seen[name] = true
	}
	if cur != "" && !seen[cur] {
		rows = append([]protocol.Model{{
			ID: d.Name() + "/" + cur, Engine: d.Name(), Name: cur, Title: cur, Default: true,
		}}, rows...)
	}
	return rows, nil
}

func encodeProject(cwd string) string {
	return strings.ReplaceAll(cwd, "/", "-")
}

func (d *Driver) findJSONL(id, cwd string) string {
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
		if info.Name() == id+".jsonl" && !strings.Contains(path, "/subagents/") {
			best = path
		}
		return nil
	})
	return best
}

func (d *Driver) List(ctx context.Context, all bool) ([]protocol.Session, error) {
	var rows []map[string]any
	if d.bin != "" {
		cmd := exec.CommandContext(ctx, d.bin, "agents", "--json")
		out, err := cmd.Output()
		if err == nil {
			_ = json.Unmarshal(out, &rows)
		}
	}
	seen := map[string]protocol.Session{}
	for _, row := range rows {
		id, _ := row["sessionId"].(string)
		if id == "" {
			continue
		}
		cwd, _ := row["cwd"].(string)
		name, _ := row["name"].(string)
		status, _ := row["status"].(string)
		pid := int(driver.Epoch(row["pid"]))
		sock, _ := row["messagingSocketPath"].(string)
		s := protocol.Session{
			Engine: d.Name(),
			ID:     id,
			Title:  name,
			CWD:    cwd,
			Status: status,
			Kind:   "interactive",
			PID:    pid,
			Live:   driver.PIDAlive(pid),
			JSONL:  d.findJSONL(id, cwd),
			Extra:  sock,
		}
		s.Busy = s.Status == "busy" || driver.Recent(s.JSONL)
		seen[id] = s
	}
	if all {
		_ = filepath.Walk(filepath.Join(d.root, "projects"), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".jsonl") {
				return nil
			}
			if strings.Contains(path, "/subagents/") {
				return nil
			}
			id := strings.TrimSuffix(info.Name(), ".jsonl")
			if _, ok := seen[id]; ok {
				return nil
			}
			s := protocol.Session{Engine: d.Name(), ID: id, Status: "历史", JSONL: path, UpdatedAt: info.ModTime().Unix()}
			seen[id] = driver.WithJSONLTitle(s)
			return nil
		})
	}
	out := make([]protocol.Session, 0, len(seen))
	for _, s := range seen {
		if !all && !s.Live && !s.Busy {
			continue
		}
		if s.JSONL == "" {
			s.JSONL = d.findJSONL(s.ID, s.CWD)
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

func (d *Driver) lookup(ctx context.Context, sessionID string) (protocol.Session, error) {
	rows, err := d.List(ctx, true)
	if err != nil {
		return protocol.Session{}, err
	}
	var hits []protocol.Session
	for _, s := range rows {
		if s.ID == sessionID || strings.HasPrefix(s.ID, sessionID) {
			hits = append(hits, s)
		}
	}
	if len(hits) != 1 {
		return protocol.Session{}, fmt.Errorf("session %s not unique or missing", sessionID)
	}
	return hits[0], nil
}

func (d *Driver) peerToken(pid int, sock string) string {
	dir := filepath.Join(d.root, "sessions")
	if sock != "" {
		sum := sha256.Sum256([]byte(sock))
		p := filepath.Join(dir, fmt.Sprintf("%d.%s.key", pid, hex.EncodeToString(sum[:])))
		if obj, err := driver.ReadJSONFile(p); err == nil {
			if t, ok := obj["peerToken"].(string); ok {
				return t
			}
		}
	}
	matches, _ := filepath.Glob(filepath.Join(dir, fmt.Sprintf("%d.*.key", pid)))
	for _, p := range matches {
		obj, err := driver.ReadJSONFile(p)
		if err != nil {
			continue
		}
		if t, ok := obj["peerToken"].(string); ok && t != "" {
			return t
		}
	}
	return ""
}

func (d *Driver) inject(sess protocol.Session, text string) error {
	if sess.Extra == "" || sess.PID == 0 {
		return fmt.Errorf("no live socket")
	}
	token := d.peerToken(sess.PID, sess.Extra)
	if token == "" {
		return fmt.Errorf("peer token missing")
	}
	c, err := net.DialTimeout("unix", sess.Extra, 5*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	msgs := []map[string]any{
		{"type": "auth", "token": token},
		{"type": "user", "message": map[string]any{"role": "user", "content": text}, "priority": "next"},
	}
	enc := json.NewEncoder(c)
	for _, m := range msgs {
		if err := enc.Encode(m); err != nil {
			return err
		}
	}
	return nil
}

func (d *Driver) Reply(ctx context.Context, sessionID, text string) (<-chan protocol.Event, error) {
	sess, err := d.lookup(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess.Live {
		if err := d.inject(sess, text); err == nil {
			return d.Watch(ctx, sess.ID)
		}
	}
	return d.runPrint(ctx, sess.CWD, text, sess.ID)
}

func (d *Driver) Send(ctx context.Context, cwd, text, resume string) (<-chan protocol.Event, error) {
	return d.runPrint(ctx, cwd, text, resume)
}

func (d *Driver) runPrint(ctx context.Context, cwd, text, resume string) (<-chan protocol.Event, error) {
	args := []string{"-p", "--output-format", "stream-json", "--include-partial-messages", "--verbose"}
	if resume != "" {
		args = append(args, "--resume", resume)
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
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
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
			if obj["type"] == "stream_event" {
				if ev, ok := obj["event"].(map[string]any); ok {
					if delta, ok := ev["delta"].(map[string]any); ok {
						if t, ok := delta["text"].(string); ok && t != "" {
							out <- protocol.Event{Type: "delta", Role: "assistant", Engine: d.Name(), Text: t}
						}
					}
				}
				continue
			}
			parsed := driver.EventsFromJSONL(obj, d.Name(), resume)
			if len(parsed) == 0 {
				continue
			}
			for _, ev := range parsed {
				out <- ev
			}
		}
		out <- protocol.Event{Type: "log", Engine: d.Name(), Done: true}
	}()
	return out, nil
}

// skipPrintDiagnostic 丢掉 Claude Code print 模式写到 stderr 的诊断行。
// 例如 [claude-code:unrecognized_model] {"model":"group/never-stuck[1m]","query_source":"sdk"}
func skipPrintDiagnostic(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "[claude-code:")
}
