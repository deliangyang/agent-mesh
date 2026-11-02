package node

import (
	"context"
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
		if !e.Available {
			continue
		}
		engines = append(engines, engineRow{EngineInfo: e, NodeID: info.ID, NodeName: info.Name})
	}
	models = openai.FromModels(snap.Models).Data
	if models == nil {
		models = []openai.ModelCard{}
	}
	// Match the relay catalog: Vue filters models by nodeID/… prefix.
	for i := range models {
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

func (s *Server) catalogParts() []busMsg {
	nodes, engines, models, dirs, sessions, followups := s.webCatalog()
	return []busMsg{
		{Type: "sessions", Sessions: sessions, Reset: true},
		{Type: "nodes", Nodes: nodes, Reset: true},
		{Type: "engines", Engines: engines, Reset: true},
		{Type: "models", Models: models, Reset: true},
		{Type: "dirs", Dirs: dirs, Reset: true},
		{Type: "followups", Followups: followups, Reset: true},
	}
}

func (s *Server) publishCatalog() {
	if s.bus == nil {
		return
	}
	for _, msg := range s.catalogParts() {
		s.bus.publish(msg)
	}
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
