package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"agent-mesh/internal/openai"
	"agent-mesh/internal/protocol"
)

const (
	sessionLimitPerEngine = 100
	turnLimit             = 20
)

// Catalog 网页目录快照。
type Catalog struct {
	Nodes     []protocol.NodeInfo    `json:"nodes"`
	Engines   []EngineRow            `json:"engines"`
	Models    []openai.ModelCard     `json:"models"`
	Dirs      []DirRow               `json:"dirs"`
	Sessions  []protocol.Session     `json:"sessions"`
	Followups []protocol.FollowQueue `json:"followups"`
}

// EngineRow 引擎 + 所属节点。
type EngineRow struct {
	protocol.EngineInfo
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name"`
}

// DirRow 工作目录 + 所属节点。
type DirRow struct {
	protocol.WorkDir
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name"`
}

// BusMsg 发给网页的 WebSocket 消息，按类型分片。
type BusMsg struct {
	Type      string                 `json:"type"`
	Nodes     []protocol.NodeInfo    `json:"nodes,omitempty"`
	Engines   []EngineRow            `json:"engines,omitempty"`
	Models    []openai.ModelCard     `json:"models,omitempty"`
	Dirs      []DirRow               `json:"dirs,omitempty"`
	Sessions  []protocol.Session     `json:"sessions,omitempty"`
	Followups []protocol.FollowQueue `json:"followups,omitempty"`
	Events    []protocol.Event       `json:"events,omitempty"`
	NodeID    string                 `json:"node_id,omitempty"`
	Engine    string                 `json:"engine,omitempty"`
	Session   string                 `json:"session,omitempty"`
	Reset     bool                   `json:"reset,omitempty"`
	Removed   []string               `json:"removed,omitempty"`
	Chunk     *openai.Chunk          `json:"chunk,omitempty"`
}

type busSub struct {
	ch         chan BusMsg
	node       string
	engine     string
	session    string
	finger     string
	lastEvents []protocol.Event
}

type catalogHub struct {
	mu   sync.Mutex
	next int
	subs map[int]*busSub
}

func newCatalogHub() *catalogHub {
	return &catalogHub{subs: map[int]*busSub{}}
}

func (h *catalogHub) subscribe() (int, <-chan BusMsg) {
	ch := make(chan BusMsg, 256)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	id := h.next
	h.subs[id] = &busSub{ch: ch}
	return id, ch
}

func (h *catalogHub) unsubscribe(id int) {
	h.mu.Lock()
	delete(h.subs, id)
	h.mu.Unlock()
}

func (h *catalogHub) send(id int, msg BusMsg) {
	h.mu.Lock()
	sub, ok := h.subs[id]
	var ch chan BusMsg
	if ok {
		ch = sub.ch
	}
	h.mu.Unlock()
	if !ok || ch == nil {
		return
	}
	defer func() { _ = recover() }()
	if msg.Type == "conversation" || msg.Type == "chunk" {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case ch <- msg:
		case <-timer.C:
		}
		return
	}
	select {
	case ch <- msg:
	default:
	}
}

func (h *catalogHub) setWatch(id int, node, engine, session, finger string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if sub, ok := h.subs[id]; ok {
		if sub.node != node || sub.engine != engine || sub.session != session {
			sub.lastEvents = nil
		}
		sub.node = node
		sub.engine = engine
		sub.session = session
		sub.finger = finger
	}
}

func (h *catalogHub) commitEvents(id int, evs []protocol.Event) (out []protocol.Event, reset bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if evs == nil {
		evs = []protocol.Event{}
	}
	sub, ok := h.subs[id]
	if !ok {
		return lastTurns(evs, turnLimit), true
	}
	out, reset = diffEvents(sub.lastEvents, evs)
	sub.lastEvents = append([]protocol.Event(nil), evs...)
	if reset {
		out = lastTurns(evs, turnLimit)
	}
	return out, reset
}

func (h *catalogHub) publish(msg BusMsg) {
	h.mu.Lock()
	subs := make([]chan BusMsg, 0, len(h.subs))
	for _, sub := range h.subs {
		subs = append(subs, sub.ch)
	}
	h.mu.Unlock()
	for _, ch := range subs {
		func(ch chan BusMsg) {
			defer func() { _ = recover() }()
			select {
			case ch <- msg:
			default:
			}
		}(ch)
	}
}

type watchHit struct {
	id                 int
	ch                 chan BusMsg
	node, engine, sess string
	finger             string
}

func (h *catalogHub) watches() []watchHit {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]watchHit, 0, len(h.subs))
	for id, sub := range h.subs {
		if sub.session == "" {
			continue
		}
		out = append(out, watchHit{id: id, ch: sub.ch, node: sub.node, engine: sub.engine, sess: sub.session, finger: sub.finger})
	}
	return out
}

func (s *Server) catalog() Catalog {
	out := Catalog{
		Nodes:     []protocol.NodeInfo{},
		Engines:   []EngineRow{},
		Models:    []openai.ModelCard{},
		Dirs:      []DirRow{},
		Sessions:  []protocol.Session{},
		Followups: []protocol.FollowQueue{},
	}
	for _, rec := range s.Dir.List() {
		info := rec.Info
		info.LastSeen = 0
		out.Nodes = append(out.Nodes, info)
		for _, e := range rec.Snapshot.Engines {
			out.Engines = append(out.Engines, EngineRow{EngineInfo: e, NodeID: rec.Info.ID, NodeName: rec.Info.Name})
		}
		for _, m := range openai.FromModels(rec.Snapshot.Models).Data {
			if !strings.HasPrefix(m.ID, rec.Info.ID+"/") {
				m.ID = rec.Info.ID + "/" + m.ID
			}
			m.Created = 0
			out.Models = append(out.Models, m)
		}
		for _, d := range rec.Snapshot.Dirs {
			out.Dirs = append(out.Dirs, DirRow{WorkDir: d, NodeID: rec.Info.ID, NodeName: rec.Info.Name})
		}
		for _, sess := range rec.Snapshot.Sessions {
			if sess.NodeID == "" {
				sess.NodeID = rec.Info.ID
				sess.NodeName = rec.Info.Name
			}
			out.Sessions = append(out.Sessions, sess)
		}
		for _, q := range rec.Snapshot.Followups {
			if q.NodeID == "" {
				q.NodeID = rec.Info.ID
			}
			if q.Items == nil {
				q.Items = []protocol.FollowItem{}
			}
			out.Followups = append(out.Followups, q)
		}
	}
	out.Sessions = rankSessions(out.Sessions, sessionLimitPerEngine)
	return out
}

func rankSessions(rows []protocol.Session, perEngine int) []protocol.Session {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].UpdatedAt != rows[j].UpdatedAt {
			return rows[i].UpdatedAt > rows[j].UpdatedAt
		}
		li := rows[i].Live || rows[i].Busy
		lj := rows[j].Live || rows[j].Busy
		if li != lj {
			return li
		}
		return rows[i].Title < rows[j].Title
	})
	if perEngine <= 0 {
		return rows
	}
	n := map[string]int{}
	out := make([]protocol.Session, 0, len(rows))
	for _, s := range rows {
		if s.Live || s.Busy {
			out = append(out, s)
			continue
		}
		if n[s.Engine] >= perEngine {
			continue
		}
		n[s.Engine]++
		out = append(out, s)
	}
	return out
}

func (s *Server) publishCatalog() {
	cat := s.catalog()
	s.catMu.Lock()
	prev := s.lastCat
	s.lastCat = cat
	s.catMu.Unlock()
	publishKeyed(s, "nodes", prev.Nodes, cat.Nodes, catalogNodeKey)
	publishKeyed(s, "engines", prev.Engines, cat.Engines, catalogEngineKey)
	publishKeyed(s, "models", prev.Models, cat.Models, catalogModelKey)
	publishKeyed(s, "dirs", prev.Dirs, cat.Dirs, catalogDirKey)
	publishKeyed(s, "sessions", prev.Sessions, cat.Sessions, catalogSessionKey)
	publishKeyed(s, "followups", prev.Followups, cat.Followups, catalogFollowKey)
	s.notifyWatches()
}

func catalogNodeKey(n protocol.NodeInfo) string { return n.ID }

func catalogEngineKey(e EngineRow) string { return e.NodeID + "\x1f" + e.Name }

func catalogModelKey(m openai.ModelCard) string { return m.ID }

func catalogDirKey(d DirRow) string { return d.NodeID + "\x1f" + d.Engine + "\x1f" + d.Path }

func catalogSessionKey(s protocol.Session) string {
	return s.NodeID + "|" + s.Engine + "|" + s.ID
}

func catalogFollowKey(q protocol.FollowQueue) string {
	return q.NodeID + "|" + q.Engine + "|" + q.Session
}

func jsonEqual(a, b any) bool {
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	return bytes.Equal(ra, rb)
}

func diffRows[T any](old, neu []T, key func(T) string) (changed []T, removed []string) {
	prev := make(map[string]T, len(old))
	for _, row := range old {
		prev[key(row)] = row
	}
	seen := make(map[string]struct{}, len(neu))
	for _, row := range neu {
		k := key(row)
		seen[k] = struct{}{}
		if was, ok := prev[k]; !ok || !jsonEqual(was, row) {
			changed = append(changed, row)
		}
	}
	for k := range prev {
		if _, ok := seen[k]; !ok {
			removed = append(removed, k)
		}
	}
	return changed, removed
}

func publishKeyed[T any](s *Server, kind string, old, neu []T, key func(T) string) {
	changed, removed := diffRows(old, neu, key)
	if len(changed) == 0 && len(removed) == 0 {
		return
	}
	if s.catalogs == nil {
		return
	}
	msg := BusMsg{Type: kind, Removed: removed}
	switch kind {
	case "sessions":
		rows, _ := any(changed).([]protocol.Session)
		msg.Sessions = rows
	case "nodes":
		rows, _ := any(changed).([]protocol.NodeInfo)
		msg.Nodes = rows
	case "engines":
		rows, _ := any(changed).([]EngineRow)
		msg.Engines = rows
	case "models":
		rows, _ := any(changed).([]openai.ModelCard)
		msg.Models = rows
	case "dirs":
		rows, _ := any(changed).([]DirRow)
		msg.Dirs = rows
	case "followups":
		rows, _ := any(changed).([]protocol.FollowQueue)
		msg.Followups = rows
	}
	s.catalogs.publish(msg)
}

func (s *Server) snapshotParts() []BusMsg {
	cat := s.catalog()
	return []BusMsg{
		{Type: "sessions", Sessions: cat.Sessions, Reset: true},
		{Type: "nodes", Nodes: cat.Nodes, Reset: true},
		{Type: "engines", Engines: cat.Engines, Reset: true},
		{Type: "models", Models: cat.Models, Reset: true},
		{Type: "dirs", Dirs: cat.Dirs, Reset: true},
		{Type: "followups", Followups: cat.Followups, Reset: true},
	}
}

func (s *Server) sessionFinger(node, engine, sid string) string {
	rec, ok := s.Dir.Get(node)
	if !ok {
		return ""
	}
	for _, sess := range rec.Snapshot.Sessions {
		if sess.ID != sid {
			continue
		}
		if engine != "" && sess.Engine != engine {
			continue
		}
		return fmt.Sprintf("%d:%t:%t:%s", sess.UpdatedAt, sess.Live, sess.Busy, sess.Status)
	}
	return ""
}

func (s *Server) notifyWatches() {
	if s.catalogs == nil {
		return
	}
	for _, w := range s.catalogs.watches() {
		finger := s.sessionFinger(w.node, w.engine, w.sess)
		if finger == "" || finger == w.finger {
			continue
		}
		s.catalogs.setWatch(w.id, w.node, w.engine, w.sess, finger)
		go s.emitConversation(w.id, w.node, w.engine, w.sess)
	}
}

func isUserEvent(ev protocol.Event) bool {
	return ev.Type == "user" || ev.Role == "user"
}

func lastTurns(evs []protocol.Event, n int) []protocol.Event {
	if n <= 0 || len(evs) == 0 {
		if evs == nil {
			return []protocol.Event{}
		}
		return evs
	}
	starts := make([]int, 0, 32)
	for i, ev := range evs {
		if isUserEvent(ev) {
			starts = append(starts, i)
		}
	}
	if len(starts) <= n {
		return evs
	}
	return evs[starts[len(starts)-n]:]
}

func diffEvents(old, neu []protocol.Event) (added []protocol.Event, reset bool) {
	if len(old) == 0 {
		return neu, true
	}
	i := 0
	for i < len(old) && i < len(neu) && eventEqual(old[i], neu[i]) {
		i++
	}
	if i == len(old) {
		return neu[i:], false
	}
	if i == len(old)-1 && i < len(neu) && eventSameKind(old[i], neu[i]) && strings.HasPrefix(neu[i].Text, old[i].Text) {
		delta := neu[i]
		delta.Text = strings.TrimPrefix(neu[i].Text, old[i].Text)
		delta.Preview = ""
		out := make([]protocol.Event, 0, 1+len(neu)-i-1)
		if delta.Text != "" || delta.Type == "tool_call" || delta.Type == "tool_result" || delta.Type == "thinking" {
			out = append(out, delta)
		}
		out = append(out, neu[i+1:]...)
		return out, false
	}
	return neu, true
}

func eventSameKind(a, b protocol.Event) bool {
	return a.Type == b.Type && a.Tool == b.Tool && a.Role == b.Role
}

func eventEqual(a, b protocol.Event) bool {
	return eventSameKind(a, b) && a.Text == b.Text
}

func (s *Server) emitConversation(id int, node, engine, session string) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	evs, err := s.loadHistory(ctx, node, engine, session)
	if err != nil {
		s.Log.Debug("conversation", "node", node, "session", session, "err", err)
		return
	}
	added, reset := s.catalogs.commitEvents(id, evs)
	if !reset && len(added) == 0 {
		return
	}
	s.catalogs.send(id, BusMsg{Type: "conversation", NodeID: node, Engine: engine, Session: session, Events: added, Reset: reset})
}

func (s *Server) loadHistory(ctx context.Context, nodeID, engine, session string) ([]protocol.Event, error) {
	rec, err := s.lookup(nodeID)
	if err != nil {
		return nil, err
	}
	since := time.Now().Add(-48 * time.Hour).Unix()
	path := fmt.Sprintf("/v1/openai/history?engine=%s&session=%s&since=%d&tail=8000&turns=%d",
		url.QueryEscape(engine), url.QueryEscape(session), since, turnLimit)
	raw, err := s.getPath(ctx, rec, path)
	if err != nil {
		return nil, err
	}
	var evs []protocol.Event
	if json.Unmarshal(raw, &evs) != nil {
		return nil, fmt.Errorf("bad history json")
	}
	if evs == nil {
		evs = []protocol.Event{}
	}
	return evs, nil
}
