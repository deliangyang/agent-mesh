package node

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/e2e"
	"agent-mesh/internal/followup"
	"agent-mesh/internal/protocol"
)

type drainDriver struct {
	stubDriver
	busy    atomic.Bool
	replies atomic.Int32
}

func (d *drainDriver) Name() string { return "cursor" }

func (d *drainDriver) List(ctx context.Context, all bool) ([]protocol.Session, error) {
	return []protocol.Session{{Engine: "cursor", ID: "s1", Busy: d.busy.Load()}}, nil
}

func (d *drainDriver) Reply(ctx context.Context, sessionID, text, model string) (<-chan protocol.Event, error) {
	d.replies.Add(1)
	ch := make(chan protocol.Event, 1)
	ch <- protocol.Event{Type: "assistant", Text: "ok", Session: sessionID, Engine: "cursor", Done: true}
	close(ch)
	return ch, nil
}

func TestDrainSessionWaitsBusyThenSends(t *testing.T) {
	root := t.TempDir()
	followup.SetRoot(filepath.Join(root, "followups"))
	t.Cleanup(func() { followup.SetRoot("") })
	ident, err := e2e.Generate()
	if err != nil {
		t.Fatal(err)
	}
	d := &drainDriver{}
	d.busy.Store(true)
	set := driver.NewSet()
	set.Register(d)
	srv := New(ident, "unused", "test", set, nil)
	if _, err := followup.Push("cursor", "s1", "", "next"); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		srv.drainSession("cursor", "s1", "")
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	if d.replies.Load() != 0 {
		t.Fatal("busy 时不应发送")
	}
	d.busy.Store(false)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("idle 后未 drain")
	}
	if d.replies.Load() != 1 {
		t.Fatalf("replies %d", d.replies.Load())
	}
}

func TestPatchFollowQueues(t *testing.T) {
	rows := []protocol.FollowQueue{
		{Engine: "cursor", Session: "s1", Items: []protocol.FollowItem{{ID: "1", Text: "先修"}}},
	}
	rows = patchFollowQueues(rows, protocol.FollowQueue{
		Engine:  "cursor",
		Session: "s1",
		Items:   []protocol.FollowItem{{ID: "1", Text: "先修"}, {ID: "2", Text: "再改"}},
	})
	if len(rows) != 1 || len(rows[0].Items) != 2 {
		t.Fatalf("upsert %#v", rows)
	}
	rows = patchFollowQueues(rows, protocol.FollowQueue{
		Engine:  "claude",
		Session: "s2",
		Items:   []protocol.FollowItem{{ID: "3", Text: "新会话"}},
	})
	if len(rows) != 2 {
		t.Fatalf("add %#v", rows)
	}
	rows = patchFollowQueues(rows, protocol.FollowQueue{Engine: "cursor", Session: "s1"})
	if len(rows) != 1 || rows[0].Session != "s2" {
		t.Fatalf("remove %#v", rows)
	}
}

func TestToFollowQueueEmptyItems(t *testing.T) {
	q := toFollowQueue(followup.File{Engine: "pi", Session: "abc"})
	if q.Engine != "pi" || q.Session != "abc" || q.Items == nil || len(q.Items) != 0 {
		t.Fatalf("%#v", q)
	}
}
