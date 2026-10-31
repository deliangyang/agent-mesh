package e2e

import (
	"bytes"
	"testing"
)

func TestHandshakeRoundTrip(t *testing.T) {
	node, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	client, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	hello, eph, err := BeginHandshake(client, node.Public())
	if err != nil {
		t.Fatal(err)
	}
	ack, nodeSess, err := AcceptHandshake(node, hello)
	if err != nil {
		t.Fatal(err)
	}
	clientSess, err := FinishHandshake(client, node.Public(), eph, ack)
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte(`{"method":"list"}`)
	env, err := clientSess.Seal(msg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := nodeSess.Open(env)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("got %q want %q", got, msg)
	}
	reply := []byte(`{"ok":true}`)
	env2, err := nodeSess.Seal(reply)
	if err != nil {
		t.Fatal(err)
	}
	got2, err := clientSess.Open(env2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got2, reply) {
		t.Fatalf("reply got %q want %q", got2, reply)
	}
}
