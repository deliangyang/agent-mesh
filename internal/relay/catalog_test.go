package relay

import (
	"strconv"
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

func TestCatalogOmitsUnavailableEngines(t *testing.T) {
	s := New("unused", NewMemory(), nil)
	s.Dir.Upsert(Record{
		Info: protocol.NodeInfo{ID: "n1", Name: "box"},
		Seen: time.Now(),
		Snapshot: protocol.NodeSnapshot{
			Engines: []protocol.EngineInfo{
				{Name: "claude", Title: "Claude", Available: true},
				{Name: "cursor", Title: "Cursor", Available: false},
			},
		},
	})
	cat := s.catalog()
	if len(cat.Engines) != 2 {
		t.Fatalf("engines %#v", cat.Engines)
	}
	byName := map[string]bool{}
	for _, e := range cat.Engines {
		byName[e.Name] = e.Available
	}
	if !byName["claude"] || byName["cursor"] {
		t.Fatalf("availability %#v", byName)
	}
}

func TestCatalogKeepsUnavailableEngines(t *testing.T) {
	s := New("unused", NewMemory(), nil)
	s.Dir.Upsert(Record{
		Info: protocol.NodeInfo{ID: "n1", Name: "box"},
		Seen: time.Now(),
		Snapshot: protocol.NodeSnapshot{
			Engines: []protocol.EngineInfo{
				{Name: "pi", Available: false},
			},
		},
	})
	cat := s.catalog()
	if len(cat.Engines) != 1 || cat.Engines[0].Name != "pi" || cat.Engines[0].Available {
		t.Fatalf("engines %#v", cat.Engines)
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

func TestRankSessionsKeepsPerEngineHistory(t *testing.T) {
	var rows []protocol.Session
	for i := 0; i < 30; i++ {
		id := strconv.Itoa(i)
		rows = append(rows, protocol.Session{Engine: "cursor", ID: "c-" + id, UpdatedAt: int64(i), Title: "old"})
		rows = append(rows, protocol.Session{Engine: "claude", ID: "a-" + id, UpdatedAt: int64(i), Title: "old"})
	}
	rows = append(rows, protocol.Session{Engine: "cursor", ID: "live", Live: true, UpdatedAt: 0})
	got := rankSessions(rows, 5)
	n := map[string]int{}
	live := 0
	for _, s := range got {
		if s.Live {
			live++
			continue
		}
		n[s.Engine]++
	}
	if live != 1 {
		t.Fatalf("live %d", live)
	}
	if n["cursor"] != 5 || n["claude"] != 5 {
		t.Fatalf("per engine %#v total %d", n, len(got))
	}
}

func TestFollowupsIncremental(t *testing.T) {
	old := []protocol.FollowQueue{
		{NodeID: "n", Engine: "cursor", Session: "a", Items: []protocol.FollowItem{{ID: "1", Text: "x"}}},
		{NodeID: "n", Engine: "cursor", Session: "b", Items: []protocol.FollowItem{{ID: "2", Text: "y"}}},
	}
	neu := []protocol.FollowQueue{
		{NodeID: "n", Engine: "cursor", Session: "a", Items: []protocol.FollowItem{{ID: "1", Text: "x"}, {ID: "3", Text: "z"}}},
	}
	changed, removed := diffRows(old, neu, catalogFollowKey)
	if len(changed) != 1 || changed[0].Session != "a" || len(changed[0].Items) != 2 {
		t.Fatalf("changed %#v", changed)
	}
	if len(removed) != 1 || removed[0] != "n|cursor|b" {
		t.Fatalf("removed %#v", removed)
	}
}

func TestCatalogPublishesFollowups(t *testing.T) {
	s := New("unused", NewMemory(), nil)
	_, ch := s.catalogs.subscribe()
	s.Dir.Upsert(Record{
		Info: protocol.NodeInfo{ID: "n1", Name: "box"},
		Seen: time.Now(),
		Snapshot: protocol.NodeSnapshot{
			Followups: []protocol.FollowQueue{
				{Engine: "cursor", Session: "s1", Items: []protocol.FollowItem{{ID: "1", Text: "hi"}}},
			},
		},
	})
	s.publishCatalog()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		select {
		case msg := <-ch:
			if msg.Type != "followups" {
				continue
			}
			if msg.Reset || len(msg.Followups) != 1 || msg.Followups[0].Session != "s1" {
				t.Fatalf("msg %#v", msg)
			}
			if msg.Followups[0].NodeID != "n1" || len(msg.Followups[0].Items) != 1 {
				t.Fatalf("queue %#v", msg.Followups[0])
			}
			return
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	t.Fatal("missing followups push")
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
