package e2e

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	ContentTypeEnvelope       = "application/vnd.agent-mesh.envelope"
	ContentTypeEnvelopeStream = "application/vnd.agent-mesh.envelope-stream"

	frameMagic0 = 'A'
	frameMagic1 = 'M'

	kindHandshake byte = 1
	kindRPC       byte = 2
	kindStream    byte = 3

	flagEph byte = 1
)

func kindByte(kind string) byte {
	switch kind {
	case "handshake":
		return kindHandshake
	case "rpc":
		return kindRPC
	case "stream":
		return kindStream
	default:
		return 0
	}
}

func kindString(k byte) string {
	switch k {
	case kindHandshake:
		return "handshake"
	case kindRPC:
		return "rpc"
	case kindStream:
		return "stream"
	default:
		return ""
	}
}

// MarshalBinary encodes the envelope without JSON/base64 (ciphertext stays raw).
func (e Envelope) MarshalBinary() ([]byte, error) {
	from := []byte(e.From)
	to := []byte(e.To)
	if len(from) > 0xffff || len(to) > 0xffff {
		return nil, errors.New("envelope id too long")
	}
	if len(e.EphPub) > 255 || len(e.Nonce) > 255 {
		return nil, errors.New("envelope field too long")
	}
	if len(e.Ciphertext) > 0xffffffff {
		return nil, errors.New("envelope ciphertext too long")
	}
	ver := e.Version
	if ver == 0 {
		ver = version
	}
	flags := byte(0)
	if len(e.EphPub) > 0 {
		flags |= flagEph
	}
	n := 2 + 1 + 1 + 1 + 2 + len(from) + 2 + len(to) + 1 + len(e.Nonce) + 4 + len(e.Ciphertext)
	if flags&flagEph != 0 {
		n += 1 + len(e.EphPub)
	}
	buf := make([]byte, 0, n)
	buf = append(buf, frameMagic0, frameMagic1, byte(ver), kindByte(e.Kind), flags)
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(from)))
	buf = append(buf, from...)
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(to)))
	buf = append(buf, to...)
	if flags&flagEph != 0 {
		buf = append(buf, byte(len(e.EphPub)))
		buf = append(buf, e.EphPub...)
	}
	buf = append(buf, byte(len(e.Nonce)))
	buf = append(buf, e.Nonce...)
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(e.Ciphertext)))
	buf = append(buf, e.Ciphertext...)
	return buf, nil
}

// UnmarshalBinary decodes a binary envelope.
func (e *Envelope) UnmarshalBinary(data []byte) error {
	if len(data) < 2+1+1+1+2+2+1+4 {
		return errors.New("envelope too short")
	}
	if data[0] != frameMagic0 || data[1] != frameMagic1 {
		return errors.New("bad envelope magic")
	}
	off := 2
	e.Version = int(data[off])
	off++
	e.Kind = kindString(data[off])
	off++
	flags := data[off]
	off++
	if off+2 > len(data) {
		return errors.New("truncated from")
	}
	fromLen := int(binary.BigEndian.Uint16(data[off:]))
	off += 2
	if off+fromLen+2 > len(data) {
		return errors.New("truncated from/to")
	}
	e.From = string(data[off : off+fromLen])
	off += fromLen
	toLen := int(binary.BigEndian.Uint16(data[off:]))
	off += 2
	if off+toLen > len(data) {
		return errors.New("truncated to")
	}
	e.To = string(data[off : off+toLen])
	off += toLen
	if flags&flagEph != 0 {
		if off >= len(data) {
			return errors.New("truncated eph len")
		}
		n := int(data[off])
		off++
		if off+n > len(data) {
			return errors.New("truncated eph")
		}
		e.EphPub = append([]byte(nil), data[off:off+n]...)
		off += n
	} else {
		e.EphPub = nil
	}
	if off >= len(data) {
		return errors.New("truncated nonce len")
	}
	nLen := int(data[off])
	off++
	if off+nLen+4 > len(data) {
		return errors.New("truncated nonce")
	}
	e.Nonce = append([]byte(nil), data[off:off+nLen]...)
	off += nLen
	cLen := int(binary.BigEndian.Uint32(data[off:]))
	off += 4
	if off+cLen != len(data) {
		if off+cLen > len(data) {
			return errors.New("truncated ciphertext")
		}
		return errors.New("trailing envelope bytes")
	}
	e.Ciphertext = append([]byte(nil), data[off:off+cLen]...)
	return nil
}

// IsBinaryPayload reports whether raw looks like a binary envelope.
func IsBinaryPayload(raw []byte) bool {
	return len(raw) >= 2 && raw[0] == frameMagic0 && raw[1] == frameMagic1
}

// ContentTypeIsEnvelope reports binary envelope content types.
func ContentTypeIsEnvelope(ct string) bool {
	ct = strings.ToLower(ct)
	return strings.Contains(ct, ContentTypeEnvelope)
}

// WantsBinary reports whether the client prefers binary envelopes.
func WantsBinary(r *http.Request) bool {
	if r == nil {
		return false
	}
	if ContentTypeIsEnvelope(r.Header.Get("Content-Type")) {
		return true
	}
	accept := strings.ToLower(r.Header.Get("Accept"))
	return strings.Contains(accept, ContentTypeEnvelope)
}

// WantsBinaryStream reports whether the client wants length-prefixed binary stream frames.
func WantsBinaryStream(r *http.Request) bool {
	if r == nil {
		return false
	}
	accept := strings.ToLower(r.Header.Get("Accept"))
	return strings.Contains(accept, ContentTypeEnvelopeStream) ||
		(WantsBinary(r) && !strings.Contains(accept, "text/event-stream"))
}

// DecodeEnvelope reads one envelope from an HTTP body (binary or JSON).
func DecodeEnvelope(r *http.Request, body []byte) (Envelope, error) {
	var env Envelope
	if len(body) == 0 {
		return env, errors.New("empty envelope")
	}
	if ContentTypeIsEnvelope(r.Header.Get("Content-Type")) || IsBinaryPayload(body) {
		if err := env.UnmarshalBinary(body); err != nil {
			return env, err
		}
		return env, nil
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return env, err
	}
	return env, nil
}

// EncodeEnvelope encodes for the wire; binary when wantBinary.
func EncodeEnvelope(env Envelope, wantBinary bool) ([]byte, string, error) {
	if wantBinary {
		raw, err := env.MarshalBinary()
		return raw, ContentTypeEnvelope, err
	}
	raw, err := json.Marshal(env)
	return raw, "application/json", err
}

// WriteEnvelope writes a single envelope response.
func WriteEnvelope(w http.ResponseWriter, code int, env Envelope, wantBinary bool) error {
	raw, ct, err := EncodeEnvelope(env, wantBinary)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(code)
	_, err = w.Write(raw)
	return err
}

// WriteStreamFrame writes one encrypted stream frame.
func WriteStreamFrame(w io.Writer, env Envelope, binaryStream bool) error {
	if binaryStream {
		raw, err := env.MarshalBinary()
		if err != nil {
			return err
		}
		var hdr [4]byte
		binary.BigEndian.PutUint32(hdr[:], uint32(len(raw)))
		if _, err := w.Write(hdr[:]); err != nil {
			return err
		}
		_, err = w.Write(raw)
		return err
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", raw)
	return err
}

// ReadStreamFrame reads one encrypted stream frame from r.
func ReadStreamFrame(r io.Reader, binaryStream bool) (Envelope, error) {
	var env Envelope
	if binaryStream {
		var hdr [4]byte
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return env, err
		}
		n := binary.BigEndian.Uint32(hdr[:])
		if n == 0 || n > 16<<20 {
			return env, fmt.Errorf("bad frame size %d", n)
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(r, buf); err != nil {
			return env, err
		}
		if err := env.UnmarshalBinary(buf); err != nil {
			return env, err
		}
		return env, nil
	}
	return env, errors.New("use SSE scanner for json stream")
}
