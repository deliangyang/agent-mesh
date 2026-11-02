package main

import "testing"

func TestNodeUIURL(t *testing.T) {
	if got := nodeUIURL("127.0.0.1:7700"); got != "http://127.0.0.1:7700" {
		t.Fatalf("got %q", got)
	}
	if got := nodeUIURL("0.0.0.0:7700"); got != "http://127.0.0.1:7700" {
		t.Fatalf("bind-all %q", got)
	}
}
