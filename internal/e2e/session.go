package e2e

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

const (
	version    = 1
	helloLabel = "mesh/e2e/v1/hello"
	ackLabel   = "mesh/e2e/v1/ack"
	c2sLabel   = "mesh/e2e/v1/c2s"
	s2cLabel   = "mesh/e2e/v1/s2c"
	nonceSize  = 12
	sessionTTL = 12 * time.Hour
)

// Envelope 中继只读取 From/To/Kind，密文对中继不可见。
type Envelope struct {
	Version    int    `json:"v"`
	From       string `json:"from"`
	To         string `json:"to"`
	Kind       string `json:"kind"` // handshake | rpc | stream
	EphPub     []byte `json:"eph,omitempty"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ct"`
}

// Session 一次握手后的双向会话密钥。
type Session struct {
	ID      string
	PeerID  string
	LocalID string
	c2s     []byte
	s2c     []byte
	sendN   atomic.Uint64
	recvN   atomic.Uint64
	Expiry  time.Time
	// clientSend 为 true 表示本端是客户端（用 c2s 加密）。
	clientSend bool
}

type handshakeHello struct {
	ClientID string `json:"client_id"`
	Unix     int64  `json:"unix"`
	Random   []byte `json:"random"`
}

type handshakeAck struct {
	NodeID    string `json:"node_id"`
	SessionID string `json:"session_id"`
	Expiry    int64  `json:"expiry"`
}

func derive(ikm []byte, info string, n int) ([]byte, error) {
	return hkdf.Key(sha256.New, ikm, []byte("agent-mesh"), info, n)
}

func aeadFrom(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func seal(key, plaintext []byte) (nonce, ct []byte, err error) {
	aead, err := aeadFrom(key)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	ct = aead.Seal(nil, nonce, plaintext, nil)
	return nonce, ct, nil
}

func open(key, nonce, ct []byte) ([]byte, error) {
	aead, err := aeadFrom(key)
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, nonce, ct, nil)
}

func counterNonce(n uint64) []byte {
	nonce := make([]byte, nonceSize)
	binary.BigEndian.PutUint64(nonce[4:], n)
	return nonce
}

func (s *Session) seal(key []byte, counter *atomic.Uint64, plaintext []byte) (nonce, ct []byte, err error) {
	n := counter.Add(1)
	nonce = counterNonce(n)
	aead, err := aeadFrom(key)
	if err != nil {
		return nil, nil, err
	}
	ct = aead.Seal(nil, nonce, plaintext, nil)
	return nonce, ct, nil
}

func (s *Session) open(key, nonce, ct []byte) ([]byte, error) {
	return open(key, nonce, ct)
}

func (s *Session) sendKey() []byte {
	if s.clientSend {
		return s.c2s
	}
	return s.s2c
}

func (s *Session) recvKey() []byte {
	if s.clientSend {
		return s.s2c
	}
	return s.c2s
}

// Seal 加密一条应用消息。
func (s *Session) Seal(plaintext []byte) (Envelope, error) {
	nonce, ct, err := s.seal(s.sendKey(), &s.sendN, plaintext)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{
		Version:    version,
		From:       s.LocalID,
		To:         s.PeerID,
		Kind:       "rpc",
		Nonce:      nonce,
		Ciphertext: ct,
	}, nil
}

// Open 解密一条应用消息。
func (s *Session) Open(env Envelope) ([]byte, error) {
	if env.Version != version {
		return nil, fmt.Errorf("unsupported envelope version %d", env.Version)
	}
	return s.open(s.recvKey(), env.Nonce, env.Ciphertext)
}

// Expired 会话是否过期。
func (s *Session) Expired() bool {
	return time.Now().After(s.Expiry)
}

// BeginHandshake 客户端发起握手，返回待发送信封和临时私钥（完成握手时用）。
func BeginHandshake(local *Identity, peer PublicIdentity) (Envelope, *ecdh.PrivateKey, error) {
	eph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return Envelope{}, nil, err
	}
	peerPub, err := peer.X25519Public()
	if err != nil {
		return Envelope{}, nil, err
	}
	ss, err := eph.ECDH(peerPub)
	if err != nil {
		return Envelope{}, nil, err
	}
	helloKey, err := derive(ss, helloLabel, 32)
	if err != nil {
		return Envelope{}, nil, err
	}
	rnd := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, rnd); err != nil {
		return Envelope{}, nil, err
	}
	body, err := json.Marshal(handshakeHello{
		ClientID: local.ID,
		Unix:     time.Now().Unix(),
		Random:   rnd,
	})
	if err != nil {
		return Envelope{}, nil, err
	}
	nonce, ct, err := seal(helloKey, body)
	if err != nil {
		return Envelope{}, nil, err
	}
	return Envelope{
		Version:    version,
		From:       local.ID,
		To:         peer.ID,
		Kind:       "handshake",
		EphPub:     eph.PublicKey().Bytes(),
		Nonce:      nonce,
		Ciphertext: ct,
	}, eph, nil
}

// AcceptHandshake 节点处理握手。
func AcceptHandshake(local *Identity, env Envelope) (Envelope, *Session, error) {
	if env.Kind != "handshake" {
		return Envelope{}, nil, errors.New("not a handshake envelope")
	}
	clientEph, err := ecdh.X25519().NewPublicKey(env.EphPub)
	if err != nil {
		return Envelope{}, nil, fmt.Errorf("client eph: %w", err)
	}
	ss, err := local.X25519.ECDH(clientEph)
	if err != nil {
		return Envelope{}, nil, err
	}
	helloKey, err := derive(ss, helloLabel, 32)
	if err != nil {
		return Envelope{}, nil, err
	}
	plain, err := open(helloKey, env.Nonce, env.Ciphertext)
	if err != nil {
		return Envelope{}, nil, fmt.Errorf("decrypt hello: %w", err)
	}
	var hello handshakeHello
	if err := json.Unmarshal(plain, &hello); err != nil {
		return Envelope{}, nil, fmt.Errorf("hello json: %w", err)
	}
	if hello.ClientID == "" {
		return Envelope{}, nil, errors.New("empty client id")
	}
	nodeEph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return Envelope{}, nil, err
	}
	ss2, err := nodeEph.ECDH(clientEph)
	if err != nil {
		return Envelope{}, nil, err
	}
	ikm := append(append([]byte{}, ss...), ss2...)
	c2s, err := derive(ikm, c2sLabel, 32)
	if err != nil {
		return Envelope{}, nil, err
	}
	s2c, err := derive(ikm, s2cLabel, 32)
	if err != nil {
		return Envelope{}, nil, err
	}
	sessID := fmt.Sprintf("%s-%d", hello.ClientID, time.Now().UnixNano())
	expiry := time.Now().Add(sessionTTL)
	ackBody, err := json.Marshal(handshakeAck{
		NodeID:    local.ID,
		SessionID: sessID,
		Expiry:    expiry.Unix(),
	})
	if err != nil {
		return Envelope{}, nil, err
	}
	ackKey, err := derive(ikm, ackLabel, 32)
	if err != nil {
		return Envelope{}, nil, err
	}
	nonce, ct, err := seal(ackKey, ackBody)
	if err != nil {
		return Envelope{}, nil, err
	}
	sess := &Session{
		ID:         sessID,
		PeerID:     hello.ClientID,
		LocalID:    local.ID,
		c2s:        c2s,
		s2c:        s2c,
		Expiry:     expiry,
		clientSend: false,
	}
	out := Envelope{
		Version:    version,
		From:       local.ID,
		To:         hello.ClientID,
		Kind:       "handshake",
		EphPub:     nodeEph.PublicKey().Bytes(),
		Nonce:      nonce,
		Ciphertext: ct,
	}
	return out, sess, nil
}

// FinishHandshake 客户端完成握手。
func FinishHandshake(local *Identity, peer PublicIdentity, clientEph *ecdh.PrivateKey, env Envelope) (*Session, error) {
	if env.Kind != "handshake" {
		return nil, errors.New("not a handshake envelope")
	}
	peerIdent, err := peer.X25519Public()
	if err != nil {
		return nil, err
	}
	ss, err := clientEph.ECDH(peerIdent)
	if err != nil {
		return nil, err
	}
	nodeEph, err := ecdh.X25519().NewPublicKey(env.EphPub)
	if err != nil {
		return nil, fmt.Errorf("node eph: %w", err)
	}
	ss2, err := clientEph.ECDH(nodeEph)
	if err != nil {
		return nil, err
	}
	ikm := append(append([]byte{}, ss...), ss2...)
	ackKey, err := derive(ikm, ackLabel, 32)
	if err != nil {
		return nil, err
	}
	plain, err := open(ackKey, env.Nonce, env.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt ack: %w", err)
	}
	var ack handshakeAck
	if err := json.Unmarshal(plain, &ack); err != nil {
		return nil, fmt.Errorf("ack json: %w", err)
	}
	c2s, err := derive(ikm, c2sLabel, 32)
	if err != nil {
		return nil, err
	}
	s2c, err := derive(ikm, s2cLabel, 32)
	if err != nil {
		return nil, err
	}
	return &Session{
		ID:         ack.SessionID,
		PeerID:     peer.ID,
		LocalID:    local.ID,
		c2s:        c2s,
		s2c:        s2c,
		Expiry:     time.Unix(ack.Expiry, 0),
		clientSend: true,
	}, nil
}
