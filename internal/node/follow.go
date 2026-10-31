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
			go s.drainSession(engine, session, cwd)
		}
	case http.MethodPut:
		out, err = followup.Update(engine, session, id, body.Text)
	case http.MethodDelete:
		out, err = followup.Delete(engine, session, id)
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

func (s *Server) followLoop(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	s.drainAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.drainAll(ctx)
		}
	}
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
	for {
		if s.sessionBusy(context.Background(), engine, session, cwd) {
			return
		}
		it, _, ok, err := followup.Pop(engine, session)
		if err != nil || !ok {
			return
		}
		if err := s.dispatchFollow(engine, session, cwd, it); err != nil {
			_ = followup.Unshift(engine, session, cwd, it)
			s.Log.Warn("followup send", "engine", engine, "session", session, "err", err)
			return
		}
	}
}

func (s *Server) dispatchFollow(engine, session, cwd string, it followup.Item) error {
	d, err := s.Drivers.Get(engine)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	s.followups().begin(engine, session)
	defer s.followups().end(engine, session)
	var evs <-chan protocol.Event
	if session != "" {
		evs, err = d.Reply(ctx, session, it.Text)
	} else {
		evs, err = d.Send(ctx, cwd, it.Text, "")
	}
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
