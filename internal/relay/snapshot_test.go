package relay

import (
	"encoding/json"
	"io"
	"testing"
	"time"

	"agent-mesh/internal/openai"
	"agent-mesh/internal/protocol"
)

func TestEmptySnapshotReplacesOld(t *testing.T) {
	m := NewMemory()
	m.Upsert(Record{
		Info: protocol.NodeInfo{ID: "n1"},
		Seen: time.Now(),
		Snapshot: protocol.NodeSnapshot{
			Sessions:  []protocol.Session{{ID: "old", Engine: "cursor"}},
			Followups: []protocol.FollowQueue{{Engine: "cursor", Session: "old", Items: []protocol.FollowItem{{ID: "1"}}}},
			Models:    []protocol.Model{{ID: "cursor/demo", Engine: "cursor", Name: "demo"}},
		},
	})
	m.Upsert(Record{
		Info: protocol.NodeInfo{ID: "n1"},
		Seen: time.Now(),
		Snapshot: protocol.NodeSnapshot{
			Sessions:  []protocol.Session{},
			Followups: []protocol.FollowQueue{},
			Models:    []protocol.Model{{ID: "cursor/demo", Engine: "cursor", Name: "demo"}},
			Engines:   []protocol.EngineInfo{},
			Dirs:      []protocol.WorkDir{},
		},
	})
	got, ok := m.Get("n1")
	if !ok {
		t.Fatal("missing")
	}
	if len(got.Snapshot.Sessions) != 0 {
		t.Fatalf("sessions %#v", got.Snapshot.Sessions)
	}
	if len(got.Snapshot.Followups) != 0 {
		t.Fatalf("followups %#v", got.Snapshot.Followups)
	}
}

func TestNilSnapshotKeepsOld(t *testing.T) {
	m := NewMemory()
	m.Upsert(Record{
		Info: protocol.NodeInfo{ID: "n1"},
		Seen: time.Now(),
		Snapshot: protocol.NodeSnapshot{
			Sessions: []protocol.Session{{ID: "old", Engine: "cursor"}},
		},
	})
	m.Upsert(Record{
		Info:     protocol.NodeInfo{ID: "n1"},
		Seen:     time.Now(),
		Snapshot: protocol.NodeSnapshot{},
	})
	got, _ := m.Get("n1")
	if len(got.Snapshot.Sessions) != 1 || got.Snapshot.Sessions[0].ID != "old" {
		t.Fatalf("kept %#v", got.Snapshot.Sessions)
	}
}

func TestPipeHasStarted(t *testing.T) {
	if !pipeHasStarted(&pipeStatus{error: io.ErrUnexpectedEOF, started: true}) {
		t.Fatal("started")
	}
	if pipeHasStarted(io.ErrUnexpectedEOF) {
		t.Fatal("plain error")
	}
}

func TestTunnelIngestIgnored(t *testing.T) {
	s := New("unused", NewMemory(), nil)
	id, ch := s.ingest.subscribe()
	defer s.ingest.unsubscribe(id)
	chunk := openai.FromEvent("id1", "cursor", "n1", "box", protocol.Event{Type: "assistant", Text: "hi", Session: "s1"})
	raw, err := json.Marshal(chunk)
	if err != nil {
		t.Fatal(err)
	}
	s.applyTunnelIngest(raw)
	select {
	case got := <-ch:
		t.Fatalf("ingest must not publish plaintext: %#v", got)
	case <-time.After(200 * time.Millisecond):
	}
}
