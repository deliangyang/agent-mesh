package cursor

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/protocol"
	"agent-mesh/internal/watchfile"
)

var convIDRe = regexp.MustCompile(`conversationId"\s*:\s*"([0-9a-f-]{36})"`)
var uuidRe = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Driver 接管本机 Cursor Agent CLI。
type Driver struct {
	config string
	data   string
	bin    string
}

// New 创建驱动。
func New() *Driver {
	cfg := os.Getenv("CURSOR_CONFIG_DIR")
	if cfg == "" {
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			cfg = filepath.Join(xdg, "cursor")
		} else {
			cfg = filepath.Join(driver.Home(), ".cursor")
		}
	}
	data := os.Getenv("CURSOR_DATA_DIR")
	if data == "" {
		data = filepath.Join(driver.Home(), ".cursor")
	}
	bin := os.Getenv("CURSOR_AGENT_BIN")
	if bin == "" {
		bin, _ = exec.LookPath("agent")
	}
	if bin == "" {
		bin = filepath.Join(driver.Home(), ".local", "bin", "agent")
	}
	return &Driver{config: cfg, data: data, bin: bin}
}

func (d *Driver) Name() string { return "cursor" }

func (d *Driver) Probe(ctx context.Context) protocol.EngineInfo {
	info := protocol.EngineInfo{
		Name:   d.Name(),
		Title:  driver.TitleOf(d.Name()),
		Binary: d.bin,
		Detail: d.config,
	}
	info.Available = driver.FileExists(d.bin)
	if info.Available {
		info.Version = driver.BinVersion(ctx, d.bin)
	}
	return info
}

func (d *Driver) Models(ctx context.Context) ([]protocol.Model, error) {
	out, err := driver.RunOutput(ctx, 8*time.Second, d.bin, "--list-models")
	if err != nil && out == "" {
		return nil, err
	}
	var rows []protocol.Model
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(strings.ToLower(line), "available") {
			continue
		}
		name, title, ok := strings.Cut(line, " - ")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		title = strings.TrimSpace(title)
		if name == "" {
			continue
		}
		def := strings.Contains(strings.ToLower(title), "default") || strings.Contains(strings.ToLower(title), "current")
		rows = append(rows, protocol.Model{
			ID:      d.Name() + "/" + name,
			Engine:  d.Name(),
			Name:    name,
			Title:   title,
			Default: def,
		})
	}
	return rows, nil
}

func encodeProject(cwd string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	s := re.ReplaceAllString(cwd, "-")
	return strings.Trim(s, "-")
}

func (d *Driver) findJSONL(id, cwd string) string {
	if cwd != "" {
		p := filepath.Join(d.data, "projects", encodeProject(cwd), "agent-transcripts", id, id+".jsonl")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	matches, _ := filepath.Glob(filepath.Join(d.data, "projects", "*", "agent-transcripts", id, id+".jsonl"))
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

func (d *Driver) List(ctx context.Context, all bool) ([]protocol.Session, error) {
	seen := map[string]protocol.Session{}
	procDir, err := os.ReadDir("/proc")
	if err == nil {
		for _, e := range procDir {
			if !e.IsDir() {
				continue
			}
			pid := 0
			fmt.Sscanf(e.Name(), "%d", &pid)
			if pid <= 0 {
				continue
			}
			cmdb, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
			if err != nil {
				continue
			}
			cmd := strings.ReplaceAll(string(cmdb), "\x00", " ")
			if !strings.Contains(cmd, "share/cursor-agent") || !strings.Contains(cmd, "index.js") {
				continue
			}
			cwd, _ := os.Readlink(filepath.Join("/proc", e.Name(), "cwd"))
			store, logp := openStoreAndLog(pid)
			sid := ""
			if store != "" {
				sid = filepath.Base(filepath.Dir(store))
			}
			if sid == "" && logp != "" {
				raw, _ := os.ReadFile(logp)
				if m := convIDRe.FindSubmatch(raw); len(m) == 2 {
					sid = string(m[1])
				}
			}
			if sid == "" {
				sid = fmt.Sprintf("pid-%d", pid)
			}
			meta := map[string]any{}
			if store != "" {
				meta, _ = driver.ReadJSONFile(filepath.Join(filepath.Dir(store), "meta.json"))
			}
			title, _ := meta["title"].(string)
			if cwd == "" {
				cwd, _ = meta["cwd"].(string)
			}
			jsonl := d.findJSONL(sid, cwd)
			seen[sid] = protocol.Session{
				Engine:    d.Name(),
				ID:        sid,
				Title:     title,
				CWD:       cwd,
				Status:    "在线",
				Kind:      "interactive",
				PID:       pid,
				Live:      true,
				Busy:      driver.Recent(jsonl),
				JSONL:     jsonl,
				UpdatedAt: driver.MtimeUnix(jsonl),
				Extra:     store,
			}
		}
	}
	if all {
		chats := filepath.Join(d.config, "chats")
		_ = filepath.Walk(chats, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || info.Name() != "meta.json" {
				return nil
			}
			id := filepath.Base(filepath.Dir(path))
			if !uuidRe.MatchString(id) {
				return nil
			}
			if _, ok := seen[id]; ok {
				return nil
			}
			obj, err := driver.ReadJSONFile(path)
			if err != nil {
				return nil
			}
			title, _ := obj["title"].(string)
			cwd, _ := obj["cwd"].(string)
			seen[id] = protocol.Session{
				Engine:    d.Name(),
				ID:        id,
				Title:     title,
				CWD:       cwd,
				Status:    "历史",
				JSONL:     d.findJSONL(id, cwd),
				UpdatedAt: driver.Epoch(obj["updatedAtMs"]),
			}
			return nil
		})
		matches, _ := filepath.Glob(filepath.Join(d.data, "projects", "*", "agent-transcripts", "*", "*.jsonl"))
		for _, p := range matches {
			id := filepath.Base(filepath.Dir(p))
			if !uuidRe.MatchString(id) {
				continue
			}
			if cur, ok := seen[id]; ok {
				if cur.JSONL == "" {
					cur.JSONL = p
					seen[id] = cur
				}
				continue
			}
			seen[id] = protocol.Session{
				Engine:    d.Name(),
				ID:        id,
				Status:    "历史",
				JSONL:     p,
				UpdatedAt: driver.MtimeUnix(p),
			}
		}
	}
	out := make([]protocol.Session, 0, len(seen))
	for _, s := range seen {
		if !all && !s.Live && !s.Busy {
			continue
		}
		if s.Title == "" {
			s = driver.WithJSONLTitle(s)
		}
		out = append(out, s)
	}
	return out, nil
}

func openStoreAndLog(pid int) (store, logp string) {
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	ents, err := os.ReadDir(fdDir)
	if err != nil {
		return "", ""
	}
	for _, e := range ents {
		t, err := os.Readlink(filepath.Join(fdDir, e.Name()))
		if err != nil {
			continue
		}
		if strings.HasSuffix(t, "/store.db") && store == "" {
			store = t
		}
		if strings.Contains(t, "cursor-agent-logs-") && strings.HasSuffix(t, ".log") {
			logp = t
		}
	}
	return store, logp
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

func (d *Driver) lookup(ctx context.Context, id string) (protocol.Session, error) {
	rows, err := d.List(ctx, true)
	if err != nil {
		return protocol.Session{}, err
	}
	sess, err := driver.MatchSession(rows, id)
	if err == nil {
		return sess, nil
	}
	jsonl := d.findJSONL(id, "")
	if jsonl == "" {
		return protocol.Session{}, err
	}
	return protocol.Session{Engine: d.Name(), ID: id, JSONL: jsonl, Status: "历史"}, nil
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
	args := []string{"-p", "--output-format", "stream-json", "--stream-partial-output", "--trust", "--approve-mcps"}
	if resume != "" {
		args = append(args, "--resume", resume)
	}
	if cwd != "" {
		args = append(args, "--workspace", cwd)
	}
	args = driver.WithModelFlag(args, driver.CLIModel(model, d.Name()))
	cmd := exec.CommandContext(ctx, d.bin, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		fmt.Fprintln(stdin, text)
		stdin.Close()
	}()
	out := make(chan protocol.Event, 32)
	go func() {
		defer close(out)
		defer cmd.Wait()
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		sawPartial := false
		sid := resume
		for sc.Scan() {
			line := sc.Text()
			if skipCursorDiagnostic(line) {
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
			kind, _ := obj["type"].(string)
			if kind == "result" {
				out <- protocol.Event{Type: "log", Engine: d.Name(), Session: sid, Done: true}
				continue
			}
			if kind == "assistant" {
				_, hasTS := obj["timestamp_ms"]
				_, hasCall := obj["model_call_id"]
				if hasTS && !hasCall {
					sawPartial = true
				} else if sawPartial && (hasCall || !hasTS) {
					continue
				}
			}
			parsed := driver.EventsFromJSONL(obj, d.Name(), sid)
			if len(parsed) == 0 {
				continue
			}
			for _, ev := range parsed {
				if ev.Session != "" {
					sid = ev.Session
				}
				if ev.Type == "assistant" && kind == "assistant" {
					ev.Type = "delta"
				}
				out <- ev
			}
		}
		out <- protocol.Event{Type: "log", Engine: d.Name(), Session: sid, Done: true}
	}()
	return out, nil
}

func skipCursorDiagnostic(line string) bool {
	s := strings.TrimSpace(line)
	if s == "" {
		return true
	}
	return strings.HasPrefix(s, "Tool {") || strings.HasPrefix(s, "Tool{")
}

// CWDHash 与 Cursor CLI 一致的目录 md5。
func CWDHash(cwd string) string {
	sum := md5.Sum([]byte(cwd))
	return hex.EncodeToString(sum[:])
}
