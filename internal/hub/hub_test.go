package hub

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/protocol"
)

type fakeDriver struct {
	name  string
	list  []protocol.Session
	watch func(ctx context.Context, sessionID string) (<-chan protocol.Event, error)
}

func (d *fakeDriver) Name() string { return d.name }

func (d *fakeDriver) List(ctx context.Context, all bool) ([]protocol.Session, error) {
	return d.list, nil
}

func (d *fakeDriver) History(ctx context.Context, sessionID string, tail int) ([]protocol.Event, error) {
	return []protocol.Event{}, nil
}

func (d *fakeDriver) Watch(ctx context.Context, sessionID string) (<-chan protocol.Event, error) {
	if d.watch != nil {
		return d.watch(ctx, sessionID)
	}
	ch := make(chan protocol.Event)
	close(ch)
	return ch, nil
}

func (d *fakeDriver) Reply(ctx context.Context, sessionID, text, model string) (<-chan protocol.Event, error) {
	return d.Watch(ctx, sessionID)
}

func (d *fakeDriver) Send(ctx context.Context, cwd, text, resume, model string) (<-chan protocol.Event, error) {
	return d.Watch(ctx, resume)
}

func (d *fakeDriver) Probe(ctx context.Context) protocol.EngineInfo {
	return protocol.EngineInfo{Name: d.name, Available: true}
}

func (d *fakeDriver) Models(ctx context.Context) ([]protocol.Model, error) {
	return []protocol.Model{}, nil
}

func TestPumpEndsAfterDoneWhileWatchStaysOpen(t *testing.T) {
	h := New(nil, "n", "box", nil)
	evs := make(chan protocol.Event, 4)
	evs <- protocol.Event{Type: "assistant", Text: "hi", Session: "s1"}
	evs <- protocol.Event{Type: "log", Session: "s1", Done: true}
	chunks := h.Pump("stub", "s1", "stub", evs)
	got := 0
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-chunks:
			if !ok {
				if got < 2 {
					t.Fatalf("chunks %d", got)
				}
				return
			}
			got++
		case <-deadline:
			t.Fatal("Pump did not finish after Done")
		}
	}
}

func TestRunWatchRestartsAfterError(t *testing.T) {
	var n atomic.Int32
	d := &fakeDriver{
		name: "stub",
		list: []protocol.Session{{Engine: "stub", ID: "s1", Live: true}},
		watch: func(ctx context.Context, sessionID string) (<-chan protocol.Event, error) {
			if n.Add(1) == 1 {
				return nil, fmt.Errorf("boom")
			}
			ch := make(chan protocol.Event)
			go func() {
				<-ctx.Done()
				close(ch)
			}()
			return ch, nil
		},
	}
	set := driver.NewSet()
	set.Register(d)
	h := New(set, "n", "box", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.scan(ctx)
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if n.Load() >= 2 && h.watching("stub", "s1") {
			return
		}
		h.scan(ctx)
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("watch calls %d watching %v", n.Load(), h.watching("stub", "s1"))
}
