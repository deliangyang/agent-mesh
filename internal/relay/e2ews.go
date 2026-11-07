package relay

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"agent-mesh/internal/e2e"

	"github.com/gorilla/websocket"
)

// handleE2EWS upgrades a browser WebSocket and bridges sealed catalog/stream frames
// to the target node. The first client binary message must be a sealed Envelope
// (typically method=catalog stream=true). Each node stream frame is forwarded as
// one WebSocket binary message containing the raw Envelope (not length-prefixed).
func (s *Server) handleE2EWS(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, err := s.lookup(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	mt, first, err := conn.ReadMessage()
	if err != nil {
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	if mt != websocket.BinaryMessage && mt != websocket.TextMessage {
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseUnsupportedData, "envelope required"))
		return
	}
	if len(first) == 0 {
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				cancel()
				return
			}
		}
	}()

	pr, pw := io.Pipe()
	hdr := http.Header{}
	hdr.Set("Content-Type", e2e.ContentTypeEnvelope)
	hdr.Set("Accept", e2e.ContentTypeEnvelopeStream)

	bridge := &streamBridge{w: pw, header: make(http.Header), pw: pw}
	go func() {
		err := s.pipeNodeHdr(ctx, rec, http.MethodPost, "/v1/stream", first, bridge, hdr)
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.Close()
	}()

	const wsWriteGrace = 5 * time.Minute
	for {
		env, err := e2e.ReadStreamFrame(pr, true)
		if err != nil {
			if err != io.EOF && ctx.Err() == nil {
				s.Log.Debug("e2e ws frame", "node", id, "err", err)
			}
			return
		}
		raw, err := env.MarshalBinary()
		if err != nil {
			return
		}
		_ = conn.SetWriteDeadline(time.Now().Add(wsWriteGrace))
		if err := conn.WriteMessage(websocket.BinaryMessage, raw); err != nil {
			return
		}
	}
}

// streamBridge captures a streamed HTTP response body into an io.Writer.
type streamBridge struct {
	header http.Header
	status int
	wrote  bool
	w      io.Writer
	pw     *io.PipeWriter
}

func (b *streamBridge) Header() http.Header {
	if b.header == nil {
		b.header = make(http.Header)
	}
	return b.header
}

func (b *streamBridge) WriteHeader(code int) {
	if b.wrote {
		return
	}
	b.status = code
	b.wrote = true
	if code >= 400 && b.pw != nil {
		_ = b.pw.CloseWithError(fmt.Errorf("node http %d", code))
	}
}

func (b *streamBridge) Write(p []byte) (int, error) {
	if !b.wrote {
		b.WriteHeader(http.StatusOK)
	}
	if b.status >= 400 {
		return len(p), nil
	}
	return b.w.Write(p)
}

func (b *streamBridge) Flush() {}
