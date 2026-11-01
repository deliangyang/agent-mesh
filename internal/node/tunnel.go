package node

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/protocol"

	"github.com/gorilla/websocket"
)

func httpToWS(u string) string {
	switch {
	case strings.HasPrefix(u, "https://"):
		return "wss://" + strings.TrimPrefix(u, "https://")
	case strings.HasPrefix(u, "http://"):
		return "ws://" + strings.TrimPrefix(u, "http://")
	default:
		return u
	}
}

func (s *Server) tunnelLoop(ctx context.Context, relays []string, advertise string) {
	if len(relays) == 0 {
		return
	}
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		started := time.Now()
		err := s.dialTunnels(ctx, relays, advertise)
		if ctx.Err() != nil {
			return
		}
		s.Log.Warn("tunnel 断开，准备重连", "err", err)
		if time.Since(started) > 30*time.Second {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 15*time.Second {
			backoff *= 2
		}
	}
}

func (s *Server) dialTunnels(ctx context.Context, relays []string, advertise string) error {
	var last error
	for _, relay := range relays {
		if err := s.serveTunnel(ctx, relay, advertise); err != nil {
			last = err
			continue
		}
		return nil
	}
	if last == nil {
		return fmt.Errorf("未配置中继")
	}
	return last
}

func (s *Server) serveTunnel(ctx context.Context, relay, advertise string) error {
	wsURL := httpToWS(strings.TrimRight(relay, "/") + "/v1/node/tunnel")
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 12 * time.Second
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	info := s.Info()
	info.Listen = s.AdvertiseURL(advertise)
	unix := time.Now().Unix()
	msg := []byte(fmt.Sprintf("%s|%s|%d", info.ID, info.Listen, unix))
	hello := protocol.RegisterRequest{
		Info:      info,
		Unix:      unix,
		Signature: s.Ident.SignBytes(msg),
		Snapshot:  s.cachedSnapshot(ctx),
	}
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteJSON(hello); err != nil {
		return err
	}
	s.Log.Info("tunnel connected", "relay", relay)

	var wmu sync.Mutex
	s.setTunnel(conn, &wmu)
	defer s.setTunnel(nil, nil)
	_ = conn.SetReadDeadline(time.Now().Add(75 * time.Second))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(75 * time.Second))
		return nil
	})
	pingDone := make(chan struct{})
	defer close(pingDone)
	go func() {
		tick := time.NewTicker(20 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-pingDone:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				wmu.Lock()
				_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				err := conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(5*time.Second))
				wmu.Unlock()
				if err != nil {
					return
				}
			}
		}
	}()
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var f protocol.TunnelFrame
		if err := conn.ReadJSON(&f); err != nil {
			return err
		}
		_ = conn.SetReadDeadline(time.Now().Add(75 * time.Second))
		if f.Path == "" || f.ID == "" {
			continue
		}
		go s.handleTunnelReq(conn, &wmu, f)
	}
}

func (s *Server) handleTunnelReq(conn *websocket.Conn, wmu *sync.Mutex, f protocol.TunnelFrame) {
	method := f.Method
	if method == "" {
		method = http.MethodGet
	}
	path := f.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(f.Body))
	req.Header.Set("Content-Type", "application/json")
	rw := &tunnelRW{id: f.ID, conn: conn, wmu: wmu, header: make(http.Header)}
	s.Handler().ServeHTTP(rw, req)
	rw.eof()
}

type tunnelRW struct {
	id     string
	conn   *websocket.Conn
	wmu    *sync.Mutex
	header http.Header
	status int
	wrote  bool
}

func (w *tunnelRW) Header() http.Header { return w.header }

func (w *tunnelRW) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.status = code
	w.flushHead()
}

const tunnelChunk = 24 << 10

func (w *tunnelRW) Write(p []byte) (int, error) {
	w.flushHead()
	sent := 0
	for sent < len(p) {
		n := tunnelChunk
		if sent+n > len(p) {
			n = len(p) - sent
		}
		cp := append([]byte(nil), p[sent:sent+n]...)
		if err := w.send(protocol.TunnelFrame{ID: w.id, Chunk: cp}); err != nil {
			return sent, err
		}
		sent += n
	}
	return sent, nil
}

func (w *tunnelRW) Flush() {}

func (w *tunnelRW) flushHead() {
	if w.wrote {
		return
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	hdr := map[string]string{}
	if ct := w.header.Get("Content-Type"); ct != "" {
		hdr["Content-Type"] = ct
	}
	if cc := w.header.Get("Cache-Control"); cc != "" {
		hdr["Cache-Control"] = cc
	}
	w.wrote = true
	_ = w.send(protocol.TunnelFrame{ID: w.id, Status: w.status, Header: hdr})
}

func (w *tunnelRW) eof() {
	w.flushHead()
	_ = w.send(protocol.TunnelFrame{ID: w.id, EOF: true})
}

func (w *tunnelRW) send(f protocol.TunnelFrame) error {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	_ = w.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	return w.conn.WriteJSON(f)
}

func (s *Server) snapshot(ctx context.Context) protocol.NodeSnapshot {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	snap := protocol.NodeSnapshot{}
	if s.Drivers == nil {
		return snap
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		engines := s.Drivers.Engines(ctx)
		if engines == nil {
			engines = []protocol.EngineInfo{}
		}
		mu.Lock()
		snap.Engines = engines
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		rows, err := s.Drivers.ListAll(ctx, "", true)
		if err != nil {
			return
		}
		if rows == nil {
			rows = []protocol.Session{}
		}
		dirs := driver.DirsOf(rows)
		if dirs == nil {
			dirs = []protocol.WorkDir{}
		}
		mu.Lock()
		snap.Sessions = rows
		snap.Dirs = dirs
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		rows, err := s.Drivers.Models(ctx, "")
		if err != nil {
			return
		}
		if rows == nil {
			rows = []protocol.Model{}
		}
		mu.Lock()
		snap.Models = rows
		mu.Unlock()
	}()
	wg.Wait()
	return snap
}

func (s *Server) cachedSnapshot(ctx context.Context) protocol.NodeSnapshot {
	s.mu.Lock()
	snap := s.snap
	s.mu.Unlock()
	if len(snap.Engines) > 0 || len(snap.Models) > 0 || len(snap.Sessions) > 0 {
		return snap
	}
	s.refreshSnapshot(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap
}

func (s *Server) peekSnapshot() protocol.NodeSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap
}

func (s *Server) refreshSnapshot(ctx context.Context) {
	s.mu.Lock()
	prev := s.snap
	s.mu.Unlock()
	snap := s.snapshot(ctx)
	s.mu.Lock()
	snap.Followups = s.snap.Followups
	raw, _ := json.Marshal(snap)
	same := bytes.Equal(raw, s.snapRaw)
	if !same {
		s.snap = snap
		s.snapRaw = raw
	}
	s.mu.Unlock()
	if !same {
		s.pushSnapshot(snap)
	}
	s.drainIdle(prev.Sessions, snap.Sessions)
}

func (s *Server) snapshotLoop(ctx context.Context) {
	s.refreshSnapshot(ctx)
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.refreshSnapshot(ctx)
		}
	}
}

func (s *Server) setTunnel(conn *websocket.Conn, wmu *sync.Mutex) {
	s.mu.Lock()
	s.tunConn = tunnelConn{conn: conn, wmu: wmu}
	s.mu.Unlock()
}

func (s *Server) pushSnapshot(snap protocol.NodeSnapshot) {
	s.mu.Lock()
	conn, wmu := s.tunConn.conn, s.tunConn.wmu
	s.mu.Unlock()
	if conn == nil || wmu == nil {
		return
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return
	}
	wmu.Lock()
	defer wmu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_ = conn.WriteJSON(protocol.TunnelFrame{ID: "snapshot", Path: "/__snapshot", Body: raw})
}
