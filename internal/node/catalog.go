package node

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/openai"
	"agent-mesh/internal/protocol"
)

// busMsg is the Vue catalog WebSocket payload (same shape as the relay).
type busMsg struct {
	Type      string                 `json:"type"`
	Nodes     []protocol.NodeInfo    `json:"nodes,omitempty"`
	Engines   []engineRow            `json:"engines,omitempty"`
	Models    []openai.ModelCard     `json:"models,omitempty"`
	Dirs      []dirRow               `json:"dirs,omitempty"`
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

type engineRow struct {
	protocol.EngineInfo
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name"`
}

type dirRow struct {
	protocol.WorkDir
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name"`
}

type busHub struct {
	mu   sync.Mutex
	next int
	subs map[int]chan busMsg
}

func newBusHub() *busHub {
	return &busHub{subs: map[int]chan busMsg{}}
}

func (h *busHub) subscribe() (int, <-chan busMsg) {
	ch := make(chan busMsg, 256)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	id := h.next
	h.subs[id] = ch
	return id, ch
}

func (h *busHub) unsubscribe(id int) {
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

func (h *busHub) publish(msg busMsg) {
	h.mu.Lock()
	subs := make([]chan busMsg, 0, len(h.subs))
	for _, ch := range h.subs {
		subs = append(subs, ch)
	}
	h.mu.Unlock()
	for _, ch := range subs {
		func(ch chan busMsg) {
			defer func() { _ = recover() }()
			timer := time.NewTimer(2 * time.Second)
			defer timer.Stop()
			select {
			case ch <- msg:
			case <-timer.C:
			}
		}(ch)
	}
}

func (s *Server) webCatalog() (nodes []protocol.NodeInfo, engines []engineRow, models []openai.ModelCard, dirs []dirRow, sessions []protocol.Session, followups []protocol.FollowQueue) {
	info := s.Info()
	snap := s.peekSnapshot()
	nodes = []protocol.NodeInfo{info}
	engines = []engineRow{}
	for _, e := range snap.Engines {
		engines = append(engines, engineRow{EngineInfo: e, NodeID: info.ID, NodeName: info.Name})
	}
	models = openai.FromModels(snap.Models).Data
	if models == nil {
		models = []openai.ModelCard{}
	}
	// Match the relay catalog: Vue filters models by nodeID/… prefix.
	// Created comes from FromModels as time.Now and would republish every scan.
	for i := range models {
		models[i].Created = 0
		if info.ID != "" && !strings.HasPrefix(models[i].ID, info.ID+"/") {
			models[i].ID = info.ID + "/" + models[i].ID
		}
	}
	dirs = []dirRow{}
	for _, d := range snap.Dirs {
		dirs = append(dirs, dirRow{WorkDir: d, NodeID: info.ID, NodeName: info.Name})
	}
	sessions = []protocol.Session{}
	for _, sess := range snap.Sessions {
		if sess.NodeID == "" {
			sess.NodeID = info.ID
			sess.NodeName = info.Name
		}
		// Active is authoritative for turns this node runs; Busy stays the
		// driver's file-mtime guess, capped so it cannot look busy for minutes
		// after the turn ended.
		sess.Active = s.turnActive(sess.Engine, sess.ID, sess.CWD)
		if sess.Active {
			sess.Busy = true
		} else if sess.JSONL != "" && !driver.RecentWithin(sess.JSONL, 12*time.Second) {
			sess.Busy = false
		}
		sessions = append(sessions, sess)
	}
	followups = []protocol.FollowQueue{}
	for _, q := range snap.Followups {
		if q.NodeID == "" {
			q.NodeID = info.ID
		}
		if q.Items == nil {
			q.Items = []protocol.FollowItem{}
		}
		followups = append(followups, q)
	}
	return nodes, engines, models, dirs, sessions, followups
}

// catalogSnap is the last catalog published on the bus, used to emit diffs.
type catalogSnap struct {
	nodes     []protocol.NodeInfo
	engines   []engineRow
	models    []openai.ModelCard
	dirs      []dirRow
	sessions  []protocol.Session
	followups []protocol.FollowQueue
}

func (s *Server) publishCatalog() {
	if s.bus == nil {
		return
	}
	nodes, engines, models, dirs, sessions, followups := s.webCatalog()
	s.mu.Lock()
	prev := s.published
	s.published = catalogSnap{
		nodes:     nodes,
		engines:   engines,
		models:    models,
		dirs:      dirs,
		sessions:  sessions,
		followups: followups,
	}
	s.mu.Unlock()
	nid := ""
	if len(nodes) > 0 {
		nid = nodes[0].ID
	}
	if changed, removed := diffRows(prev.nodes, nodes, func(n protocol.NodeInfo) string { return n.ID }); len(changed) > 0 || len(removed) > 0 {
		s.bus.publish(busMsg{Type: "nodes", Nodes: changed, Removed: removed, NodeID: nid})
	}
	if changed, removed := diffRows(prev.engines, engines, engineBusKey); len(changed) > 0 || len(removed) > 0 {
		s.bus.publish(busMsg{Type: "engines", Engines: changed, Removed: removed, NodeID: nid})
	}
	if changed, removed := diffRows(prev.models, models, func(m openai.ModelCard) string { return m.ID }); len(changed) > 0 || len(removed) > 0 {
		s.bus.publish(busMsg{Type: "models", Models: changed, Removed: removed, NodeID: nid})
	}
	if changed, removed := diffRows(prev.dirs, dirs, dirBusKey); len(changed) > 0 || len(removed) > 0 {
		s.bus.publish(busMsg{Type: "dirs", Dirs: changed, Removed: removed, NodeID: nid})
	}
	if changed, removed := diffRows(prev.sessions, sessions, sessionBusKey); len(changed) > 0 || len(removed) > 0 {
		out := make([]protocol.Session, len(changed))
		for i := range changed {
			out[i] = changed[i]
			markGit(&out[i])
		}
		s.bus.publish(busMsg{Type: "sessions", Sessions: out, Removed: removed, NodeID: nid})
	}
	if changed, removed := diffRows(prev.followups, followups, followBusKey); len(changed) > 0 || len(removed) > 0 {
		s.bus.publish(busMsg{Type: "followups", Followups: changed, Removed: removed, NodeID: nid})
	}
}

func engineBusKey(e engineRow) string {
	return e.NodeID + "\x1f" + e.Name
}

func dirBusKey(d dirRow) string {
	return d.NodeID + "\x1f" + d.Engine + "\x1f" + d.Path
}

func sessionBusKey(s protocol.Session) string {
	return s.NodeID + "|" + s.Engine + "|" + s.ID
}

func followBusKey(q protocol.FollowQueue) string {
	return q.NodeID + "|" + q.Engine + "|" + q.Session
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
		was, ok := prev[k]
		if !ok || !jsonEqual(was, row) {
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

func jsonEqual(a, b any) bool {
	ra, errA := json.Marshal(a)
	rb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return bytes.Equal(ra, rb)
}

// markGit sets Git from a single .git stat. It does not run git status.
func markGit(sess *protocol.Session) {
	ok := false
	if cwd := strings.TrimSpace(sess.CWD); cwd != "" {
		if _, err := os.Stat(filepath.Join(cwd, ".git")); err == nil {
			ok = true
		}
	}
	sess.Git = &ok
}

// publishLiveSession pushes one active session without waiting for the snapshot scan.
func (s *Server) publishLiveSession(engine, session, cwd string) {
	if s == nil || s.bus == nil || session == "" {
		return
	}
	row := protocol.Session{
		Engine:    engine,
		ID:        session,
		CWD:       cwd,
		NodeID:    s.Ident.ID,
		NodeName:  s.Name,
		Live:      true,
		Busy:      true,
		Active:    true,
		UpdatedAt: time.Now().Unix(),
	}
	_, _, _, _, sessions, _ := s.webCatalog()
	for _, prev := range sessions {
		if prev.Engine != engine || prev.ID != session {
			continue
		}
		if row.CWD == "" {
			row.CWD = prev.CWD
		}
		if prev.Title != "" {
			row.Title = prev.Title
		}
		if prev.UpdatedAt > row.UpdatedAt {
			row.UpdatedAt = prev.UpdatedAt
		}
		if prev.NodeID != "" {
			row.NodeID = prev.NodeID
		}
		if prev.NodeName != "" {
			row.NodeName = prev.NodeName
		}
		break
	}
	markGit(&row)
	s.bus.publish(busMsg{Type: "sessions", Sessions: []protocol.Session{row}, NodeID: s.Ident.ID})
}

// publishWatchedSession pushes the opened session with a git marker.
func (s *Server) publishWatchedSession(engine, session string) {
	if s == nil || s.bus == nil || session == "" {
		return
	}
	row := protocol.Session{Engine: engine, ID: session, NodeID: s.Ident.ID, NodeName: s.Name}
	_, _, _, _, sessions, _ := s.webCatalog()
	for _, prev := range sessions {
		if prev.Engine == engine && prev.ID == session {
			row = prev
			break
		}
	}
	markGit(&row)
	s.bus.publish(busMsg{Type: "sessions", Sessions: []protocol.Session{row}, NodeID: s.Ident.ID})
}

func (s *Server) sendWatchHistory(write func(any) error, engine, session string) {
	if session == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	evs, err := s.sessionHistory(ctx, engine, session, 8000)
	if err != nil {
		s.Log.Debug("history", "session", session, "err", err)
		return
	}
	evs = driver.LastTurns(evs, 20)
	_ = write(busMsg{
		Type:    "conversation",
		NodeID:  s.Ident.ID,
		Engine:  engine,
		Session: session,
		Events:  evs,
		Reset:   true,
	})
}
