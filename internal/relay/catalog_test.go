package relay

import (
	"testing"
	"time"

	"agent-mesh/internal/openai"
	"agent-mesh/internal/protocol"
)

func TestDiffRowsIncremental(t *testing.T) {
	old := []protocol.Session{
		{NodeID: "n", Engine: "workbuddy", ID: "a", Busy: false, UpdatedAt: 1},
		{NodeID: "n", Engine: "workbuddy", ID: "b", Busy: false, UpdatedAt: 1},
	}
	neu := []protocol.Session{
		{NodeID: "n", Engine: "workbuddy", ID: "a", Busy: true, UpdatedAt: 2},
		{NodeID: "n", Engine: "workbuddy", ID: "c", Busy: true, UpdatedAt: 3},
	}
	changed, removed := diffRows(old, neu, catalogSessionKey)
	if len(changed) != 2 {
		t.Fatalf("changed %#v", changed)
	}
	if len(removed) != 1 || removed[0] != "n|workbuddy|b" {
		t.Fatalf("removed %#v", removed)
	}
}

func TestDiffRowsUnchanged(t *testing.T) {
	rows := []openai.ModelCard{{ID: "n/workbuddy/m", OwnedBy: "workbuddy"}}
	changed, removed := diffRows(rows, rows, catalogModelKey)
	if len(changed) != 0 || len(removed) != 0 {
		t.Fatalf("changed %#v removed %#v", changed, removed)
	}
}

func TestCatalogStripsLastSeen(t *testing.T) {
	s := New("unused", NewMemory(), nil)
	s.Dir.Upsert(Record{
		Info: protocol.NodeInfo{ID: "n1", Name: "box", LastSeen: 99},
		Seen: time.Now(),
		Snapshot: protocol.NodeSnapshot{
			Models: []protocol.Model{{ID: "m1", Engine: "workbuddy", Name: "m1"}},
		},
	})
	cat := s.catalog()
	if len(cat.Nodes) != 1 || cat.Nodes[0].LastSeen != 0 {
		t.Fatalf("nodes %#v", cat.Nodes)
	}
	if len(cat.Models) != 1 || cat.Models[0].Created != 0 {
		t.Fatalf("models %#v", cat.Models)
	}
	s.publishCatalog()
	s.publishCatalog()
}

func TestCatalogSendAfterUnsubscribe(t *testing.T) {
	h := newCatalogHub()
	id, ch := h.subscribe()
	h.unsubscribe(id)
	h.send(id, BusMsg{Type: "conversation"})
	h.publish(BusMsg{Type: "nodes"})
	select {
	case <-ch:
		t.Fatal("unsubscribed client should not receive")
	default:
	}
}
