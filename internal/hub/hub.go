package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/openai"
	"agent-mesh/internal/protocol"
)

// Hub 监听本机已启用引擎的在线会话，转成 OpenAI chunk 推给订阅者和中继。
type Hub struct {
	Drivers  *driver.Set
	NodeID   string
	NodeName string
	Log      *slog.Logger

	mu       sync.Mutex
	subs     map[int]chan openai.Chunk
	next     int
	watches  map[string]context.CancelFunc
	relays   []string
	password string
	http     *http.Client
}

// New 创建集线器。
func New(drivers *driver.Set, nodeID, nodeName string, log *slog.Logger) *Hub {
	if log == nil {
		log = slog.Default()
	}
	return &Hub{
		Drivers:  drivers,
		NodeID:   nodeID,
		NodeName: nodeName,
		Log:      log,
		subs:     make(map[int]chan openai.Chunk),
		watches:  make(map[string]context.CancelFunc),
		http:     &http.Client{Timeout: 8 * time.Second},
	}
}

// SetRelays 设置中继 ingest 地址（不含路径）。
func (h *Hub) SetRelays(urls []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.relays = append([]string(nil), urls...)
}

// SetPassword 中继约定密码，ingest 时带 Bearer。
func (h *Hub) SetPassword(pw string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.password = pw
}

// Subscribe 订阅 chunk。调用方必须 Unsubscribe。
func (h *Hub) Subscribe(buf int) (int, <-chan openai.Chunk) {
	if buf <= 0 {
		buf = 64
	}
	ch := make(chan openai.Chunk, buf)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	id := h.next
	h.subs[id] = ch
	return id, ch
}

// Unsubscribe 取消订阅。
func (h *Hub) Unsubscribe(id int) {
	h.mu.Lock()
	ch, ok := h.subs[id]
	if ok {
		delete(h.subs, id)
	}
	h.mu.Unlock()
	if ok {
		close(ch)
	}
}

// Broadcast 向本地订阅者和中继推送。
func (h *Hub) Broadcast(chunk openai.Chunk) {
	h.mu.Lock()
	subs := make([]chan openai.Chunk, 0, len(h.subs))
	for _, ch := range h.subs {
		subs = append(subs, ch)
	}
	relays := append([]string(nil), h.relays...)
	password := h.password
	h.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- chunk:
		default:
		}
	}
	if len(relays) == 0 {
		return
	}
	raw, err := json.Marshal(chunk)
	if err != nil {
		return
	}
	for _, relay := range relays {
		u := relay + "/v1/openai/ingest"
		go h.post(u, raw, password)
	}
}

func (h *Hub) post(u string, raw []byte, password string) {
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(raw))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if password != "" {
		req.Header.Set("Authorization", "Bearer "+password)
	}
	resp, err := h.http.Do(req)
	if err != nil {
		h.Log.Debug("ingest failed", "url", u, "err", err)
		return
	}
	resp.Body.Close()
}

func keyOf(engine, id string) string { return engine + "/" + id }

// Start 周期扫描在线会话并挂上 Watch。
func (h *Hub) Start(ctx context.Context) {
	h.scan(ctx)
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			h.stopAll()
			return
		case <-t.C:
			h.scan(ctx)
		}
	}
}

func (h *Hub) stopAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for k, cancel := range h.watches {
		cancel()
		delete(h.watches, k)
	}
}

func (h *Hub) scan(ctx context.Context) {
	rows, err := h.Drivers.ListAll(ctx, "", false)
	if err != nil {
		h.Log.Debug("hub list", "err", err)
		return
	}
	live := map[string]protocol.Session{}
	for _, s := range rows {
		if s.Live || s.Busy {
			live[keyOf(s.Engine, s.ID)] = s
		}
	}
	h.mu.Lock()
	for k, cancel := range h.watches {
		if _, ok := live[k]; !ok {
			cancel()
			delete(h.watches, k)
		}
	}
	h.mu.Unlock()
	for k, sess := range live {
		h.ensureWatch(ctx, sess)
		_ = k
	}
}

func (h *Hub) ensureWatch(parent context.Context, sess protocol.Session) {
	k := keyOf(sess.Engine, sess.ID)
	h.mu.Lock()
	if _, ok := h.watches[k]; ok {
		h.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	h.watches[k] = cancel
	h.mu.Unlock()
	go h.runWatch(ctx, sess)
}

func (h *Hub) runWatch(ctx context.Context, sess protocol.Session) {
	d, err := h.Drivers.Get(sess.Engine)
	if err != nil {
		return
	}
	ch, err := d.Watch(ctx, sess.ID)
	if err != nil {
		h.Log.Debug("watch", "engine", sess.Engine, "session", sess.ID, "err", err)
		return
	}
	id := openai.NewID()
	model := sess.Engine
	for ev := range ch {
		if ev.Engine == "" {
			ev.Engine = sess.Engine
		}
		if ev.Session == "" {
			ev.Session = sess.ID
		}
		h.Broadcast(openai.FromEvent(id, model, h.NodeID, h.NodeName, ev))
	}
}

// WatchOne 显式跟踪一个会话（含历史），直到 ctx 取消。
func (h *Hub) WatchOne(ctx context.Context, engine, sessionID string) (<-chan openai.Chunk, error) {
	d, err := h.Drivers.Get(engine)
	if err != nil {
		return nil, err
	}
	evs, err := d.Watch(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make(chan openai.Chunk, 32)
	go func() {
		defer close(out)
		id := openai.NewID()
		for ev := range evs {
			if ev.Engine == "" {
				ev.Engine = engine
			}
			if ev.Session == "" {
				ev.Session = sessionID
			}
			chunk := openai.FromEvent(id, engine, h.NodeID, h.NodeName, ev)
			h.Broadcast(chunk)
			out <- chunk
		}
	}()
	return out, nil
}

// Pump 把引擎事件流转成 OpenAI chunk 并广播。
func (h *Hub) Pump(engine, session, model string, evs <-chan protocol.Event) <-chan openai.Chunk {
	out := make(chan openai.Chunk, 32)
	go func() {
		defer close(out)
		id := openai.NewID()
		if model == "" {
			model = engine
		}
		for ev := range evs {
			if ev.Engine == "" {
				ev.Engine = engine
			}
			if ev.Session == "" {
				ev.Session = session
			}
			chunk := openai.FromEvent(id, model, h.NodeID, h.NodeName, ev)
			h.Broadcast(chunk)
			select {
			case out <- chunk:
			default:
			}
		}
	}()
	return out
}
