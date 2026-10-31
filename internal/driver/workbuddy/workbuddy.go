package workbuddy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/protocol"
	"agent-mesh/internal/watchfile"
)

// Driver 接管本机 WorkBuddy CLI 网关。
type Driver struct {
	root string
}

// New 创建驱动。
func New() *Driver {
	root := os.Getenv("WORKBUDDY_CONFIG_DIR")
	if root == "" {
		root = filepath.Join(driver.Home(), ".workbuddy")
	}
	return &Driver{root: root}
}

func (d *Driver) Name() string { return "workbuddy" }

func (d *Driver) Probe(ctx context.Context) protocol.EngineInfo {
	info := protocol.EngineInfo{
		Name:   d.Name(),
		Title:  driver.TitleOf(d.Name()),
		Detail: d.root,
	}
	gws := d.discover()
	info.Available = len(gws) > 0 || driver.FileExists(filepath.Join(d.root, "models.json"))
	if len(gws) > 0 {
		info.Binary = gws[0].URL
		info.Version = fmt.Sprintf("gateway pid %d", gws[0].PID)
	}
	return info
}

func (d *Driver) Models(ctx context.Context) ([]protocol.Model, error) {
	raw, err := os.ReadFile(filepath.Join(d.root, "models.json"))
	if err != nil {
		return d.modelsFromGateway(ctx)
	}
	var items []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	var rows []protocol.Model
	for i, m := range items {
		if m.ID == "" {
			continue
		}
		title := m.Name
		if title == "" {
			title = m.ID
		}
		rows = append(rows, protocol.Model{
			ID:      d.Name() + "/" + m.ID,
			Engine:  d.Name(),
			Name:    m.ID,
			Title:   title,
			Default: i == 0,
		})
	}
	return rows, nil
}

func (d *Driver) modelsFromGateway(ctx context.Context) ([]protocol.Model, error) {
	gws := d.discover()
	if len(gws) == 0 {
		return nil, fmt.Errorf("no workbuddy models.json or live gateway")
	}
	gw := gws[0]
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gw.URL+"/api/v1/models", nil)
	if err != nil {
		return nil, err
	}
	if gw.Password != "" {
		req.Header.Set("Authorization", "Bearer "+gw.Password)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("models http %d: %s", resp.StatusCode, b)
	}
	var wrap struct {
		Data []struct {
			ID    string `json:"id"`
			Owned string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &wrap); err != nil {
		return nil, err
	}
	var rows []protocol.Model
	for _, m := range wrap.Data {
		if m.ID == "" {
			continue
		}
		rows = append(rows, protocol.Model{ID: d.Name() + "/" + m.ID, Engine: d.Name(), Name: m.ID, Title: m.ID})
	}
	return rows, nil
}

type gateway struct {
	PID      int
	URL      string
	Password string
	Session  string
	CWD      string
}

func (d *Driver) discover() []gateway {
	dir := filepath.Join(d.root, "sessions")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []gateway
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		obj, err := driver.ReadJSONFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		pid := int(driver.Epoch(obj["pid"]))
		url, _ := obj["url"].(string)
		if url == "" {
			url, _ = obj["endpoint"].(string)
		}
		if pid == 0 || url == "" || !driver.PIDAlive(pid) {
			continue
		}
		sid, _ := obj["sessionId"].(string)
		cwd, _ := obj["cwd"].(string)
		pass := os.Getenv("CODEBUDDY_GATEWAY_PASSWORD")
		if pass == "" {
			pass = os.Getenv("WORKBUDDY_GATEWAY_PASSWORD")
		}
		if env := readProcEnviron(pid); env["CODEBUDDY_GATEWAY_PASSWORD"] != "" {
			pass = env["CODEBUDDY_GATEWAY_PASSWORD"]
		}
		out = append(out, gateway{PID: pid, URL: strings.TrimRight(url, "/"), Password: pass, Session: sid, CWD: cwd})
	}
	return out
}

func readProcEnviron(pid int) map[string]string {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, part := range bytes.Split(raw, []byte{0}) {
		k, v, ok := strings.Cut(string(part), "=")
		if ok {
			out[k] = v
		}
	}
	return out
}

func (d *Driver) findJSONL(sessionID string) string {
	var best string
	var bestM time.Time
	_ = filepath.Walk(filepath.Join(d.root, "projects"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if info.Name() == sessionID+".jsonl" {
			if info.ModTime().After(bestM) {
				best, bestM = path, info.ModTime()
			}
		}
		return nil
	})
	return best
}

func (d *Driver) List(ctx context.Context, all bool) ([]protocol.Session, error) {
	gws := d.discover()
	seen := map[string]protocol.Session{}
	for _, gw := range gws {
		s := protocol.Session{
			Engine: d.Name(),
			ID:     gw.Session,
			CWD:    gw.CWD,
			Status: "在线",
			Kind:   "gateway",
			PID:    gw.PID,
			Live:   true,
			Extra:  gw.URL,
		}
		if s.ID == "" {
			s.ID = fmt.Sprintf("pid-%d", gw.PID)
		}
		jsonl := d.findJSONL(gw.Session)
		s.JSONL = jsonl
		s.Busy = driver.Recent(jsonl)
		s.UpdatedAt = driver.MtimeUnix(jsonl)
		seen[s.ID] = driver.WithJSONLTitle(s)
	}
	if all {
		_ = filepath.Walk(filepath.Join(d.root, "projects"), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".jsonl") {
				return nil
			}
			id := strings.TrimSuffix(info.Name(), ".jsonl")
			if _, ok := seen[id]; ok {
				return nil
			}
			seen[id] = driver.WithJSONLTitle(protocol.Session{
				Engine:    d.Name(),
				ID:        id,
				Status:    "历史",
				JSONL:     path,
				UpdatedAt: info.ModTime().Unix(),
			})
			return nil
		})
	}
	out := make([]protocol.Session, 0, len(seen))
	for _, s := range seen {
		if !all && !s.Live && !s.Busy {
			continue
		}
		out = append(out, s)
	}
	return out, nil
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

func (d *Driver) Reply(ctx context.Context, sessionID, text string) (<-chan protocol.Event, error) {
	gws := d.discover()
	if len(gws) == 0 {
		return nil, fmt.Errorf("no live workbuddy gateway")
	}
	gw := gws[0]
	if gw.Password == "" {
		return nil, fmt.Errorf("gateway password missing")
	}
	// 先盯 jsonl，避免 reply 往返期间丢掉实时行。
	ch, watchErr := d.Watch(ctx, sessionID)
	body := map[string]string{"text": text}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gw.URL+"/api/v1/sessions/"+sessionID+"/reply", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+gw.Password)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("live reply: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("live reply http %d: %s", resp.StatusCode, b)
	}
	if watchErr != nil {
		out := make(chan protocol.Event, 1)
		out <- protocol.Event{Type: "log", Text: "已通过 live reply 追加", Session: sessionID, Engine: d.Name(), Done: true}
		close(out)
		return out, nil
	}
	return ch, nil
}

func (d *Driver) Send(ctx context.Context, cwd, text, resume string) (<-chan protocol.Event, error) {
	if resume != "" {
		return d.Reply(ctx, resume, text)
	}
	gws := d.discover()
	if len(gws) == 0 {
		return nil, fmt.Errorf("no live workbuddy gateway")
	}
	gw := gws[0]
	if cwd == "" {
		cwd = gw.CWD
	}
	if cwd == "" {
		return nil, fmt.Errorf("请选择工作目录")
	}
	acp, err := newACP(ctx, gw)
	if err != nil {
		return nil, err
	}
	if _, err := acp.call(ctx, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientInfo":      map[string]any{"name": "agent-mesh", "version": "1.0.0"},
		"clientCapabilities": map[string]any{},
	}); err != nil {
		return nil, fmt.Errorf("acp initialize: %w", err)
	}
	res, err := acp.call(ctx, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}})
	if err != nil {
		return nil, fmt.Errorf("session/new: %w", err)
	}
	sid, _ := res["sessionId"].(string)
	if sid == "" {
		return nil, fmt.Errorf("session/new 未返回 sessionId")
	}
	out := make(chan protocol.Event, 256)
	go func() {
		defer close(out)
		out <- protocol.Event{Type: "session", Engine: d.Name(), Session: sid}
		wctx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() {
			_, err := acp.call(wctx, "session/prompt", map[string]any{
				"sessionId": sid,
				"prompt":    []any{map[string]any{"type": "text", "text": text}},
			})
			if err != nil && wctx.Err() == nil {
				out <- protocol.Event{Type: "log", Engine: d.Name(), Session: sid, Text: err.Error()}
			}
			time.Sleep(800 * time.Millisecond)
			cancel()
		}()
		var watch <-chan protocol.Event
		for i := 0; i < 50 && watch == nil; i++ {
			ch, werr := d.Watch(wctx, sid)
			if werr == nil {
				watch = ch
				break
			}
			select {
			case <-wctx.Done():
				out <- protocol.Event{Type: "log", Engine: d.Name(), Session: sid, Text: "已新建会话", Done: true}
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
		if watch == nil {
			out <- protocol.Event{Type: "log", Engine: d.Name(), Session: sid, Text: "已新建会话", Done: true}
			return
		}
		for ev := range watch {
			out <- ev
		}
	}()
	return out, nil
}
