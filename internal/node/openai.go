package node

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/followup"
	"agent-mesh/internal/gitdiff"
	"agent-mesh/internal/openai"
	"agent-mesh/internal/protocol"

	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (s *Server) mountOpenAI(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/models", s.handleModels)
	mux.HandleFunc("GET /v1/openai/engines", s.handleEngines)
	mux.HandleFunc("POST /v1/openai/engines/{name}/enabled", s.handleEngineToggle)
	mux.HandleFunc("GET /v1/openai/sessions", s.handleSessions)
	mux.HandleFunc("GET /v1/openai/history", s.handleOpenHistory)
	mux.HandleFunc("GET /v1/openai/events", s.handleEvents)
	mux.HandleFunc("GET /v1/openai/stream", s.handleOpenStream)
	mux.HandleFunc("GET /v1/openai/directories", s.handleDirectories)
	mux.HandleFunc("GET /v1/openai/commands", s.handleCommands)
	mux.HandleFunc("GET /v1/openai/ws", s.handleWS)
	mux.HandleFunc("GET /v1/openai/git", s.handleGitRepos)
	mux.HandleFunc("GET /v1/openai/git/diff", s.handleGitDiff)
	mux.HandleFunc("POST /v1/openai/choice", s.handleChoice)
	mux.HandleFunc("GET /v1/openai/followups", s.handleFollowups)
	mux.HandleFunc("POST /v1/openai/followups", s.handleFollowups)
	mux.HandleFunc("PUT /v1/openai/followups", s.handleFollowups)
	mux.HandleFunc("DELETE /v1/openai/followups", s.handleFollowups)
	mux.HandleFunc("GET /v1/openai/mesh", s.handleMesh)
	mux.HandleFunc("PUT /v1/openai/mesh", s.handleMesh)
	mux.HandleFunc("POST /v1/openai/mesh/test", s.handleMeshTest)
	mux.HandleFunc("POST /v1/chat/completions", s.handleChat)
}

func (s *Server) pushIngestChunk(chunk openai.Chunk) bool {
	raw, err := json.Marshal(chunk)
	if err != nil {
		return false
	}
	s.mu.Lock()
	conn, wmu := s.tunConn.conn, s.tunConn.wmu
	s.mu.Unlock()
	if conn == nil || wmu == nil {
		return false
	}
	wmu.Lock()
	defer wmu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteJSON(protocol.TunnelFrame{Path: "/v1/openai/ingest", Body: raw}); err != nil {
		return false
	}
	return true
}

func (s *Server) handleCommands(w http.ResponseWriter, r *http.Request) {
	engine := r.URL.Query().Get("engine")
	cwd := r.URL.Query().Get("cwd")
	writeJSON(w, http.StatusOK, s.Drivers.SlashItems(r.Context(), engine, cwd))
}

func (s *Server) handleDirectories(w http.ResponseWriter, r *http.Request) {
	if snap := s.peekSnapshot(); snap.Dirs != nil {
		writeJSON(w, http.StatusOK, snap.Dirs)
		return
	}
	writeJSON(w, http.StatusOK, s.Drivers.Directories(r.Context()))
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	var wmu sync.Mutex
	write := func(v any) error {
		wmu.Lock()
		defer wmu.Unlock()
		return conn.WriteJSON(v)
	}
	hid, hch := s.Hub.Subscribe(64)
	defer s.Hub.Unsubscribe(hid)
	bid, bch := s.bus.subscribe()
	defer s.bus.unsubscribe(bid)
	for _, msg := range s.catalogParts() {
		if err := write(msg); err != nil {
			return
		}
	}
	go func() {
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				Type    string `json:"type"`
				Engine  string `json:"engine"`
				Session string `json:"session"`
			}
			if json.Unmarshal(raw, &msg) != nil {
				continue
			}
			switch msg.Type {
			case "refresh":
				for _, part := range s.catalogParts() {
					_ = write(part)
				}
			case "watch":
				s.sendWatchHistory(write, msg.Engine, msg.Session)
			}
		}
	}()
	for {
		select {
		case <-r.Context().Done():
			return
		case chunk, ok := <-hch:
			if !ok {
				return
			}
			cp := chunk
			if err := write(busMsg{Type: "chunk", Chunk: &cp}); err != nil {
				return
			}
		case msg, ok := <-bch:
			if !ok {
				return
			}
			if err := write(msg); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	engine := r.URL.Query().Get("engine")
	if snap := s.peekSnapshot(); len(snap.Models) > 0 {
		rows := snap.Models
		if engine != "" {
			var filtered []protocol.Model
			for _, m := range rows {
				if m.Engine == engine {
					filtered = append(filtered, m)
				}
			}
			rows = filtered
		}
		writeJSON(w, http.StatusOK, openai.FromModels(rows))
		return
	}
	rows, err := s.Drivers.Models(r.Context(), engine)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, openai.FromModels(rows))
}

func (s *Server) handleEngines(w http.ResponseWriter, r *http.Request) {
	if snap := s.peekSnapshot(); len(snap.Engines) > 0 {
		writeJSON(w, http.StatusOK, snap.Engines)
		return
	}
	writeJSON(w, http.StatusOK, s.Drivers.Engines(r.Context()))
}

func (s *Server) handleEngineToggle(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if err := s.Drivers.SetEnabled(name, body.Enabled); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	go s.refreshSnapshot(context.Background())
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "engine": name, "enabled": body.Enabled})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	engine := r.URL.Query().Get("engine")
	all := r.URL.Query().Get("all") == "1" || r.URL.Query().Get("all") == "true"
	if snap := s.peekSnapshot(); len(snap.Sessions) > 0 {
		out := []protocol.Session{}
		for _, sess := range snap.Sessions {
			if engine != "" && sess.Engine != engine {
				continue
			}
			if !all && !sess.Live && !sess.Busy {
				continue
			}
			if sess.NodeID == "" {
				sess.NodeID = s.Ident.ID
				sess.NodeName = s.Name
			}
			out = append(out, sess)
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	rows, err := s.Drivers.ListAll(r.Context(), engine, all)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if rows == nil {
		rows = []protocol.Session{}
	}
	for i := range rows {
		rows[i].NodeID = s.Ident.ID
		rows[i].NodeName = s.Name
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) handleOpenHistory(w http.ResponseWriter, r *http.Request) {
	engine := r.URL.Query().Get("engine")
	sid := r.URL.Query().Get("session")
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	if tail <= 0 {
		tail = 8000
	}
	turns, _ := strconv.Atoi(r.URL.Query().Get("turns"))
	if turns <= 0 {
		turns = 20
	}
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	evs, err := s.sessionHistory(r.Context(), engine, sid, tail)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if since > 0 {
		evs = driver.FilterSince(evs, since)
	}
	writeJSON(w, http.StatusOK, driver.LastTurns(evs, turns))
}

func (s *Server) sessionHistory(ctx context.Context, engine, sid string, tail int) ([]protocol.Event, error) {
	names := make([]string, 0, 2)
	if engine != "" {
		names = append(names, engine)
	}
	if sid != "" {
		if _, name, ok := s.driverForSession(ctx, sid, engine); ok {
			if name != engine {
				names = append(names, name)
			}
		}
	}
	var last error
	for _, name := range names {
		d, err := s.Drivers.Get(name)
		if err != nil {
			last = err
			continue
		}
		evs, err := d.History(ctx, sid, tail)
		if err == nil {
			if evs == nil {
				evs = []protocol.Event{}
			}
			return evs, nil
		}
		last = err
		if !sessionGone(err) {
			return nil, err
		}
	}
	if last == nil || sessionGone(last) {
		return []protocol.Event{}, nil
	}
	return nil, last
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	id, ch := s.Hub.Subscribe(64)
	defer s.Hub.Unsubscribe(id)
	for {
		select {
		case <-r.Context().Done():
			return
		case chunk, ok := <-ch:
			if !ok {
				return
			}
			if err := writeOpenAIChunk(w, fl, chunk); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleOpenStream(w http.ResponseWriter, r *http.Request) {
	engine := r.URL.Query().Get("engine")
	sid := r.URL.Query().Get("session")
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	ch, err := s.Hub.WatchOne(r.Context(), engine, sid)
	if err != nil {
		_, _ = fmt.Fprintf(w, "data: {\"error\":%q}\n\n", err.Error())
		fl.Flush()
		return
	}
	for chunk := range ch {
		if err := writeOpenAIChunk(w, fl, chunk); err != nil {
			return
		}
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	fl.Flush()
}

func (s *Server) handleGitRepos(w http.ResponseWriter, r *http.Request) {
	cwd := r.URL.Query().Get("cwd")
	if cwd == "" {
		http.Error(w, "cwd is required", http.StatusBadRequest)
		return
	}
	rows, err := gitdiff.List(r.Context(), cwd)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if rows == nil {
		rows = []gitdiff.Repo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": rows})
}

func (s *Server) handleGitDiff(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	if repo == "" {
		repo = r.URL.Query().Get("cwd")
	}
	if repo == "" {
		http.Error(w, "repo is required", http.StatusBadRequest)
		return
	}
	bundle, err := gitdiff.Diff(r.Context(), repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, bundle)
}

func (s *Server) dispatchChoice(ctx context.Context, req protocol.Request) protocol.Response {
	var p protocol.ChoiceParams
	_ = json.Unmarshal(req.Params, &p)
	p.Text = strings.TrimSpace(p.Text)
	if p.Text == "" {
		return protocol.Response{ID: req.ID, Error: "missing choice text"}
	}
	if p.Engine == "" {
		return protocol.Response{ID: req.ID, Error: "engine is required"}
	}
	runCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	_, engine, evs, err := s.startPrompt(runCtx, p.Engine, p.Session, p.CWD, p.Text, "")
	if err != nil {
		stop()
		return protocol.Response{ID: req.ID, Error: err.Error()}
	}
	go func() {
		defer stop()
		if s.Hub == nil || evs == nil {
			return
		}
		for range s.Hub.Pump(engine, p.Session, engine, evs) {
		}
	}()
	return protocol.Response{ID: req.ID, OK: true, Body: protocol.MustJSON(map[string]any{"ok": true})}
}

func (s *Server) handleChoice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Engine  string `json:"engine"`
		Session string `json:"session"`
		CWD     string `json:"cwd"`
		Text    string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		http.Error(w, "missing choice text", http.StatusBadRequest)
		return
	}
	if req.Engine == "" {
		http.Error(w, "engine is required", http.StatusBadRequest)
		return
	}
	runCtx, stop := context.WithCancel(context.WithoutCancel(r.Context()))
	_, engine, evs, err := s.startPrompt(runCtx, req.Engine, req.Session, req.CWD, req.Text, "")
	if err != nil {
		stop()
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	go func() {
		defer stop()
		if s.Hub == nil || evs == nil {
			return
		}
		for range s.Hub.Pump(engine, req.Session, engine, evs) {
		}
	}()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// localModelID strips this node's id/ prefix so Vue can send the same
// nodeID/engine/name ids the relay catalog uses.
func (s *Server) localModelID(model string) string {
	model = strings.TrimSpace(model)
	if s.Ident.ID == "" {
		return model
	}
	prefix := s.Ident.ID + "/"
	if strings.HasPrefix(model, prefix) {
		return strings.TrimPrefix(model, prefix)
	}
	return model
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req openai.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	req.Model = s.localModelID(req.Model)
	engine, modelName := openai.EngineModel(req.Model, req.Engine, s.Drivers.Names())
	if engine == "" {
		http.Error(w, "model must look like cursor or cursor/composer-2.5", http.StatusBadRequest)
		return
	}
	text := openai.LastUserText(req.Messages)
	files := openai.CollectFiles(req)
	if text == "" && len(files) == 0 {
		http.Error(w, "messages missing user text", http.StatusBadRequest)
		return
	}
	text, err := openai.ApplyFiles(req.CWD, text, files)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Session != "" {
		if _, name, ok := s.driverForSession(r.Context(), req.Session, engine); ok {
			if name != engine {
				modelName = ""
			}
			engine = name
		}
	}
	if req.Session != "" && s.sessionBusy(r.Context(), engine, req.Session, req.CWD) {
		queued, qerr := followup.Push(engine, req.Session, req.CWD, text)
		if qerr != nil {
			http.Error(w, qerr.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"queued": true, "followups": queued})
		s.publishFollow(queued)
		go s.drainSession(engine, req.Session, req.CWD)
		return
	}
	runKey := req.Session
	if runKey == "" && req.CWD != "" {
		runKey = "cwd:" + req.CWD
	}
	sid := req.Session
	if runKey != "" {
		s.followups().begin(engine, runKey)
		defer s.followups().end(engine, runKey)
		defer func() {
			if sid != "" {
				go s.drainSession(engine, sid, req.CWD)
			}
		}()
	}
	// a dropped browser must not kill the engine process; progress continues over broadcast / WebSocket.
	// cancel this turn's Watch after Pump; long-term follow stays with Hub.runWatch.
	runCtx, stop := context.WithCancel(context.WithoutCancel(r.Context()))
	defer stop()
	_, engine, evs, err := s.startPrompt(runCtx, engine, req.Session, req.CWD, text, modelName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	chunks := s.Hub.Pump(engine, req.Session, req.Model, evs)
	note := func(chunk openai.Chunk) {
		if chunk.Mesh.Session != "" {
			sid = chunk.Mesh.Session
		}
	}
	if req.Stream {
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "stream unsupported", http.StatusInternalServerError)
			for range chunks {
			}
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		broken := false
		for chunk := range chunks {
			note(chunk)
			if broken {
				continue
			}
			if err := writeOpenAIChunk(w, fl, chunk); err != nil {
				broken = true
			}
		}
		if !broken {
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			fl.Flush()
		}
		return
	}
	var b strings.Builder
	id := ""
	for chunk := range chunks {
		note(chunk)
		id = chunk.ID
		if len(chunk.Choices) > 0 {
			b.WriteString(chunk.Choices[0].Delta.Content)
		}
	}
	writeJSON(w, http.StatusOK, openai.Completion{
		ID:      id,
		Object:  "chat.completion",
		Created: 0,
		Model:   req.Model,
		Choices: []openai.CompletionChoice{{
			Index:        0,
			Message:      openai.Delta{Role: "assistant", Content: b.String()},
			FinishReason: "stop",
		}},
		Mesh: openai.Mesh{Engine: engine, Session: sid, NodeID: s.Ident.ID, NodeName: s.Name},
	})
}

func writeOpenAIChunk(w http.ResponseWriter, fl http.Flusher, chunk openai.Chunk) error {
	raw, err := json.Marshal(chunk)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
		return err
	}
	fl.Flush()
	return nil
}

func sessionGone(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "not unique or missing") || strings.Contains(msg, "has no jsonl")
}

func (s *Server) driverForSession(ctx context.Context, session, prefer string) (driver.Driver, string, bool) {
	if session == "" || s.Drivers == nil {
		return nil, "", false
	}
	if d, name, ok := s.matchSessionDriver(s.peekSnapshot().Sessions, session, prefer); ok {
		return d, name, true
	}
	rows, err := s.Drivers.ListAll(ctx, "", true)
	if err != nil {
		return nil, "", false
	}
	return s.matchSessionDriver(rows, session, prefer)
}

func (s *Server) matchSessionDriver(rows []protocol.Session, session, prefer string) (driver.Driver, string, bool) {
	var hit protocol.Session
	found := false
	for _, row := range rows {
		if row.ID != session || row.Engine == "" {
			continue
		}
		if prefer != "" && row.Engine == prefer {
			d, err := s.Drivers.Get(row.Engine)
			if err == nil {
				return d, row.Engine, true
			}
		}
		if !found {
			hit = row
			found = true
		}
	}
	if !found {
		return nil, "", false
	}
	d, err := s.Drivers.Get(hit.Engine)
	if err != nil {
		return nil, "", false
	}
	return d, hit.Engine, true
}

func (s *Server) startPrompt(ctx context.Context, engine, session, cwd, text, model string) (driver.Driver, string, <-chan protocol.Event, error) {
	d, err := s.Drivers.Get(engine)
	if err != nil {
		return nil, engine, nil, err
	}
	model = driver.CLIModel(model, engine)
	if session == "" {
		evs, err := d.Send(ctx, cwd, text, "", model)
		return d, engine, evs, err
	}
	evs, err := d.Reply(ctx, session, text, model)
	if err == nil {
		return d, engine, evs, nil
	}
	if alt, name, ok := s.driverForSession(ctx, session, engine); ok && name != engine {
		evs, err = alt.Reply(ctx, session, text, "")
		if err == nil {
			return alt, name, evs, nil
		}
		d, engine = alt, name
	}
	if sessionGone(err) {
		evs, err = d.Send(ctx, cwd, text, session, model)
	}
	return d, engine, evs, err
}
