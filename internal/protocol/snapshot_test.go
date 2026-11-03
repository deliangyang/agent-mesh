package protocol

import "testing"

func TestPublicSnapshotStripsSensitive(t *testing.T) {
	in := NodeSnapshot{
		Models:    []Model{{ID: "m1", Engine: "claude", Name: "m1"}},
		Engines:   []EngineInfo{{Name: "claude", Available: true, Binary: "/secret/bin", Detail: "leak"}},
		Sessions:  []Session{{ID: "s1", Title: "secret", CWD: "/home/u"}},
		Dirs:      []WorkDir{{Path: "/home/u"}},
		Followups: []FollowQueue{{Session: "s1", Items: []FollowItem{{Text: "hi"}}}},
	}
	out := PublicSnapshot(in)
	if out.Sessions == nil || out.Dirs == nil || out.Followups == nil {
		t.Fatalf("want empty non-nil slices for merge replace: %#v", out)
	}
	if len(out.Sessions) != 0 || len(out.Dirs) != 0 || len(out.Followups) != 0 {
		t.Fatalf("sensitive fields remain: %#v", out)
	}
	if len(out.Models) != 1 || out.Models[0].ID != "m1" {
		t.Fatalf("models: %#v", out.Models)
	}
	if len(out.Engines) != 1 || out.Engines[0].Binary != "" || out.Engines[0].Detail != "" {
		t.Fatalf("engines: %#v", out.Engines)
	}
	if in.Engines[0].Binary == "" {
		t.Fatal("input mutated")
	}
}
