package skel

import (
	"context"
	"testing"
)

func TestNameAndProbe(t *testing.T) {
	d := New()
	if d.Name() != "skel" {
		t.Fatalf("name %q", d.Name())
	}
	info := d.Probe(context.Background())
	if info.Name != "skel" || info.Title == "" {
		t.Fatalf("%#v", info)
	}
	if info.Caps.Ask {
		t.Fatalf("skel should use unknown-engine caps without ask: %#v", info.Caps)
	}
}

func TestModelsStub(t *testing.T) {
	rows, err := New().Models(context.Background())
	if err != nil || len(rows) != 1 || !rows[0].Default {
		t.Fatalf("%v %#v", err, rows)
	}
}
