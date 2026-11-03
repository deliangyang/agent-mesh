package e2e

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Fixed seeds for cross-language vectors (not for production use).
var (
	vectorEdSeed = mustDecode("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	vectorXPriv  = mustDecode("a0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf")
	vectorEph    = mustDecode("c0c1c2c3c4c5c6c7c8c9cacbcccdcecfd0d1d2d3d4d5d6d7d8d9dadbdcdddedf")
	vectorNodeEd = mustDecode("fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210")
	vectorNodeX  = mustDecode("101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f")
)

func mustDecode(hexStr string) []byte {
	b, err := hex.DecodeString(hexStr)
	if err != nil {
		panic(err)
	}
	return b
}

func identityFromSeeds(edSeed, xPriv []byte) *Identity {
	priv := ed25519.NewKeyFromSeed(edSeed)
	pub := priv.Public().(ed25519.PublicKey)
	x, err := ecdh.X25519().NewPrivateKey(xPriv)
	if err != nil {
		panic(err)
	}
	return &Identity{
		ID:      hex.EncodeToString(pub[:8]),
		Sign:    priv,
		SignPub: pub,
		X25519:  x,
	}
}

func TestWriteWebVectors(t *testing.T) {
	client := identityFromSeeds(vectorEdSeed, vectorXPriv)
	node := identityFromSeeds(vectorNodeEd, vectorNodeX)

	eph, err := ecdh.X25519().NewPrivateKey(vectorEph)
	if err != nil {
		t.Fatal(err)
	}
	peerPub, err := node.Public().X25519Public()
	if err != nil {
		t.Fatal(err)
	}
	ss, err := eph.ECDH(peerPub)
	if err != nil {
		t.Fatal(err)
	}
	helloKey, err := derive(ss, helloLabel, 32)
	if err != nil {
		t.Fatal(err)
	}
	helloBody := []byte(`{"client_id":"` + client.ID + `","unix":1700000000,"random":"AQIDBAUGBwgJCgsMDQ4PEA=="}`)
	// Fixed nonce for deterministic seal: use open/seal with known key via raw aead.
	nonce := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	aead, err := aeadFrom(helloKey)
	if err != nil {
		t.Fatal(err)
	}
	helloCT := aead.Seal(nil, nonce, helloBody, nil)

	helloEnv := Envelope{
		Version:    version,
		From:       client.ID,
		To:         node.ID,
		Kind:       "handshake",
		EphPub:     eph.PublicKey().Bytes(),
		Nonce:      nonce,
		Ciphertext: helloCT,
	}

	// Accept with fixed node eph by replaying AcceptHandshake is non-deterministic.
	// Instead finish a controlled handshake: node uses vectorEph as its "node eph" for ack path.
	clientEphPub, err := ecdh.X25519().NewPublicKey(eph.PublicKey().Bytes())
	if err != nil {
		t.Fatal(err)
	}
	// Client hello ECDH uses eph×node.static; AcceptHandshake uses node.static×clientEph — same ss.
	ssCheck, err := node.X25519.ECDH(clientEphPub)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(ssCheck) != hex.EncodeToString(ss) {
		t.Fatalf("ss mismatch")
	}
	nodeEphSeed := mustDecode("303132333435363738393a3b3c3d3e3f404142434445464748494a4b4c4d4e4f")
	nodeEph, err := ecdh.X25519().NewPrivateKey(nodeEphSeed)
	if err != nil {
		t.Fatal(err)
	}
	ss2, err := nodeEph.ECDH(clientEphPub)
	if err != nil {
		t.Fatal(err)
	}
	ikm := append(append([]byte{}, ss...), ss2...)
	c2s, err := derive(ikm, c2sLabel, 32)
	if err != nil {
		t.Fatal(err)
	}
	s2c, err := derive(ikm, s2cLabel, 32)
	if err != nil {
		t.Fatal(err)
	}
	ackKey, err := derive(ikm, ackLabel, 32)
	if err != nil {
		t.Fatal(err)
	}
	ackBody := []byte(`{"node_id":"` + node.ID + `","session_id":"sess-vector-1","expiry":1800000000}`)
	ackAEAD, err := aeadFrom(ackKey)
	if err != nil {
		t.Fatal(err)
	}
	ackNonce := []byte{12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	ackCT := ackAEAD.Seal(nil, ackNonce, ackBody, nil)
	ackEnv := Envelope{
		Version:    version,
		From:       node.ID,
		To:         client.ID,
		Kind:       "handshake",
		EphPub:     nodeEph.PublicKey().Bytes(),
		Nonce:      ackNonce,
		Ciphertext: ackCT,
	}

	clientSess := &Session{
		ID:         "sess-vector-1",
		PeerID:     node.ID,
		LocalID:    client.ID,
		c2s:        c2s,
		s2c:        s2c,
		Expiry:     time.Unix(1800000000, 0),
		clientSend: true,
	}
	rpcPlain := []byte(`{"id":"1","method":"list"}`)
	rpcEnv, err := clientSess.Seal(rpcPlain)
	if err != nil {
		t.Fatal(err)
	}
	bin, err := rpcEnv.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	frameEnv := Envelope{
		Version: 1, From: "a", To: "b", Kind: "stream",
		Nonce: []byte{1, 2}, Ciphertext: []byte("hi"),
	}
	frameBin, err := frameEnv.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	doc := map[string]any{
		"client": map[string]string{
			"id":             client.ID,
			"ed25519_seed":   base64.StdEncoding.EncodeToString(vectorEdSeed),
			"x25519_private": base64.StdEncoding.EncodeToString(vectorXPriv),
			"sign_pub":       base64.StdEncoding.EncodeToString(client.SignPub),
			"x25519_pub":     base64.StdEncoding.EncodeToString(client.X25519.PublicKey().Bytes()),
		},
		"node": map[string]string{
			"id":             node.ID,
			"ed25519_seed":   base64.StdEncoding.EncodeToString(vectorNodeEd),
			"x25519_private": base64.StdEncoding.EncodeToString(vectorNodeX),
			"sign_pub":       base64.StdEncoding.EncodeToString(node.SignPub),
			"x25519_pub":     base64.StdEncoding.EncodeToString(node.X25519.PublicKey().Bytes()),
		},
		"client_eph_private": base64.StdEncoding.EncodeToString(vectorEph),
		"client_eph_public":  base64.StdEncoding.EncodeToString(eph.PublicKey().Bytes()),
		"node_eph_private":   base64.StdEncoding.EncodeToString(nodeEphSeed),
		"node_eph_public":    base64.StdEncoding.EncodeToString(nodeEph.PublicKey().Bytes()),
		"ss":                 base64.StdEncoding.EncodeToString(ss),
		"ss2":                base64.StdEncoding.EncodeToString(ss2),
		"hello_key":          base64.StdEncoding.EncodeToString(helloKey),
		"hello_body":         string(helloBody),
		"hello_nonce":        base64.StdEncoding.EncodeToString(nonce),
		"hello_ct":           base64.StdEncoding.EncodeToString(helloCT),
		"hello_env_bin":      base64.StdEncoding.EncodeToString(mustBin(helloEnv)),
		"ack_key":            base64.StdEncoding.EncodeToString(ackKey),
		"ack_body":           string(ackBody),
		"ack_nonce":          base64.StdEncoding.EncodeToString(ackNonce),
		"ack_ct":             base64.StdEncoding.EncodeToString(ackCT),
		"ack_env_bin":        base64.StdEncoding.EncodeToString(mustBin(ackEnv)),
		"c2s":                base64.StdEncoding.EncodeToString(c2s),
		"s2c":                base64.StdEncoding.EncodeToString(s2c),
		"rpc_plain":          string(rpcPlain),
		"rpc_env_bin":        base64.StdEncoding.EncodeToString(bin),
		"rpc_nonce":          base64.StdEncoding.EncodeToString(rpcEnv.Nonce),
		"rpc_ct":             base64.StdEncoding.EncodeToString(rpcEnv.Ciphertext),
		"frame_bin":          base64.StdEncoding.EncodeToString(frameBin),
	}

	out := filepath.Join("..", "relay", "web", "src", "e2e", "vectors.json")
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustBin(env Envelope) []byte {
	b, err := env.MarshalBinary()
	if err != nil {
		panic(err)
	}
	return b
}
