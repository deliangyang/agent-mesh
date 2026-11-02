package driver

import (
	"testing"

	"agent-mesh/internal/protocol"
)

func TestDefaultCapsKnownEngines(t *testing.T) {
	c := DefaultCaps("claude")
	if !c.Ask || !c.Tools || !c.Diff || !c.Followups {
		t.Fatalf("%#v", c)
	}
	c = DefaultCaps("unknown-engine")
	if c.Ask || !c.Tools || !c.Diff || !c.Followups {
		t.Fatalf("unknown %#v", c)
	}
}

func TestCapsOrDefault(t *testing.T) {
	got := CapsOrDefault("pi", protocol.EngineCaps{})
	if !got.Ask || !got.Tools {
		t.Fatalf("default %#v", got)
	}
	got = CapsOrDefault("pi", protocol.EngineCaps{Tools: true})
	if got.Ask || !got.Tools || got.Diff {
		t.Fatalf("probe override %#v", got)
	}
}
