package relay

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"agent-mesh/internal/i18n"
	"agent-mesh/internal/protocol"
)

const ttl = 30 * time.Second

// Record is a registered node.
type Record struct {
	Info      protocol.NodeInfo
	Advertise string
	Seen      time.Time
	Snapshot  protocol.NodeSnapshot
}

// Directory is the node directory; a Redis-backed impl can replace it.
type Directory interface {
	Upsert(Record)
	Get(id string) (Record, bool)
	List() []Record
	Sweep(now time.Time)
	Touch(id string)
	SetSnapshot(id string, snap protocol.NodeSnapshot)
}

// Memory is an in-memory directory for a single relay; shared storage can replace it for many relays.
type Memory struct {
	mu    sync.RWMutex
	nodes map[string]Record
}

// NewMemory creates an in-memory directory.
func NewMemory() *Memory {
	return &Memory{nodes: make(map[string]Record)}
}

func (m *Memory) Upsert(r Record) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.nodes[r.Info.ID]; ok {
		r.Snapshot = mergeSnapshot(old.Snapshot, r.Snapshot)
	}
	m.nodes[r.Info.ID] = r
}

func (m *Memory) Get(id string) (Record, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.nodes[id]
	return r, ok
}

func (m *Memory) List() []Record {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Record, 0, len(m.nodes))
	now := time.Now()
	for _, r := range m.nodes {
		if now.Sub(r.Seen) <= ttl {
			out = append(out, r)
		}
	}
	return out
}

func (m *Memory) Sweep(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, r := range m.nodes {
		if now.Sub(r.Seen) > ttl {
			delete(m.nodes, id)
		}
	}
}

func (m *Memory) Touch(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.nodes[id]
	if !ok {
		return
	}
	r.Seen = time.Now()
	r.Info.LastSeen = r.Seen.Unix()
	m.nodes[id] = r
}

func (m *Memory) SetSnapshot(id string, snap protocol.NodeSnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.nodes[id]
	if !ok {
		return
	}
	r.Snapshot = mergeSnapshot(r.Snapshot, snap)
	r.Seen = time.Now()
	r.Info.LastSeen = r.Seen.Unix()
	m.nodes[id] = r
}

func mergeSnapshot(old, neu protocol.NodeSnapshot) protocol.NodeSnapshot {
	if neu.Models == nil {
		neu.Models = old.Models
	}
	if neu.Engines == nil {
		neu.Engines = old.Engines
	}
	if neu.Sessions == nil {
		neu.Sessions = old.Sessions
	}
	if neu.Dirs == nil {
		neu.Dirs = old.Dirs
	}
	if neu.Followups == nil {
		neu.Followups = old.Followups
	}
	return neu
}

// Server forwards encrypted envelopes without decrypting them.
// OpenAI proxy routes may still pipe bytes for external tools; catalog snapshots are public-only
// (no session paths/bodies), and ingest/history aggregation for the Web UI is disabled.
type Server struct {
	Listen   string
	Dir      Directory
	Log      *slog.Logger
	Client   *http.Client
	Password string
	Web      bool
	secret   []byte
	ingest   *ingestHub
	tunnels  *tunnelHub
	catalogs *catalogHub
	catMu    sync.Mutex
	lastCat  Catalog
}

// New creates a relay.
func New(listen string, dir Directory, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	if dir == nil {
		dir = NewMemory()
	}
	s := &Server{
		Listen:   listen,
		Dir:      dir,
		Log:      log,
		Client:   &http.Client{Timeout: 120 * time.Second},
		ingest:   newIngestHub(),
		tunnels:  newTunnelHub(),
		catalogs: newCatalogHub(),
	}
	s.initAuth()
	return s
}

// Handler is the router.
func (s *Server) Handler() http.Handler {
	s.initAuth()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("POST /v1/register", s.handleRegister)
	mux.HandleFunc("GET /v1/node/tunnel", s.handleNodeTunnel)
	mux.HandleFunc("GET /v1/nodes", s.handleNodes)
	mux.HandleFunc("POST /v1/to/{id}/handshake", s.forward("/v1/handshake"))
	mux.HandleFunc("POST /v1/to/{id}/rpc", s.forward("/v1/rpc"))
	mux.HandleFunc("POST /v1/to/{id}/stream", s.forwardStream)
	s.mountOpenAI(mux)
	s.mountWeb(mux)
	return s.withAuth(withCORS(mux))
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

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req protocol.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		i18n.Error(w, r, http.StatusBadRequest, "http.badJSON")
		return
	}
	if time.Since(time.Unix(req.Unix, 0)) > 2*time.Minute {
		i18n.Error(w, r, http.StatusUnauthorized, "http.staleRegister")
		return
	}
	msg := []byte(fmt.Sprintf("%s|%s|%d", req.Info.ID, req.Info.Listen, req.Unix))
	if len(req.Info.SignPub) != ed25519.PublicKeySize || !ed25519.Verify(req.Info.SignPub, msg, req.Signature) {
		i18n.Error(w, r, http.StatusUnauthorized, "http.badSignature")
		return
	}
	req.Info.LastSeen = time.Now().Unix()
	s.Dir.Upsert(Record{Info: req.Info, Advertise: req.Info.Listen, Seen: time.Now(), Snapshot: protocol.PublicSnapshot(req.Snapshot)})
	s.publishCatalog()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": req.Info.ID})
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	rows := s.Dir.List()
	infos := make([]protocol.NodeInfo, 0, len(rows))
	for _, r := range rows {
		infos = append(infos, r.Info)
	}
	writeJSON(w, http.StatusOK, infos)
}

func (s *Server) lookup(id string) (Record, error) {
	rec, ok := s.Dir.Get(id)
	if !ok {
		return Record{}, fmt.Errorf("unknown node")
	}
	if time.Since(rec.Seen) > ttl {
		return Record{}, fmt.Errorf("node expired")
	}
	return rec, nil
}

func (s *Server) forward(path string) http.HandlerFunc {
	path = strings.TrimSpace(path)
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		rec, err := s.lookup(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		writePipeErr(w, s.pipeNodeHdr(r.Context(), rec, http.MethodPost, path, body, w, r.Header))
	}
}

func (s *Server) forwardStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, err := s.lookup(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	body, _ := io.ReadAll(r.Body)
	writePipeErr(w, s.pipeNodeHdr(r.Context(), rec, http.MethodPost, "/v1/stream", body, w, r.Header))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ListenAndServe starts the relay.
func (s *Server) ListenAndServe(ctx context.Context) error {
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				s.Dir.Sweep(now)
			}
		}
	}()
	hs := &http.Server{Addr: s.Listen, Handler: s.Handler()}
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(sh)
	}()
	s.initAuth()
	s.Log.Info("relay listen", "addr", s.Listen, "web", s.Web)
	err := hs.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
