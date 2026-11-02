package workbuddy

import (
	"path/filepath"
	"testing"
)

func TestPreferGatewayOrdersResumeAndCWD(t *testing.T) {
	gws := []gateway{
		{URL: "http://127.0.0.1:1", Session: "a", CWD: filepath.Clean(`/tmp/a`)},
		{URL: "http://127.0.0.1:2", Session: "b", CWD: filepath.Clean(`/tmp/b`)},
		{URL: "http://127.0.0.1:3", Session: "c", CWD: filepath.Clean(`/tmp/c`)},
	}
	got := preferGateway(gws, filepath.Clean(`/tmp/b`), "c")
	if len(got) != 3 {
		t.Fatalf("len=%d want 3", len(got))
	}
	if got[0].Session != "c" {
		t.Fatalf("first=%q want resume session c", got[0].Session)
	}
	if got[1].Session != "b" {
		t.Fatalf("second=%q want cwd session b", got[1].Session)
	}
}
