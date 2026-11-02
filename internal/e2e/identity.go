// Package e2e provides application E2E encryption: the relay sees routing metadata, not plaintext.
package e2e

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"crypto/ecdh"
)

// Identity is a long-term node or client identity: Ed25519 for signatures, X25519 for key exchange.
type Identity struct {
	ID      string
	Sign    ed25519.PrivateKey
	SignPub ed25519.PublicKey
	X25519  *ecdh.PrivateKey
}

type identityFile struct {
	ID            string `json:"id"`
	Ed25519Seed   string `json:"ed25519_seed"`
	X25519Private string `json:"x25519_private"`
}

// Generate creates a new identity.
func Generate() (*Identity, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519: %w", err)
	}
	xpriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate x25519: %w", err)
	}
	id := hex.EncodeToString(pub[:8])
	return &Identity{ID: id, Sign: priv, SignPub: pub, X25519: xpriv}, nil
}

// Public is safe to publish to relays and clients.
func (id *Identity) Public() PublicIdentity {
	return PublicIdentity{
		ID:        id.ID,
		SignPub:   append(ed25519.PublicKey(nil), id.SignPub...),
		X25519Pub: id.X25519.PublicKey().Bytes(),
	}
}

// PublicIdentity is the public identity.
type PublicIdentity struct {
	ID        string `json:"id"`
	SignPub   []byte `json:"sign_pub"`
	X25519Pub []byte `json:"x25519_pub"`
}

// SignBytes signs a message.
func (id *Identity) SignBytes(msg []byte) []byte {
	return ed25519.Sign(id.Sign, msg)
}

// Verify verifies a signature.
func (p PublicIdentity) Verify(msg, sig []byte) bool {
	if len(p.SignPub) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(p.SignPub, msg, sig)
}

// X25519Public parses the peer X25519 public key.
func (p PublicIdentity) X25519Public() (*ecdh.PublicKey, error) {
	return ecdh.X25519().NewPublicKey(p.X25519Pub)
}

// Save writes the key file with mode 0600.
func (id *Identity) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir identity dir: %w", err)
	}
	doc := identityFile{
		ID:            id.ID,
		Ed25519Seed:   base64.StdEncoding.EncodeToString(id.Sign.Seed()),
		X25519Private: base64.StdEncoding.EncodeToString(id.X25519.Bytes()),
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("write identity: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename identity: %w", err)
	}
	return nil
}

// LoadOrCreate loads an identity, or creates one if missing.
func LoadOrCreate(path string) (*Identity, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			id, err := Generate()
			if err != nil {
				return nil, err
			}
			if err := id.Save(path); err != nil {
				return nil, err
			}
			return id, nil
		}
		return nil, fmt.Errorf("read identity: %w", err)
	}
	var doc identityFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse identity: %w", err)
	}
	seed, err := base64.StdEncoding.DecodeString(doc.Ed25519Seed)
	if err != nil {
		return nil, fmt.Errorf("decode ed25519 seed: %w", err)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	xraw, err := base64.StdEncoding.DecodeString(doc.X25519Private)
	if err != nil {
		return nil, fmt.Errorf("decode x25519: %w", err)
	}
	xpriv, err := ecdh.X25519().NewPrivateKey(xraw)
	if err != nil {
		return nil, fmt.Errorf("parse x25519: %w", err)
	}
	pub := priv.Public().(ed25519.PublicKey)
	id := doc.ID
	if id == "" {
		id = hex.EncodeToString(pub[:8])
	}
	return &Identity{ID: id, Sign: priv, SignPub: pub, X25519: xpriv}, nil
}
