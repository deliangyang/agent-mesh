package watchfile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPollJSONLSeesAppendAfterEOF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	if err := os.WriteFile(path, []byte("{\"n\":1}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := PollJSONL(ctx, path, true, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{\"n\":2}\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	select {
	case obj := <-ch:
		if n, _ := obj["n"].(float64); n != 2 {
			t.Fatalf("%#v", obj)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for appended line")
	}
	cancel()
}

func TestPollJSONLHandlesTruncate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.jsonl")
	if err := os.WriteFile(path, []byte("{\"n\":1}\n{\"n\":2}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := PollJSONL(ctx, path, true, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if err := os.WriteFile(path, []byte("{\"n\":9}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case obj := <-ch:
		if n, _ := obj["n"].(float64); n != 9 {
			t.Fatalf("%#v", obj)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for truncated rewrite")
	}
	cancel()
}
