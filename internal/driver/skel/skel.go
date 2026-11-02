// Package skel is a copy-paste template for a new local coding-agent driver.
//
// It is NOT registered in factory.Attach. To add an engine:
//  1. Copy this package to internal/driver/<name>/.
//  2. Implement List/History/Watch/Reply/Send/Models against the vendor CLI or session files.
//  3. Prefer driver.ParseJSONL, RunPrint, WatchJSONL, and AskFromTool.
//  4. Register in factory.AllNames + Attach; keep cmd/node -engines and slash roots in sync.
//
// See AGENTS.md "Add an engine".
package skel

import (
	"context"
	"fmt"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/protocol"
)

// Compile-time check that the stub satisfies driver.Driver.
var _ driver.Driver = (*Driver)(nil)

// Driver is a stub local coding-agent engine.
type Driver struct {
	bin string
}

// New creates the stub driver.
func New() *Driver {
	bin := driver.LookCLI([]string{"SKEL_BIN"}, []string{"skel-agent"}, nil)
	return &Driver{bin: bin}
}

func (d *Driver) Name() string { return "skel" }

func (d *Driver) Probe(ctx context.Context) protocol.EngineInfo {
	_ = ctx
	info := protocol.EngineInfo{
		Name:   d.Name(),
		Title:  "Skel (template)",
		Binary: d.bin,
		Caps:   driver.DefaultCaps(d.Name()),
	}
	info.Available = driver.BinExists(d.bin)
	if info.Available {
		info.Version = driver.BinVersion(ctx, d.bin)
	}
	return info
}

func (d *Driver) Models(ctx context.Context) ([]protocol.Model, error) {
	_ = ctx
	return []protocol.Model{{
		ID: d.Name() + "/default", Engine: d.Name(), Name: "default", Title: "default", Default: true,
	}}, nil
}

func (d *Driver) List(ctx context.Context, all bool) ([]protocol.Session, error) {
	_ = ctx
	_ = all
	return nil, nil
}

func (d *Driver) History(ctx context.Context, sessionID string, tail int) ([]protocol.Event, error) {
	_ = ctx
	_ = sessionID
	_ = tail
	return nil, nil
}

func (d *Driver) Watch(ctx context.Context, sessionID string) (<-chan protocol.Event, error) {
	_ = sessionID
	ch := make(chan protocol.Event)
	go func() {
		defer close(ch)
		<-ctx.Done()
	}()
	return ch, nil
}

func (d *Driver) Reply(ctx context.Context, sessionID, text, model string) (<-chan protocol.Event, error) {
	_ = ctx
	_ = sessionID
	_ = text
	_ = model
	return nil, fmt.Errorf("skel: Reply not implemented")
}

func (d *Driver) Send(ctx context.Context, cwd, text, resume, model string) (<-chan protocol.Event, error) {
	_ = ctx
	_ = cwd
	_ = text
	_ = resume
	_ = model
	return nil, fmt.Errorf("skel: Send not implemented")
}
