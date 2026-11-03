package node

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"sync"
	"time"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/e2e"
	"agent-mesh/internal/hub"
	"agent-mesh/internal/i18n"
	"agent-mesh/internal/openai"
	"agent-mesh/internal/protocol"
	meshweb "agent-mesh/internal/relay/web"

	"github.com/gorilla/websocket"
)

// Server runs on a machine with the agents and handles encrypted RPC plus the OpenAI-compatible API.
type Server struct {
	Ident   *e2e.Identity
	Listen  string
	Name    string
	Drivers *driver.Set
	Hub     *hub.Hub
	Log     *slog.Logger

	mu       sync.Mutex
	sessions map[string][]*e2e.Session // peer client id → recent sessions (newest first)
	snap     protocol.NodeSnapshot
	snapRaw  []byte
	tunConn  tunnelConn
	follow   *followRuns
	turns    *turnCancels
	bus      *busHub

	relays    []string
	password  string
	advertise string
	meshOK    bool
	meshErr   string
	regCancel context.CancelFunc
}

type tunnelConn struct {
	conn *websocket.Conn
	wmu  *sync.Mutex
}

// New creates the node server.
func New(ident *e2e.Identity, listen, name string, drivers *driver.Set, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{
		Ident:    ident,
		Listen:   listen,
		Name:     name,
		Drivers:  drivers,
		Log:      log,
		sessions: make(map[string][]*e2e.Session),
		follow:   newFollowRuns(),
		turns:    newTurnCancels(),
		snap:     protocol.NodeSnapshot{Followups: []protocol.FollowQueue{}},
		bus:      newBusHub(),
	}
	s.Hub = hub.New(drivers, ident.ID, name, log)
	s.Hub.SetPushIngest(s.pushIngestChunk)
	return s
}

// Info is public info.
func (s *Server) Info() protocol.NodeInfo {
	pub := s.Ident.Public()
	return protocol.NodeInfo{
		ID:      pub.ID,
		Name:    s.Name,
		Listen:  s.Listen,
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		Engines: s.Drivers.Names(),
		SignPub: pub.SignPub,
		X25519:  pub.X25519Pub,
	}
}

// Handler is the HTTP router.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /v1/info", s.handleInfo)
	mux.HandleFunc("POST /v1/handshake", s.handleHandshake)
	mux.HandleFunc("POST /v1/rpc", s.handleRPC)
	mux.HandleFunc("POST /v1/stream", s.handleStream)
	mux.HandleFunc("POST /login", s.handleLocalLogin)
	mux.HandleFunc("POST /logout", s.handleLocalLogin)
	s.mountOpenAI(mux)
	h := http.Handler(mux)
	if ui, err := meshweb.Attach(mux); err == nil {
		h = ui
	}
	return withCORS(withLog(s.Log, h))
}

func (s *Server) handleLocalLogin(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept-Language")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Info("http", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start))
	})
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Info())
}

func (s *Server) handleHandshake(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		i18n.Error(w, r, http.StatusBadRequest, "http.badEnvelope")
		return
	}
	env, err := e2e.DecodeEnvelope(r, body)
	if err != nil {
		i18n.Error(w, r, http.StatusBadRequest, "http.badEnvelope")
		return
	}
	bin := e2e.WantsBinary(r)
	ack, sess, err := e2e.AcceptHandshake(s.Ident, env)
	if err != nil {
		s.Log.Warn("handshake failed", "err", err)
		i18n.Error(w, r, http.StatusUnauthorized, "http.handshakeFailed")
		return
	}
	s.mu.Lock()
	s.rememberSession(sess)
	s.mu.Unlock()
	if err := e2e.WriteEnvelope(w, http.StatusOK, ack, bin); err != nil {
		s.Log.Debug("handshake write", "err", err)
	}
}

const maxSessionsPerPeer = 4

// rememberSession keeps the newest handshakes per client so a racing catalog WS
// and watch stream that finished handshake in different orders can both decrypt.
func (s *Server) rememberSession(sess *e2e.Session) {
	list := s.sessions[sess.PeerID]
	next := make([]*e2e.Session, 0, maxSessionsPerPeer)
	next = append(next, sess)
	for _, old := range list {
		if old == nil || old.Expired() || old.ID == sess.ID {
			continue
		}
		next = append(next, old)
		if len(next) >= maxSessionsPerPeer {
			break
		}
	}
	s.sessions[sess.PeerID] = next
}

func (s *Server) sessionsFor(peerID string) []*e2e.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.sessions[peerID]
	out := make([]*e2e.Session, 0, len(list))
	for _, sess := range list {
		if sess != nil && !sess.Expired() {
			out = append(out, sess)
		}
	}
	return out
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	env, sess, req, err := s.decodeRequest(r)
	_ = env
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	bin := e2e.WantsBinary(r)
	resp := s.dispatch(r.Context(), req)
	plain, _ := json.Marshal(resp)
	out, err := sess.Seal(plain)
	if err != nil {
		i18n.Error(w, r, http.StatusInternalServerError, "http.seal")
		return
	}
	out.Kind = "rpc"
	if err := e2e.WriteEnvelope(w, http.StatusOK, out, bin); err != nil {
		s.Log.Debug("rpc write", "err", err)
	}
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	_, sess, req, err := s.decodeRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		i18n.Error(w, r, http.StatusInternalServerError, "http.streamUnsupported")
		return
	}
	binStream := e2e.WantsBinaryStream(r)
	if binStream {
		w.Header().Set("Content-Type", e2e.ContentTypeEnvelopeStream)
	} else {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if req.Method == protocol.MethodCatalog {
		s.streamCatalog(r.Context(), w, flusher, sess, binStream)
		return
	}
	ch, err := s.dispatchStream(r.Context(), req)
	if err != nil {
		s.writeStream(w, flusher, sess, protocol.Event{Type: "error", Text: err.Error(), Done: true}, binStream)
		return
	}
	for ev := range ch {
		if err := s.writeStream(w, flusher, sess, ev, binStream); err != nil {
			return
		}
	}
}

// streamCatalog pushes sealed busMsg JSON (same shape as the local OpenAI WS catalog).
func (s *Server) streamCatalog(ctx context.Context, w http.ResponseWriter, fl http.Flusher, sess *e2e.Session, bin bool) {
	writeMsg := func(msg busMsg) error {
		plain, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		env, err := sess.Seal(plain)
		if err != nil {
			return err
		}
		env.Kind = "stream"
		if err := e2e.WriteStreamFrame(w, env, bin); err != nil {
			return err
		}
		fl.Flush()
		return nil
	}
	for _, msg := range s.catalogParts() {
		if err := writeMsg(msg); err != nil {
			return
		}
	}
	if s.bus == nil {
		<-ctx.Done()
		return
	}
	bid, bch := s.bus.subscribe()
	defer s.bus.unsubscribe(bid)
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-bch:
			if !ok {
				return
			}
			if err := writeMsg(msg); err != nil {
				return
			}
		}
	}
}

func (s *Server) writeStream(w http.ResponseWriter, fl http.Flusher, sess *e2e.Session, ev protocol.Event, binStream bool) error {
	plain, _ := json.Marshal(ev)
	env, err := sess.Seal(plain)
	if err != nil {
		return err
	}
	env.Kind = "stream"
	if err := e2e.WriteStreamFrame(w, env, binStream); err != nil {
		return err
	}
	fl.Flush()
	return nil
}

func (s *Server) decodeRequest(r *http.Request) (e2e.Envelope, *e2e.Session, protocol.Request, error) {
	var req protocol.Request
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return e2e.Envelope{}, nil, req, fmt.Errorf("bad envelope")
	}
	env, err := e2e.DecodeEnvelope(r, body)
	if err != nil {
		return env, nil, req, fmt.Errorf("bad envelope")
	}
	cands := s.sessionsFor(env.From)
	if len(cands) == 0 {
		return env, nil, req, fmt.Errorf("no session")
	}
	var sess *e2e.Session
	var plain []byte
	for _, cand := range cands {
		pt, err := cand.Open(env)
		if err == nil {
			sess, plain = cand, pt
			break
		}
	}
	if sess == nil {
		return env, nil, req, fmt.Errorf("decrypt")
	}
	if err := json.Unmarshal(plain, &req); err != nil {
		return env, nil, req, fmt.Errorf("bad request")
	}
	return env, sess, req, nil
}

func (s *Server) dispatch(ctx context.Context, req protocol.Request) protocol.Response {
	switch req.Method {
	case protocol.MethodHealth, protocol.MethodInfo:
		return protocol.Response{ID: req.ID, OK: true, Body: protocol.MustJSON(s.Info())}
	case protocol.MethodList:
		var p protocol.ListParams
		_ = json.Unmarshal(req.Params, &p)
		rows, err := s.Drivers.ListAll(ctx, p.Engine, p.All)
		if err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		return protocol.Response{ID: req.ID, OK: true, Body: protocol.MustJSON(rows)}
	case protocol.MethodStatus:
		rows, err := s.Drivers.ListAll(ctx, "", false)
		if err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		return protocol.Response{ID: req.ID, OK: true, Body: protocol.MustJSON(rows)}
	case protocol.MethodHistory:
		var p protocol.HistoryParams
		_ = json.Unmarshal(req.Params, &p)
		evs, err := s.sessionHistory(ctx, p.Engine, p.SessionID, p.Tail)
		if err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		return protocol.Response{ID: req.ID, OK: true, Body: protocol.MustJSON(evs)}
	case protocol.MethodEngines:
		return protocol.Response{ID: req.ID, OK: true, Body: protocol.MustJSON(s.Drivers.Engines(ctx))}
	case protocol.MethodModels:
		var p protocol.ModelsParams
		_ = json.Unmarshal(req.Params, &p)
		rows, err := s.Drivers.Models(ctx, p.Engine)
		if err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		return protocol.Response{ID: req.ID, OK: true, Body: protocol.MustJSON(rows)}
	case protocol.MethodCommands:
		var p protocol.CommandsParams
		_ = json.Unmarshal(req.Params, &p)
		return protocol.Response{ID: req.ID, OK: true, Body: protocol.MustJSON(s.Drivers.SlashItems(ctx, p.Engine, p.CWD))}
	case protocol.MethodFollowups:
		return s.dispatchFollowups(ctx, req)
	case protocol.MethodChoice:
		return s.dispatchChoice(ctx, req)
	case protocol.MethodGit:
		return s.dispatchGit(ctx, req)
	case protocol.MethodGitDiff:
		return s.dispatchGitDiff(ctx, req)
	default:
		return protocol.Response{ID: req.ID, Error: "unknown method"}
	}
}

func (s *Server) dispatchStream(ctx context.Context, req protocol.Request) (<-chan protocol.Event, error) {
	switch req.Method {
	case protocol.MethodCatalog:
		return nil, fmt.Errorf("catalog uses streamCatalog")
	case protocol.MethodWatch:
		var p protocol.WatchParams
		_ = json.Unmarshal(req.Params, &p)
		d, err := s.Drivers.Get(p.Engine)
		if err != nil {
			return nil, err
		}
		return d.Watch(ctx, p.SessionID)
	case protocol.MethodReply:
		var p protocol.ReplyParams
		_ = json.Unmarshal(req.Params, &p)
		d, err := s.Drivers.Get(p.Engine)
		if err != nil {
			return nil, err
		}
		text, err := applyRPCFiles(p.Text, p.CWD, p.Files)
		if err != nil {
			return nil, err
		}
		return d.Reply(ctx, p.SessionID, text, driver.CLIModel(p.Model, p.Engine))
	case protocol.MethodSend:
		var p protocol.SendParams
		_ = json.Unmarshal(req.Params, &p)
		d, err := s.Drivers.Get(p.Engine)
		if err != nil {
			return nil, err
		}
		text, err := applyRPCFiles(p.Text, p.CWD, p.Files)
		if err != nil {
			return nil, err
		}
		return d.Send(ctx, p.CWD, text, p.Resume, driver.CLIModel(p.Model, p.Engine))
	default:
		return nil, fmt.Errorf("method %s is not a stream", req.Method)
	}
}

func applyRPCFiles(text, cwd string, files []protocol.AttachFile) (string, error) {
	if len(files) == 0 {
		return text, nil
	}
	rows := make([]openai.ChatFile, 0, len(files))
	for _, f := range files {
		rows = append(rows, openai.ChatFile{Name: f.Name, MIME: f.MIME, Data: f.Data})
	}
	return openai.ApplyFiles(cwd, text, rows)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ListenAndServe starts the node HTTP server.
func (s *Server) ListenAndServe(ctx context.Context) error {
	hs := &http.Server{Addr: s.Listen, Handler: s.Handler()}
	s.mu.Lock()
	s.snap.Followups = followQueues()
	s.mu.Unlock()
	go s.Hub.Start(ctx)
	go s.snapshotLoop(ctx)
	go s.drainAll(ctx)
	go func() {
		<-ctx.Done()
		shctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(shctx)
	}()
	s.Log.Info("node listen", "addr", s.Listen, "id", s.Ident.ID)
	err := hs.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
