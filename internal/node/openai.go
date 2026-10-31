package node

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
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
	mux.HandleFunc("POST /v1/chat/completions", s.handleChat)
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
	id, ch := s.Hub.Subscribe(64)
	defer s.Hub.Unsubscribe(id)
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	for {
		select {
		case <-r.Context().Done():
			return
		case chunk, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.WriteJSON(chunk); err != nil {
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
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "engine": name, "enabled": body.Enabled})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	engine := r.URL.Query().Get("engine")
	all := r.URL.Query().Get("all") == "1" || r.URL.Query().Get("all") == "true"
	if snap := s.peekSnapshot(); len(snap.Sessions) > 0 || len(snap.Engines) > 0 {
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
	if since <= 0 {
		since = time.Now().Add(-48 * time.Hour).Unix()
	}
	d, err := s.Drivers.Get(engine)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	evs, err := d.History(r.Context(), sid, tail)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, driver.LastTurns(driver.FilterSince(evs, since), turns))
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
		http.Error(w, "需要 cwd", http.StatusBadRequest)
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
		http.Error(w, "需要 repo", http.StatusBadRequest)
		return
	}
	bundle, err := gitdiff.Diff(r.Context(), repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, bundle)
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
		http.Error(w, "缺少选项内容", http.StatusBadRequest)
		return
	}
	if req.Engine == "" {
		http.Error(w, "需要 engine", http.StatusBadRequest)
		return
	}
	d, err := s.Drivers.Get(req.Engine)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	var evs <-chan protocol.Event
	if req.Session != "" {
		evs, err = d.Reply(r.Context(), req.Session, req.Text)
	} else {
		evs, err = d.Send(r.Context(), req.CWD, req.Text, "")
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	go func() {
		for range evs {
		}
	}()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req openai.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	engine, _ := openai.SplitModel(req.Model, s.Drivers.Names())
	if req.Engine != "" {
		engine = req.Engine
	}
	if engine == "" {
		http.Error(w, "model 需要形如 cursor 或 cursor/composer-2.5", http.StatusBadRequest)
		return
	}
	d, err := s.Drivers.Get(engine)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	text := openai.LastUserText(req.Messages)
	files := openai.CollectFiles(req)
	if text == "" && len(files) == 0 {
		http.Error(w, "messages 缺少用户文本", http.StatusBadRequest)
		return
	}
	text, err = openai.ApplyFiles(req.CWD, text, files)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Session != "" && s.sessionBusy(r.Context(), engine, req.Session, req.CWD) {
		queued, qerr := followup.Push(engine, req.Session, req.CWD, text)
		if qerr != nil {
			http.Error(w, qerr.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"queued": true, "followups": queued})
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
	var evs <-chan protocol.Event
	if req.Session != "" {
		evs, err = d.Reply(r.Context(), req.Session, text)
	} else {
		evs, err = d.Send(r.Context(), req.CWD, text, "")
	}
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
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		for chunk := range chunks {
			note(chunk)
			if err := writeOpenAIChunk(w, fl, chunk); err != nil {
				return
			}
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		fl.Flush()
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
