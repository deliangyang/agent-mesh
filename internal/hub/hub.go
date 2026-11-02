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

const (
	quietAfterDone = 300 * time.Millisecond
	chunkSendWait  = 2 * time.Second
)

// Hub watches live sessions on enabled local engines and pushes OpenAI chunks to subscribers and the relay.
type Hub struct {
	Drivers  *driver.Set
	NodeID   string
	NodeName string
	Log      *slog.Logger

	mu         sync.Mutex
	subs       map[int]chan openai.Chunk
	next       int
	watchSeq   int
	watches    map[string]watchSlot
	relays     []string
	password   string
	http       *http.Client
	pushIngest func(openai.Chunk) bool
}

type watchSlot struct {
	cancel context.CancelFunc
	gen    int
}

// New creates a hub.
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
		watches:  make(map[string]watchSlot),
		http:     &http.Client{Timeout: 8 * time.Second},
	}
}

// SetRelays sets relay ingest URLs (no path).
func (h *Hub) SetRelays(urls []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.relays = append([]string(nil), urls...)
}

// SetPassword is the relay password, sent as Bearer on ingest.
func (h *Hub) SetPassword(pw string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.password = pw
}

// SetPushIngest sets tunnel push. When it returns true, HTTP ingest is skipped.
func (h *Hub) SetPushIngest(fn func(openai.Chunk) bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pushIngest = fn
}

// Subscribe subscribes to chunks. Callers must Unsubscribe.
func (h *Hub) Subscribe(buf int) (int, <-chan openai.Chunk) {
	if buf < 256 {
		buf = 256
	}
	ch := make(chan openai.Chunk, buf)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	id := h.next
	h.subs[id] = ch
	return id, ch
}

// Unsubscribe unsubscribes.
func (h *Hub) Unsubscribe(id int) {
	h.mu.Lock()
	ch, ok := h.subs[id]
	if ok {
		delete(h.subs, id)
	}
	h.mu.Unlock()
	if ok {
		defer func() { _ = recover() }()
		close(ch)
	}
}

type chunkSub struct {
	id int
	ch chan openai.Chunk
}

// Broadcast pushes to local subscribers and the relay.
func (h *Hub) Broadcast(chunk openai.Chunk) {
	h.mu.Lock()
	subs := make([]chunkSub, 0, len(h.subs))
	for id, ch := range h.subs {
		subs = append(subs, chunkSub{id: id, ch: ch})
	}
	relays := append([]string(nil), h.relays...)
	password := h.password
	push := h.pushIngest
	h.mu.Unlock()
	var drop []int
	for _, sub := range subs {
		if !sendChunk(sub.ch, chunk, chunkSendWait) {
			drop = append(drop, sub.id)
		}
	}
	for _, id := range drop {
		h.Unsubscribe(id)
	}
	if push != nil && push(chunk) {
		return
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

func sendChunk(ch chan openai.Chunk, chunk openai.Chunk, wait time.Duration) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	select {
	case ch <- chunk:
		return true
	default:
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case ch <- chunk:
		return true
	case <-timer.C:
		return false
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

// Start scans live sessions on a loop and attaches Watch.
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
	for k, slot := range h.watches {
		slot.cancel()
		delete(h.watches, k)
	}
}

func (h *Hub) watching(engine, id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.watches[keyOf(engine, id)]
	return ok
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
	for k, slot := range h.watches {
		if _, ok := live[k]; !ok {
			slot.cancel()
			delete(h.watches, k)
		}
	}
	h.mu.Unlock()
	for _, sess := range live {
		h.ensureWatch(ctx, sess)
	}
}

func (h *Hub) ensureWatch(parent context.Context, sess protocol.Session) {
	k := keyOf(sess.Engine, sess.ID)
	h.mu.Lock()
	if _, ok := h.watches[k]; ok {
		h.mu.Unlock()
		return
	}
	h.watchSeq++
	gen := h.watchSeq
	ctx, cancel := context.WithCancel(parent)
	h.watches[k] = watchSlot{cancel: cancel, gen: gen}
	h.mu.Unlock()
	go h.runWatch(ctx, gen, sess)
}

func (h *Hub) runWatch(ctx context.Context, gen int, sess protocol.Session) {
	k := keyOf(sess.Engine, sess.ID)
	defer func() {
		h.mu.Lock()
		if slot, ok := h.watches[k]; ok && slot.gen == gen {
			delete(h.watches, k)
		}
		h.mu.Unlock()
	}()
	d, err := h.Drivers.Get(sess.Engine)
	if err != nil {
		return
	}
	ch, err := d.Watch(ctx, sess.ID)
	if err != nil {
		h.Log.Debug("watch", "engine", sess.Engine, "session", sess.ID, "err", err)
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
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

// WatchOne follows one session, including history, until ctx is canceled.
func (h *Hub) WatchOne(ctx context.Context, engine, sessionID string) (<-chan openai.Chunk, error) {
	d, err := h.Drivers.Get(engine)
	if err != nil {
		return nil, err
	}
	evs, err := d.Watch(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make(chan openai.Chunk, 256)
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
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// Pump turns an engine event stream into OpenAI chunks and broadcasts them.
// after Done, a short quiet period with no new events closes the output channel without stopping the underlying Watch.
func (h *Hub) Pump(engine, session, model string, evs <-chan protocol.Event) <-chan openai.Chunk {
	out := make(chan openai.Chunk, 256)
	go func() {
		defer close(out)
		if evs == nil {
			return
		}
		id := openai.NewID()
		if model == "" {
			model = engine
		}
		var quiet <-chan time.Time
		for {
			var ev protocol.Event
			var ok bool
			if quiet == nil {
				ev, ok = <-evs
				if !ok {
					return
				}
			} else {
				select {
				case ev, ok = <-evs:
					if !ok {
						return
					}
				case <-quiet:
					return
				}
			}
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
			case <-time.After(30 * time.Second):
				h.Log.Debug("pump downstream blocked too long; ending this turn", "session", ev.Session, "type", ev.Type)
				return
			}
			if ev.Done || quiet != nil {
				quiet = time.After(quietAfterDone)
			}
		}
	}()
	return out
}
