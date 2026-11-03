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
	"sync"
	"time"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/protocol"
	"agent-mesh/internal/watchfile"
)

// Driver drives the local WorkBuddy CLI gateway.
type Driver struct {
	root  string
	mu    sync.Mutex
	serve *meshServe
}

// New creates the driver.
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
		Caps:   driver.DefaultCaps(d.Name()),
	}
	gws := d.discover()
	info.Available = len(gws) > 0 ||
		len(d.modelsFromFile()) > 0 ||
		len(d.modelsFromProductConfig()) > 0 ||
		driver.FileExists(filepath.Join(d.root, "cache", "acc-product-config-v3.json"))
	if len(gws) > 0 {
		info.Binary = gws[0].URL
		info.Version = fmt.Sprintf("gateway pid %d", gws[0].PID)
	}
	return info
}

func (d *Driver) Models(ctx context.Context) ([]protocol.Model, error) {
	if rows := d.modelsFromFile(); len(rows) > 0 {
		return rows, nil
	}
	if rows := d.modelsFromProductConfig(); len(rows) > 0 {
		return rows, nil
	}
	rows, err := d.modelsFromGateway(ctx)
	if len(rows) > 0 {
		return rows, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("no workbuddy models.json, product config, or live gateway")
}

func (d *Driver) modelsFromFile() []protocol.Model {
	raw, err := os.ReadFile(filepath.Join(d.root, "models.json"))
	if err != nil {
		return nil
	}
	return parseWorkBuddyModels(d.Name(), raw)
}

func (d *Driver) modelsFromProductConfig() []protocol.Model {
	for _, name := range []string{
		filepath.Join("cache", "acc-product-config-v3.json"),
		filepath.Join("cache", "acc-product-config.json"),
	} {
		raw, err := os.ReadFile(filepath.Join(d.root, name))
		if err != nil {
			continue
		}
		if rows := parseWorkBuddyProductModels(d.Name(), raw); len(rows) > 0 {
			return rows
		}
	}
	return nil
}

func parseWorkBuddyProductModels(engine string, raw []byte) []protocol.Model {
	var wrap struct {
		Models []map[string]any `json:"models"`
		Agents []struct {
			Models json.RawMessage `json:"models"`
		} `json:"agents"`
	}
	if json.Unmarshal(raw, &wrap) != nil || len(wrap.Models) == 0 {
		return nil
	}
	byID := map[string]map[string]any{}
	for _, obj := range wrap.Models {
		id, _ := obj["id"].(string)
		if id != "" {
			byID[id] = obj
		}
	}
	src := wrap.Models
	for _, ag := range wrap.Agents {
		ids := workBuddyModelIDs(ag.Models)
		if len(ids) == 0 {
			continue
		}
		var filtered []map[string]any
		for _, id := range ids {
			if obj, ok := byID[id]; ok {
				filtered = append(filtered, obj)
			}
		}
		if len(filtered) > 0 {
			src = filtered
			break
		}
	}
	var rows []protocol.Model
	for _, obj := range src {
		id, _ := obj["id"].(string)
		if id == "" {
			continue
		}
		title, _ := obj["name"].(string)
		if title == "" {
			title, _ = obj["title"].(string)
		}
		if title == "" {
			title, _ = obj["displayName"].(string)
		}
		if title == "" {
			title = id
		}
		def, _ := obj["isDefault"].(bool)
		rows = append(rows, protocol.Model{
			ID:      engine + "/" + id,
			Engine:  engine,
			Name:    id,
			Title:   title,
			Default: def,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	hasDef := false
	for _, r := range rows {
		if r.Default {
			hasDef = true
			break
		}
	}
	if !hasDef {
		rows[0].Default = true
	}
	return rows
}

func workBuddyModelIDs(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var ids []string
	if json.Unmarshal(raw, &ids) == nil {
		return ids
	}
	var objs []map[string]any
	if json.Unmarshal(raw, &objs) != nil {
		return nil
	}
	for _, obj := range objs {
		if id, _ := obj["id"].(string); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func parseWorkBuddyModels(engine string, raw []byte) []protocol.Model {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "[]" || string(raw) == "{}" {
		return nil
	}
	type item struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Title string `json:"title"`
		Owned string `json:"owned_by"`
	}
	add := func(rows *[]protocol.Model, m item, i int) {
		if m.ID == "" {
			return
		}
		title := m.Title
		if title == "" {
			title = m.Name
		}
		if title == "" {
			title = m.ID
		}
		*rows = append(*rows, protocol.Model{
			ID:      engine + "/" + m.ID,
			Engine:  engine,
			Name:    m.ID,
			Title:   title,
			Default: i == 0 && len(*rows) == 0,
		})
	}
	var rows []protocol.Model
	var items []item
	if json.Unmarshal(raw, &items) == nil {
		for i, m := range items {
			add(&rows, m, i)
		}
		return rows
	}
	var wrap struct {
		Data    []item `json:"data"`
		Models  []item `json:"models"`
		Default string `json:"default"`
	}
	if json.Unmarshal(raw, &wrap) != nil {
		return nil
	}
	src := wrap.Data
	if len(src) == 0 {
		src = wrap.Models
	}
	for i, m := range src {
		add(&rows, m, i)
		if wrap.Default != "" && m.ID == wrap.Default && len(rows) > 0 {
			for j := range rows {
				rows[j].Default = rows[j].Name == wrap.Default
			}
		}
	}
	return rows
}

func (d *Driver) modelsFromGateway(ctx context.Context) ([]protocol.Model, error) {
	gws := d.discover()
	if len(gws) == 0 {
		return nil, fmt.Errorf("no workbuddy models.json, product config, or live gateway")
	}
	var last error
	cli := &http.Client{Timeout: 5 * time.Second}
	for _, gw := range gws {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, gw.URL+"/api/v1/models", nil)
		if err != nil {
			last = err
			continue
		}
		setGatewayHeaders(req, gw)
		resp, err := cli.Do(req)
		if err != nil {
			last = err
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			last = fmt.Errorf("models http %d: %s", resp.StatusCode, b)
			continue
		}
		rows := parseWorkBuddyModels(d.Name(), b)
		if len(rows) > 0 {
			return rows, nil
		}
		last = fmt.Errorf("gateway returned no models")
	}
	if last == nil {
		last = fmt.Errorf("no workbuddy models.json, product config, or live gateway")
	}
	return nil, last
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
		kind, _ := obj["kind"].(string)
		if kind == "prewarm" {
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
		pass, _ := obj["password"].(string)
		if pass == "" {
			pass, _ = obj["token"].(string)
		}
		if pass == "" {
			pass = os.Getenv("CODEBUDDY_GATEWAY_PASSWORD")
		}
		if pass == "" {
			pass = os.Getenv("WORKBUDDY_GATEWAY_PASSWORD")
		}
		if env := readProcEnviron(pid); env["CODEBUDDY_GATEWAY_PASSWORD"] != "" {
			pass = env["CODEBUDDY_GATEWAY_PASSWORD"]
		} else if env["WORKBUDDY_GATEWAY_PASSWORD"] != "" {
			pass = env["WORKBUDDY_GATEWAY_PASSWORD"]
		}
		out = append(out, gateway{PID: pid, URL: strings.TrimRight(url, "/"), Password: pass, Session: sid, CWD: cwd})
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
			Status: "online",
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
		s.Busy = driver.RecentWithin(jsonl, 2*time.Minute)
		s.UpdatedAt = driver.MtimeUnix(jsonl)
		seen[s.ID] = driver.WithJSONLTitle(s)
	}
	// Always scan projects so recently-active jsonl sessions stay Busy for the
	// hub watcher. On Windows the interactive gateway file often dies while the
	// desktop app keeps appending the transcript.
	_ = filepath.Walk(filepath.Join(d.root, "projects"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".jsonl") {
			return nil
		}
		id := strings.TrimSuffix(info.Name(), ".jsonl")
		if cur, ok := seen[id]; ok {
			if cur.JSONL == "" {
				cur.JSONL = path
			}
			cur.Busy = cur.Busy || driver.RecentWithin(path, 2*time.Minute)
			cur.UpdatedAt = driver.MtimeUnix(path)
			seen[id] = driver.WithJSONLTitle(cur)
			return nil
		}
		busy := driver.RecentWithin(path, 2*time.Minute)
		if !all && !busy {
			return nil
		}
		status := "history"
		if busy {
			status = "busy"
		}
		seen[id] = driver.WithJSONLTitle(protocol.Session{
			Engine:    d.Name(),
			ID:        id,
			Status:    status,
			JSONL:     path,
			Busy:      busy,
			UpdatedAt: info.ModTime().Unix(),
		})
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

func (d *Driver) Reply(ctx context.Context, sessionID, text, model string) (<-chan protocol.Event, error) {
	return d.promptSession(ctx, d.sessionCWD(sessionID), sessionID, text, model)
}

func (d *Driver) sessionCWD(sessionID string) string {
	path := d.findJSONL(sessionID)
	if path == "" {
		return ""
	}
	var cwd string
	_ = watchfile.TailJSONL(path, 40, func(obj map[string]any) {
		if s, _ := obj["cwd"].(string); s != "" {
			cwd = s
		}
	})
	return cwd
}

func (d *Driver) Send(ctx context.Context, cwd, text, resume, model string) (<-chan protocol.Event, error) {
	if resume != "" {
		return d.Reply(ctx, resume, text, model)
	}
	if cwd == "" {
		return nil, fmt.Errorf("pick a working directory")
	}
	return d.promptSession(ctx, cwd, "", text, model)
}

func openACPSession(ctx context.Context, acp *acpClient, cwd, resume, model string) (string, error) {
	params := map[string]any{"cwd": cwd, "mcpServers": []any{}}
	if model != "" {
		params["model"] = model
	}
	try := func(method string, extra map[string]any) (string, error) {
		body := map[string]any{}
		for k, v := range params {
			body[k] = v
		}
		for k, v := range extra {
			body[k] = v
		}
		res, err := acp.call(ctx, method, body)
		if err != nil {
			return "", fmt.Errorf("%s: %w", method, err)
		}
		sid, _ := res["sessionId"].(string)
		return sid, nil
	}
	if resume != "" {
		if sid, err := try("session/load", map[string]any{"sessionId": resume}); err == nil {
			if sid == "" {
				sid = resume
			}
			return sid, nil
		}
		if sid, err := try("session/resume", map[string]any{"sessionId": resume}); err == nil {
			if sid == "" {
				sid = resume
			}
			return sid, nil
		}
	}
	sid, err := try("session/new", nil)
	if err != nil {
		if looksAuthRequired(err) {
			return "", fmt.Errorf("%w (open WorkBuddy and sign in, or keep its window running)", err)
		}
		return "", err
	}
	if sid == "" {
		if resume != "" {
			return resume, nil
		}
		return "", fmt.Errorf("session/new did not return sessionId")
	}
	return sid, nil
}

func looksAuthRequired(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "authentication required") || strings.Contains(msg, "auth_required") || strings.Contains(msg, "/login")
}

func (d *Driver) promptSession(ctx context.Context, cwd, resume, text, model string) (<-chan protocol.Event, error) {
	gw, err := d.ensureGateway(ctx, cwd, resume)
	if err != nil {
		return nil, err
	}
	if cwd == "" {
		cwd = gw.CWD
	}
	if cwd == "" && resume == "" {
		return nil, fmt.Errorf("pick a working directory")
	}
	acp, err := newACP(ctx, gw)
	if err != nil {
		return nil, err
	}
	if _, err := acp.call(ctx, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientInfo":         map[string]any{"name": "agent-mesh", "version": "1.0.0"},
		"clientCapabilities": map[string]any{},
	}); err != nil {
		return nil, fmt.Errorf("acp initialize: %w", err)
	}
	cwdParam := cwd
	if cwdParam == "" {
		cwdParam = gw.CWD
	}
	modelName := driver.CLIModel(model, d.Name())
	sid, err := openACPSession(ctx, acp, cwdParam, resume, modelName)
	if err != nil {
		return nil, err
	}
	out := make(chan protocol.Event, 256)
	go func() {
		defer close(out)
		out <- protocol.Event{Type: "session", Engine: d.Name(), Session: sid}
		wctx, cancel := context.WithCancel(ctx)
		defer cancel()
		promptErr := make(chan error, 1)
		go func() {
			_, err := acp.call(ctx, "session/prompt", map[string]any{
				"sessionId": sid,
				"prompt":    []any{map[string]any{"type": "text", "text": text}},
			})
			promptErr <- err
		}()
		var watch <-chan protocol.Event
		for i := 0; i < 100 && watch == nil; i++ {
			ch, werr := d.Watch(wctx, sid)
			if werr == nil {
				watch = ch
				break
			}
			select {
			case <-wctx.Done():
				out <- protocol.Event{Type: "log", Engine: d.Name(), Session: sid, Text: "created a new session", Done: true}
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
		if watch == nil {
			out <- protocol.Event{Type: "log", Engine: d.Name(), Session: sid, Text: "created a new session", Done: true}
			return
		}
		promptDone := false
		idle := time.NewTimer(time.Hour)
		defer idle.Stop()
		for {
			select {
			case ev, ok := <-watch:
				if !ok {
					return
				}
				out <- ev
				if ev.Done {
					return
				}
				if promptDone {
					if !idle.Stop() {
						select {
						case <-idle.C:
						default:
						}
					}
					idle.Reset(3 * time.Second)
				}
			case err := <-promptErr:
				if err != nil && wctx.Err() == nil {
					out <- protocol.Event{Type: "log", Engine: d.Name(), Session: sid, Text: err.Error()}
				}
				promptDone = true
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				idle.Reset(3 * time.Second)
			case <-idle.C:
				if promptDone {
					out <- protocol.Event{Type: "log", Engine: d.Name(), Session: sid, Text: "created a new session", Done: true}
					return
				}
			case <-wctx.Done():
				return
			}
		}
	}()
	return out, nil
}
