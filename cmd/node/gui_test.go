package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNodeUIURL(t *testing.T) {
	if got := nodeUIURL("127.0.0.1:7700"); got != "http://127.0.0.1:7700" {
		t.Fatalf("got %q", got)
	}
	if got := nodeUIURL("0.0.0.0:7700"); got != "http://127.0.0.1:7700" {
		t.Fatalf("bind-all %q", got)
	}
}

func TestExistingNodeHealthy(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ok.Close()
	if !existingNodeHealthy(ok.URL) {
		t.Fatal("expected healthy")
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusServiceUnavailable)
	}))
	defer bad.Close()
	if existingNodeHealthy(bad.URL) {
		t.Fatal("expected unhealthy")
	}
	if existingNodeHealthy("http://127.0.0.1:1") {
		t.Fatal("expected dial failure")
	}
}
