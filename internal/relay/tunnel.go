package relay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"agent-mesh/internal/e2e"
	"agent-mesh/internal/protocol"

	"github.com/gorilla/websocket"
)

type pipeStatus struct {
	error
	started bool
}

func (e *pipeStatus) Unwrap() error { return e.error }

func pipeHasStarted(err error) bool {
	var p *pipeStatus
	return errors.As(err, &p) && p.started
}

type tunnelHub struct {
	mu    sync.Mutex
	nodes map[string]*nodeTunnel
}

func newTunnelHub() *tunnelHub {
	return &tunnelHub{nodes: map[string]*nodeTunnel{}}
}

type nodeTunnel struct {
	id   string
	conn *websocket.Conn
	wmu  sync.Mutex
	mu   sync.Mutex
	wait map[string]chan protocol.TunnelFrame
}

func (h *tunnelHub) bind(id string, conn *websocket.Conn) *nodeTunnel {
	t := &nodeTunnel{id: id, conn: conn, wait: map[string]chan protocol.TunnelFrame{}}
	h.mu.Lock()
	old := h.nodes[id]
	h.nodes[id] = t
	h.mu.Unlock()
	if old != nil && old.conn != conn {
		_ = old.conn.Close()
		old.failAll("replaced")
	}
	return t
}

func (h *tunnelHub) unbind(id string, t *nodeTunnel) {
	h.mu.Lock()
	if h.nodes[id] == t {
		delete(h.nodes, id)
	}
	h.mu.Unlock()
}

func (h *tunnelHub) get(id string) *nodeTunnel {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.nodes[id]
}

func (t *nodeTunnel) failAll(msg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, ch := range t.wait {
		select {
		case ch <- protocol.TunnelFrame{ID: id, Error: msg, EOF: true}:
		default:
		}
		delete(t.wait, id)
	}
}

func (t *nodeTunnel) subscribe(id string) chan protocol.TunnelFrame {
	ch := make(chan protocol.TunnelFrame, 512)
	t.mu.Lock()
	t.wait[id] = ch
	t.mu.Unlock()
	return ch
}

func (t *nodeTunnel) unsubscribe(id string) {
	t.mu.Lock()
	delete(t.wait, id)
	t.mu.Unlock()
}

func (t *nodeTunnel) dispatch(f protocol.TunnelFrame) {
	t.mu.Lock()
	ch := t.wait[f.ID]
	t.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- f:
	case <-time.After(15 * time.Second):
	}
}

func (t *nodeTunnel) write(f protocol.TunnelFrame) error {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	_ = t.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	return t.conn.WriteJSON(f)
}

func newFrameID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func tunnelReq(id, method, path string, body []byte, hdr http.Header) protocol.TunnelFrame {
	req := protocol.TunnelFrame{ID: id, Method: method, Path: path, Header: map[string]string{}}
	if hdr != nil {
		if ct := hdr.Get("Content-Type"); ct != "" {
			req.Header["Content-Type"] = ct
		}
		if a := hdr.Get("Accept"); a != "" {
			req.Header["Accept"] = a
		}
	}
	if len(body) == 0 {
		return req
	}
	ct := ""
	if hdr != nil {
		ct = hdr.Get("Content-Type")
	}
	if e2e.ContentTypeIsEnvelope(ct) || e2e.IsBinaryPayload(body) {
		req.Raw = append([]byte(nil), body...)
		if req.Header["Content-Type"] == "" {
			req.Header["Content-Type"] = e2e.ContentTypeEnvelope
		}
		return req
	}
	req.Body = json.RawMessage(body)
	return req
}

func (h *tunnelHub) Do(ctx context.Context, nodeID, method, path string, body []byte) (int, []byte, error) {
	return h.DoHdr(ctx, nodeID, method, path, body, nil)
}

func (h *tunnelHub) DoHdr(ctx context.Context, nodeID, method, path string, body []byte, hdr http.Header) (int, []byte, error) {
	t := h.get(nodeID)
	if t == nil {
		return 0, nil, fmt.Errorf("node tunnel offline")
	}
	id := newFrameID()
	ch := t.subscribe(id)
	defer t.unsubscribe(id)
	req := tunnelReq(id, method, path, body, hdr)
	if err := t.write(req); err != nil {
		return 0, nil, err
	}
	var status int
	var buf bytes.Buffer
	for {
		select {
		case <-ctx.Done():
			return 0, nil, ctx.Err()
		case f, ok := <-ch:
			if !ok {
				return status, buf.Bytes(), nil
			}
			if f.Error != "" && len(f.Body) == 0 && len(f.Chunk) == 0 && !f.EOF {
				return 0, nil, fmt.Errorf("%s", f.Error)
			}
			if f.Status != 0 {
				status = f.Status
			}
			if len(f.Body) > 0 {
				buf.Write(f.Body)
			}
			if len(f.Chunk) > 0 {
				buf.Write(f.Chunk)
			}
			if f.EOF {
				if status >= 400 {
					return status, buf.Bytes(), fmt.Errorf("http %d", status)
				}
				return status, buf.Bytes(), nil
			}
		}
	}
}

func (h *tunnelHub) Pipe(ctx context.Context, nodeID, method, path string, body []byte, w http.ResponseWriter) error {
	return h.PipeHdr(ctx, nodeID, method, path, body, w, nil)
}

func (h *tunnelHub) PipeHdr(ctx context.Context, nodeID, method, path string, body []byte, w http.ResponseWriter, hdr http.Header) error {
	t := h.get(nodeID)
	if t == nil {
		return fmt.Errorf("node tunnel offline")
	}
	id := newFrameID()
	ch := t.subscribe(id)
	defer t.unsubscribe(id)
	req := tunnelReq(id, method, path, body, hdr)
	if err := t.write(req); err != nil {
		return err
	}
	headerSent := false
	fl, _ := w.(http.Flusher)
	fail := func(err error) error {
		if headerSent {
			return &pipeStatus{error: err, started: true}
		}
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return fail(ctx.Err())
		case f, ok := <-ch:
			if !ok {
				if headerSent {
					return nil
				}
				return fmt.Errorf("node tunnel closed")
			}
			if f.Error != "" && !headerSent && len(f.Chunk) == 0 && len(f.Body) == 0 {
				return fmt.Errorf("%s", f.Error)
			}
			if !headerSent {
				status := f.Status
				if status == 0 {
					status = http.StatusOK
				}
				if ct := f.Header["Content-Type"]; ct != "" {
					w.Header().Set("Content-Type", ct)
				}
				if cc := f.Header["Cache-Control"]; cc != "" {
					w.Header().Set("Cache-Control", cc)
				}
				w.WriteHeader(status)
				headerSent = true
			}
			if len(f.Body) > 0 {
				_, _ = w.Write(f.Body)
				if fl != nil {
					fl.Flush()
				}
			}
			if len(f.Chunk) > 0 {
				_, _ = w.Write(f.Chunk)
				if fl != nil {
					fl.Flush()
				}
			}
			if f.EOF {
				return nil
			}
		}
	}
}

func (s *Server) handleNodeTunnel(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	var req protocol.RegisterRequest
	if err := conn.ReadJSON(&req); err != nil {
		return
	}
	if time.Since(time.Unix(req.Unix, 0)) > 2*time.Minute {
		return
	}
	msg := []byte(fmt.Sprintf("%s|%s|%d", req.Info.ID, req.Info.Listen, req.Unix))
	if len(req.Info.SignPub) != ed25519.PublicKeySize || !ed25519.Verify(req.Info.SignPub, msg, req.Signature) {
		return
	}
	req.Info.LastSeen = time.Now().Unix()
	s.Dir.Upsert(Record{Info: req.Info, Advertise: req.Info.Listen, Seen: time.Now(), Snapshot: protocol.PublicSnapshot(req.Snapshot)})
	s.publishCatalog()
	t := s.tunnels.bind(req.Info.ID, conn)
	defer s.tunnels.unbind(req.Info.ID, t)
	_ = conn.SetReadDeadline(time.Now().Add(75 * time.Second))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(75 * time.Second))
		s.Dir.Touch(req.Info.ID)
		return nil
	})
	conn.SetPingHandler(func(app string) error {
		_ = conn.SetReadDeadline(time.Now().Add(75 * time.Second))
		s.Dir.Touch(req.Info.ID)
		t.wmu.Lock()
		defer t.wmu.Unlock()
		return conn.WriteControl(websocket.PongMessage, []byte(app), time.Now().Add(5*time.Second))
	})
	done := make(chan struct{})
	defer close(done)
	go func() {
		tick := time.NewTicker(20 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				t.wmu.Lock()
				err := conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(5*time.Second))
				t.wmu.Unlock()
				if err != nil {
					return
				}
			}
		}
	}()
	s.Log.Info("node tunnel up", "id", req.Info.ID, "advertise", req.Info.Listen)
	for {
		var f protocol.TunnelFrame
		if err := conn.ReadJSON(&f); err != nil {
			t.failAll(err.Error())
			s.Log.Info("node tunnel down", "id", req.Info.ID, "err", err)
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(75 * time.Second))
		s.Dir.Touch(req.Info.ID)
		if f.Path == "/__snapshot" {
			s.applyNodeSnapshot(req.Info.ID, f.Body)
			continue
		}
		if f.Path == "/v1/openai/ingest" {
			s.applyTunnelIngest(f.Body)
			continue
		}
		t.dispatch(f)
	}
}

func (s *Server) applyTunnelIngest(raw json.RawMessage) {
	// Ignore plaintext session chunks from older nodes; Web E2E uses Envelope streams.
	_ = raw
}

func (s *Server) applyNodeSnapshot(id string, raw json.RawMessage) {
	var snap protocol.NodeSnapshot
	if json.Unmarshal(raw, &snap) != nil {
		return
	}
	s.Dir.SetSnapshot(id, protocol.PublicSnapshot(snap))
	s.publishCatalog()
}
