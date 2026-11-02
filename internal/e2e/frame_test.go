package e2e

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestEnvelopeBinaryRoundTrip(t *testing.T) {
	env := Envelope{
		Version:    1,
		From:       "client-abc",
		To:         "node-xyz",
		Kind:       "rpc",
		Nonce:      []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
		Ciphertext: bytes.Repeat([]byte{0xab}, 64),
	}
	raw, err := env.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var got Envelope
	if err := got.UnmarshalBinary(raw); err != nil {
		t.Fatal(err)
	}
	if got.From != env.From || got.To != env.To || got.Kind != env.Kind || got.Version != env.Version {
		t.Fatalf("meta %#v", got)
	}
	if !bytes.Equal(got.Nonce, env.Nonce) || !bytes.Equal(got.Ciphertext, env.Ciphertext) {
		t.Fatalf("payload mismatch")
	}
}

func TestEnvelopeBinarySmallerThanJSON(t *testing.T) {
	env := Envelope{
		Version:    1,
		From:       "aaaaaaaa",
		To:         "bbbbbbbb",
		Kind:       "stream",
		Nonce:      make([]byte, 12),
		Ciphertext: bytes.Repeat([]byte("x"), 1024),
	}
	bin, err := env.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	js, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if len(bin) >= len(js) {
		t.Fatalf("binary %d should beat json %d", len(bin), len(js))
	}
}

func TestEnvelopeBinaryHandshakeEph(t *testing.T) {
	env := Envelope{
		Version:    1,
		From:       "c",
		To:         "n",
		Kind:       "handshake",
		EphPub:     bytes.Repeat([]byte{9}, 32),
		Nonce:      make([]byte, 12),
		Ciphertext: []byte("ct"),
	}
	raw, err := env.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var got Envelope
	if err := got.UnmarshalBinary(raw); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.EphPub, env.EphPub) {
		t.Fatalf("eph %#v", got.EphPub)
	}
}

func TestDecodeEnvelopeJSONAndBinary(t *testing.T) {
	env := Envelope{Version: 1, From: "a", To: "b", Kind: "rpc", Nonce: []byte{1}, Ciphertext: []byte{2}}
	js, _ := json.Marshal(env)
	r := httptest.NewRequest("POST", "/", bytes.NewReader(js))
	r.Header.Set("Content-Type", "application/json")
	got, err := DecodeEnvelope(r, js)
	if err != nil || got.From != "a" {
		t.Fatalf("json %#v %v", got, err)
	}
	bin, _ := env.MarshalBinary()
	r2 := httptest.NewRequest("POST", "/", bytes.NewReader(bin))
	r2.Header.Set("Content-Type", ContentTypeEnvelope)
	got2, err := DecodeEnvelope(r2, bin)
	if err != nil || got2.From != "a" || !bytes.Equal(got2.Ciphertext, []byte{2}) {
		t.Fatalf("bin %#v %v", got2, err)
	}
}

func TestStreamFrameRoundTrip(t *testing.T) {
	env := Envelope{Version: 1, From: "a", To: "b", Kind: "stream", Nonce: []byte{1, 2}, Ciphertext: []byte("hi")}
	var buf bytes.Buffer
	if err := WriteStreamFrame(&buf, env, true); err != nil {
		t.Fatal(err)
	}
	got, err := ReadStreamFrame(&buf, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "stream" || !bytes.Equal(got.Ciphertext, []byte("hi")) {
		t.Fatalf("%#v", got)
	}
}
