package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"agent-mesh/internal/i18n"
	"agent-mesh/internal/openai"
	"agent-mesh/internal/protocol"

	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type ingestHub struct {
	mu   sync.Mutex
	next int
	subs map[int]chan openai.Chunk
}

func newIngestHub() *ingestHub {
	return &ingestHub{subs: make(map[int]chan openai.Chunk)}
}

func (h *ingestHub) subscribe() (int, <-chan openai.Chunk) {
	ch := make(chan openai.Chunk, 1024)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	id := h.next
	h.subs[id] = ch
	return id, ch
}

func (h *ingestHub) unsubscribe(id int) {
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

func (h *ingestHub) publish(chunk openai.Chunk) {
	h.mu.Lock()
	type sub struct {
		id int
		ch chan openai.Chunk
	}
	subs := make([]sub, 0, len(h.subs))
	for id, ch := range h.subs {
		subs = append(subs, sub{id: id, ch: ch})
	}
	h.mu.Unlock()
	var drop []int
	for _, item := range subs {
		func(item sub) {
			defer func() { _ = recover() }()
			timer := time.NewTimer(2 * time.Second)
			defer timer.Stop()
			select {
			case item.ch <- chunk:
			case <-timer.C:
				drop = append(drop, item.id)
			}
		}(item)
	}
	for _, id := range drop {
		h.unsubscribe(id)
	}
}

func (s *Server) mountOpenAI(mux *http.ServeMux) {
	if s.ingest == nil {
		s.ingest = newIngestHub()
	}
	if s.catalogs == nil {
		s.catalogs = newCatalogHub()
	}
	mux.HandleFunc("GET /v1/models", s.handleOpenModels)
	mux.HandleFunc("GET /v1/openai/engines", s.handleOpenEngines)
	mux.HandleFunc("GET /v1/openai/sessions", s.handleOpenSessions)
	mux.HandleFunc("GET /v1/openai/events", s.handleOpenEvents)
	mux.HandleFunc("GET /v1/openai/ws", s.handleOpenWS)
	mux.HandleFunc("GET /v1/openai/directories", s.handleOpenDirectories)
	mux.HandleFunc("GET /v1/openai/history", s.handleOpenHistory)
	mux.HandleFunc("GET /v1/openai/commands", s.handleOpenCommands)
	mux.HandleFunc("GET /v1/openai/git", s.handleOpenGitRepos)
	mux.HandleFunc("GET /v1/openai/git/diff", s.handleOpenGitDiff)
	mux.HandleFunc("GET /v1/openai/file", s.handleOpenInboxFile)
	mux.HandleFunc("POST /v1/openai/stop", s.handleOpenStop)
	mux.HandleFunc("POST /v1/openai/choice", s.handleOpenChoice)
	mux.HandleFunc("GET /v1/openai/followups", s.handleOpenFollowups)
	mux.HandleFunc("POST /v1/openai/followups", s.handleOpenFollowups)
	mux.HandleFunc("PUT /v1/openai/followups", s.handleOpenFollowups)
	mux.HandleFunc("DELETE /v1/openai/followups", s.handleOpenFollowups)
	mux.HandleFunc("POST /v1/openai/ingest", s.handleIngest)
	mux.HandleFunc("GET /v1/openai/mesh", s.handleOpenMesh)
	mux.HandleFunc("POST /v1/chat/completions", s.handleOpenChat)
	mux.HandleFunc("GET /v1/to/{id}/v1/models", s.proxyNode("GET", "/v1/models"))
	mux.HandleFunc("GET /v1/to/{id}/v1/openai/sessions", s.proxyNode("GET", "/v1/openai/sessions"))
	mux.HandleFunc("GET /v1/to/{id}/v1/openai/engines", s.proxyNode("GET", "/v1/openai/engines"))
	mux.HandleFunc("GET /v1/to/{id}/v1/openai/dirs", s.proxyNode("GET", "/v1/openai/dirs"))
	mux.HandleFunc("GET /v1/to/{id}/v1/openai/events", s.proxyNodeStream("GET", "/v1/openai/events"))
	mux.HandleFunc("GET /v1/to/{id}/v1/openai/commands", s.proxyNode("GET", "/v1/openai/commands"))
	mux.HandleFunc("GET /v1/to/{id}/v1/openai/git", s.proxyNode("GET", "/v1/openai/git"))
	mux.HandleFunc("GET /v1/to/{id}/v1/openai/git/diff", s.proxyNode("GET", "/v1/openai/git/diff"))
	mux.HandleFunc("GET /v1/to/{id}/v1/openai/file", s.proxyNode("GET", "/v1/openai/file"))
	mux.HandleFunc("POST /v1/to/{id}/v1/openai/stop", s.proxyNode("POST", "/v1/openai/stop"))
	mux.HandleFunc("POST /v1/to/{id}/v1/openai/choice", s.proxyNode("POST", "/v1/openai/choice"))
	mux.HandleFunc("GET /v1/to/{id}/v1/openai/followups", s.proxyNode("GET", "/v1/openai/followups"))
	mux.HandleFunc("POST /v1/to/{id}/v1/openai/followups", s.proxyNode("POST", "/v1/openai/followups"))
	mux.HandleFunc("PUT /v1/to/{id}/v1/openai/followups", s.proxyNode("PUT", "/v1/openai/followups"))
	mux.HandleFunc("DELETE /v1/to/{id}/v1/openai/followups", s.proxyNode("DELETE", "/v1/openai/followups"))
	mux.HandleFunc("POST /v1/to/{id}/v1/chat/completions", s.proxyNode("POST", "/v1/chat/completions"))
}

func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	// Session plaintext must not land on the relay; Web E2E uses Envelope streams instead.
	io.Copy(io.Discard, r.Body)
	w.WriteHeader(http.StatusNoContent)
}

// handleOpenMesh answers the desktop settings probe; relay itself is not a local node.
func (s *Server) handleOpenMesh(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"local": false})
}

func writePipeErr(w http.ResponseWriter, err error) {
	if err == nil || pipeHasStarted(err) {
		return
	}
	http.Error(w, err.Error(), http.StatusBadGateway)
}

func (s *Server) handleOpenEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		i18n.Error(w, r, http.StatusInternalServerError, "http.streamUnsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	id, ch := s.ingest.subscribe()
	defer s.ingest.unsubscribe(id)
	for {
		select {
		case <-r.Context().Done():
			return
		case chunk, ok := <-ch:
			if !ok {
				return
			}
			raw, _ := json.Marshal(chunk)
			if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

func (s *Server) handleOpenWS(w http.ResponseWriter, r *http.Request) {
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
	id, ch := s.ingest.subscribe()
	defer s.ingest.unsubscribe(id)
	cid, cch := s.catalogs.subscribe()
	defer s.catalogs.unsubscribe(cid)
	go func() {
		for _, msg := range s.snapshotParts() {
			if err := write(msg); err != nil {
				return
			}
		}
	}()
	go func() {
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				Type    string `json:"type"`
				Node    string `json:"node"`
				Engine  string `json:"engine"`
				Session string `json:"session"`
			}
			if json.Unmarshal(raw, &msg) != nil {
				continue
			}
			switch msg.Type {
			case "refresh":
				for _, part := range s.snapshotParts() {
					_ = write(part)
				}
			case "watch":
				// Do not pull plaintext history onto the relay; browser E2E loads history via Envelope RPC.
				finger := s.sessionFinger(msg.Node, msg.Engine, msg.Session)
				s.catalogs.setWatch(cid, msg.Node, msg.Engine, msg.Session, finger)
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
			cp := chunk
			if err := write(BusMsg{Type: "chunk", Chunk: &cp}); err != nil {
				return
			}
		case msg, ok := <-cch:
			if !ok {
				return
			}
			if err := write(msg); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleOpenDirectories(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.catalog().Dirs)
}

func (s *Server) handleOpenGitRepos(w http.ResponseWriter, r *http.Request) {
	s.proxyOpenJSON(w, r, "/v1/openai/git")
}

func (s *Server) handleOpenGitDiff(w http.ResponseWriter, r *http.Request) {
	s.proxyOpenJSON(w, r, "/v1/openai/git/diff")
}

func (s *Server) handleOpenInboxFile(w http.ResponseWriter, r *http.Request) {
	rec, err := s.lookupNode(r.URL.Query().Get("node"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	path := "/v1/openai/file"
	if q := r.URL.RawQuery; q != "" {
		path += "?" + q
	}
	writePipeErr(w, s.pipeNode(r.Context(), rec, http.MethodGet, path, nil, w))
}

func (s *Server) handleOpenStop(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Node string `json:"node"`
	}
	_ = json.Unmarshal(body, &req)
	rec, err := s.lookupNode(req.Node)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writePipeErr(w, s.pipeNode(r.Context(), rec, http.MethodPost, "/v1/openai/stop", body, w))
}

func (s *Server) handleOpenChoice(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Node string `json:"node"`
	}
	_ = json.Unmarshal(body, &req)
	rec, err := s.lookupNode(req.Node)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writePipeErr(w, s.pipeNode(r.Context(), rec, http.MethodPost, "/v1/openai/choice", body, w))
}

func (s *Server) handleOpenFollowups(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.proxyOpenJSON(w, r, "/v1/openai/followups")
		return
	}
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Node string `json:"node"`
	}
	_ = json.Unmarshal(body, &req)
	nodeID := req.Node
	if nodeID == "" {
		nodeID = r.URL.Query().Get("node")
	}
	rec, err := s.lookupNode(nodeID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	path := "/v1/openai/followups"
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}
	writePipeErr(w, s.pipeNode(r.Context(), rec, r.Method, path, body, w))
}

func (s *Server) proxyOpenJSON(w http.ResponseWriter, r *http.Request, path string) {
	rec, err := s.lookupNode(r.URL.Query().Get("node"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	q := r.URL.RawQuery
	if q != "" {
		path += "?" + q
	}
	raw, err := s.getJSON(r, rec, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *Server) handleOpenCommands(w http.ResponseWriter, r *http.Request) {
	rec, err := s.lookupNode(r.URL.Query().Get("node"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	q := r.URL.RawQuery
	path := "/v1/openai/commands"
	if q != "" {
		path += "?" + q
	}
	raw, err := s.getJSON(r, rec, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *Server) lookupNode(nodeID string) (Record, error) {
	if nodeID != "" {
		return s.lookup(nodeID)
	}
	rows := s.Dir.List()
	if len(rows) == 1 {
		return rows[0], nil
	}
	return Record{}, fmt.Errorf("unknown node")
}

func (s *Server) handleOpenHistory(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("node")
	rec, err := s.lookupNode(nodeID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	q := r.URL.RawQuery
	path := "/v1/openai/history"
	if q != "" {
		path += "?" + q
	}
	raw, err := s.getJSON(r, rec, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *Server) handleOpenModels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, openai.ModelList{Object: "list", Data: s.catalog().Models})
}

func (s *Server) handleOpenEngines(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.catalog().Engines)
}

func (s *Server) handleOpenSessions(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "1" || r.URL.Query().Get("all") == "true"
	engine := r.URL.Query().Get("engine")
	out := []protocol.Session{}
	for _, sess := range s.catalog().Sessions {
		if engine != "" && sess.Engine != engine {
			continue
		}
		if !all && !sess.Live && !sess.Busy {
			continue
		}
		out = append(out, sess)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleOpenChat(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req openai.ChatRequest
	if json.Unmarshal(body, &req) != nil {
		i18n.Error(w, r, http.StatusBadRequest, "http.badJSON")
		return
	}
	nodeID, model := splitNodeModel(req.Model, s.Dir.List())
	if nodeID == "" {
		i18n.Error(w, r, http.StatusBadRequest, "http.modelMustLookLikeNode")
		return
	}
	req.Model = model
	raw, _ := json.Marshal(req)
	rec, err := s.lookup(nodeID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writePipeErr(w, s.pipeNode(r.Context(), rec, http.MethodPost, "/v1/chat/completions", raw, w))
}

func splitNodeModel(model string, nodes []Record) (nodeID, rest string) {
	model = strings.TrimSpace(model)
	for _, rec := range nodes {
		if strings.HasPrefix(model, rec.Info.ID+"/") {
			return rec.Info.ID, strings.TrimPrefix(model, rec.Info.ID+"/")
		}
	}
	if len(nodes) == 1 {
		return nodes[0].Info.ID, model
	}
	return "", model
}

func (s *Server) getJSON(r *http.Request, rec Record, path string) ([]byte, error) {
	return s.getPath(r.Context(), rec, path)
}

func (s *Server) getPath(ctx context.Context, rec Record, path string) ([]byte, error) {
	if s.tunnels != nil && s.tunnels.get(rec.Info.ID) != nil {
		status, body, err := s.tunnels.Do(ctx, rec.Info.ID, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		if status >= 400 {
			return nil, fmt.Errorf("http %d", status)
		}
		return body, nil
	}
	u := strings.TrimRight(rec.Advertise, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	cli := s.Client
	cp := http.Client{Timeout: 3 * time.Second}
	if cli != nil {
		cp.Transport = cli.Transport
	}
	cli = &cp
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return b, nil
}

func (s *Server) pipeNode(ctx context.Context, rec Record, method, path string, body []byte, w http.ResponseWriter) error {
	return s.pipeNodeHdr(ctx, rec, method, path, body, w, nil)
}

func (s *Server) pipeNodeHdr(ctx context.Context, rec Record, method, path string, body []byte, w http.ResponseWriter, hdr http.Header) error {
	if s.tunnels != nil && s.tunnels.get(rec.Info.ID) != nil {
		err := s.tunnels.PipeHdr(ctx, rec.Info.ID, method, path, body, w, hdr)
		if err == nil {
			return nil
		}
		if pipeHasStarted(err) {
			return err
		}
		s.Log.Debug("tunnel pipe", "node", rec.Info.ID, "path", path, "err", err)
	}
	return s.httpPipe(ctx, rec, method, path, body, w, hdr)
}

func (s *Server) httpPipe(ctx context.Context, rec Record, method, path string, body []byte, w http.ResponseWriter, hdr http.Header) error {
	u := strings.TrimRight(rec.Advertise, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	ct := "application/json"
	if hdr != nil {
		if v := hdr.Get("Content-Type"); v != "" {
			ct = v
		}
		if v := hdr.Get("Accept"); v != "" {
			req.Header.Set("Accept", v)
		}
	}
	req.Header.Set("Content-Type", ct)
	if req.Header.Get("Accept") == "" &&
		(strings.Contains(path, "/chat/completions") || strings.Contains(path, "/stream") || strings.Contains(path, "/events")) {
		req.Header.Set("Accept", "text/event-stream")
	}
	cli := s.Client
	if cli != nil && cli.Timeout != 0 {
		cp := *cli
		cp.Timeout = 0
		cli = &cp
	}
	if cli == nil {
		cli = http.DefaultClient
	}
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		if strings.EqualFold(k, "Content-Type") || strings.EqualFold(k, "Cache-Control") {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
	}
	w.WriteHeader(resp.StatusCode)
	fl, _ := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return &pipeStatus{error: werr, started: true}
			}
			if fl != nil {
				fl.Flush()
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return &pipeStatus{error: err, started: true}
		}
	}
}

func (s *Server) proxyNode(method, path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		rec, err := s.lookup(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		u := path
		if r.URL.RawQuery != "" {
			u += "?" + r.URL.RawQuery
		}
		writePipeErr(w, s.pipeNode(r.Context(), rec, method, u, body, w))
	}
}

func (s *Server) proxyNodeStream(method, path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		rec, err := s.lookup(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		u := path
		if r.URL.RawQuery != "" {
			u += "?" + r.URL.RawQuery
		}
		writePipeErr(w, s.pipeNode(r.Context(), rec, method, u, nil, w))
	}
}
