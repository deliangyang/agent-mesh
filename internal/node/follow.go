package node

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"agent-mesh/internal/followup"
	"agent-mesh/internal/protocol"
)

type followRuns struct {
	mu     sync.Mutex
	active map[string]int
	drain  map[string]bool
}

func newFollowRuns() *followRuns {
	return &followRuns{active: map[string]int{}, drain: map[string]bool{}}
}

func followKey(engine, session string) string { return engine + "\x00" + session }

func (r *followRuns) begin(engine, session string) {
	if engine == "" || session == "" {
		return
	}
	r.mu.Lock()
	r.active[followKey(engine, session)]++
	r.mu.Unlock()
}

func (r *followRuns) end(engine, session string) {
	if engine == "" || session == "" {
		return
	}
	r.mu.Lock()
	k := followKey(engine, session)
	n := r.active[k] - 1
	if n <= 0 {
		delete(r.active, k)
	} else {
		r.active[k] = n
	}
	r.mu.Unlock()
}

func (r *followRuns) running(engine, session string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active[followKey(engine, session)] > 0
}

func (s *Server) followups() *followRuns {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.follow == nil {
		s.follow = newFollowRuns()
	}
	return s.follow
}

func (s *Server) sessionBusy(ctx context.Context, engine, session, cwd string) bool {
	runs := s.followups()
	if session != "" && runs.running(engine, session) {
		return true
	}
	if cwd != "" && runs.running(engine, "cwd:"+cwd) {
		return true
	}
	if session == "" {
		return false
	}
	d, err := s.Drivers.Get(engine)
	if err != nil {
		return false
	}
	rows, err := d.List(ctx, true)
	if err != nil {
		return false
	}
	for _, row := range rows {
		if row.ID == session {
			return row.Busy
		}
	}
	return false
}

func (s *Server) handleFollowups(w http.ResponseWriter, r *http.Request) {
	engine := r.URL.Query().Get("engine")
	session := r.URL.Query().Get("session")
	id := r.URL.Query().Get("id")
	cwd := r.URL.Query().Get("cwd")
	var body struct {
		Engine  string `json:"engine"`
		Session string `json:"session"`
		CWD     string `json:"cwd"`
		Text    string `json:"text"`
		ID      string `json:"id"`
	}
	if r.Method != http.MethodGet && r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if body.Engine != "" {
		engine = body.Engine
	}
	if body.Session != "" {
		session = body.Session
	}
	if body.CWD != "" {
		cwd = body.CWD
	}
	if body.ID != "" {
		id = body.ID
	}
	var (
		out followup.File
		err error
	)
	switch r.Method {
	case http.MethodGet:
		out, err = followup.List(engine, session)
	case http.MethodPost:
		out, err = followup.Push(engine, session, cwd, body.Text)
		if err == nil {
			s.publishFollow(out)
			go s.drainSession(engine, session, cwd)
		}
	case http.MethodPut:
		out, err = followup.Update(engine, session, id, body.Text)
		if err == nil {
			s.publishFollow(out)
		}
	case http.MethodDelete:
		out, err = followup.Delete(engine, session, id)
		if err == nil {
			s.publishFollow(out)
		}
	default:
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	if err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "不存在") {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) dispatchFollowups(_ context.Context, req protocol.Request) protocol.Response {
	var p protocol.FollowupsParams
	_ = json.Unmarshal(req.Params, &p)
	var (
		out followup.File
		err error
	)
	switch p.Action {
	case "", "list":
		out, err = followup.List(p.Engine, p.Session)
	case "push", "add":
		out, err = followup.Push(p.Engine, p.Session, p.CWD, p.Text)
		if err == nil {
			s.publishFollow(out)
			go s.drainSession(p.Engine, p.Session, p.CWD)
		}
	case "update":
		out, err = followup.Update(p.Engine, p.Session, p.ID, p.Text)
		if err == nil {
			s.publishFollow(out)
		}
	case "delete":
		out, err = followup.Delete(p.Engine, p.Session, p.ID)
		if err == nil {
			s.publishFollow(out)
		}
	default:
		return protocol.Response{ID: req.ID, Error: "unknown followups action"}
	}
	if err != nil {
		return protocol.Response{ID: req.ID, Error: err.Error()}
	}
	return protocol.Response{ID: req.ID, OK: true, Body: protocol.MustJSON(out)}
}

func (s *Server) drainAll(ctx context.Context) {
	rows, err := followup.ListAll()
	if err != nil {
		return
	}
	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}
		s.drainSession(row.Engine, row.Session, row.CWD)
	}
}

func (s *Server) drainSession(engine, session, cwd string) {
	if engine == "" || session == "" {
		return
	}
	runs := s.followups()
	k := followKey(engine, session)
	runs.mu.Lock()
	if runs.drain[k] {
		runs.mu.Unlock()
		return
	}
	runs.drain[k] = true
	runs.mu.Unlock()
	defer func() {
		runs.mu.Lock()
		delete(runs.drain, k)
		runs.mu.Unlock()
	}()
	fails := 0
	for {
		if s.sessionBusy(context.Background(), engine, session, cwd) {
			time.Sleep(400 * time.Millisecond)
			continue
		}
		it, rest, ok, err := followup.Pop(engine, session)
		if err != nil || !ok {
			return
		}
		s.publishFollow(rest)
		if err := s.dispatchFollow(engine, session, cwd, it); err != nil {
			_ = followup.Unshift(engine, session, cwd, it)
			if queued, lerr := followup.List(engine, session); lerr == nil {
				s.publishFollow(queued)
			}
			s.Log.Warn("followup send", "engine", engine, "session", session, "err", err)
			fails++
			if fails >= 5 {
				return
			}
			time.Sleep(time.Duration(fails) * time.Second)
			continue
		}
		fails = 0
	}
}

func toFollowQueue(f followup.File) protocol.FollowQueue {
	items := make([]protocol.FollowItem, 0, len(f.Items))
	for _, it := range f.Items {
		items = append(items, protocol.FollowItem{ID: it.ID, Text: it.Text, Created: it.Created})
	}
	return protocol.FollowQueue{
		Engine:  f.Engine,
		Session: f.Session,
		CWD:     f.CWD,
		Items:   items,
	}
}

func followQueues() []protocol.FollowQueue {
	rows, err := followup.ListAll()
	if err != nil || len(rows) == 0 {
		return []protocol.FollowQueue{}
	}
	out := make([]protocol.FollowQueue, 0, len(rows))
	for _, row := range rows {
		out = append(out, toFollowQueue(row))
	}
	return out
}

func patchFollowQueues(rows []protocol.FollowQueue, q protocol.FollowQueue) []protocol.FollowQueue {
	if q.Items == nil {
		q.Items = []protocol.FollowItem{}
	}
	out := make([]protocol.FollowQueue, 0, len(rows)+1)
	found := false
	for _, row := range rows {
		if row.Engine == q.Engine && row.Session == q.Session {
			found = true
			if len(q.Items) == 0 {
				continue
			}
			out = append(out, q)
			continue
		}
		out = append(out, row)
	}
	if !found && len(q.Items) > 0 {
		out = append(out, q)
	}
	return out
}

func (s *Server) publishFollow(f followup.File) {
	q := toFollowQueue(f)
	s.mu.Lock()
	s.snap.Followups = patchFollowQueues(s.snap.Followups, q)
	snap := s.snap
	raw, _ := json.Marshal(snap)
	s.snapRaw = raw
	s.mu.Unlock()
	s.pushSnapshot(snap)
}

func sessionBusyKey(engine, id string) string { return engine + "\x00" + id }

func (s *Server) drainIdle(prev, next []protocol.Session) {
	was := make(map[string]bool, len(prev))
	for _, sess := range prev {
		if sess.ID == "" {
			continue
		}
		was[sessionBusyKey(sess.Engine, sess.ID)] = sess.Busy || sess.Live
	}
	for _, sess := range next {
		if sess.ID == "" {
			continue
		}
		k := sessionBusyKey(sess.Engine, sess.ID)
		if was[k] && !sess.Busy && !sess.Live {
			go s.drainSession(sess.Engine, sess.ID, sess.CWD)
		}
	}
}

func (s *Server) dispatchFollow(engine, session, cwd string, it followup.Item) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if session != "" {
		if _, name, ok := s.driverForSession(ctx, session, engine); ok {
			engine = name
		}
	}
	s.followups().begin(engine, session)
	defer s.followups().end(engine, session)
	_, engine, evs, err := s.startPrompt(ctx, engine, session, cwd, it.Text)
	if err != nil {
		return err
	}
	if evs == nil {
		return nil
	}
	chunks := s.Hub.Pump(engine, session, engine, evs)
	for range chunks {
	}
	return nil
}
