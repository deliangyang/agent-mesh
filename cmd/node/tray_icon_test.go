package main

import (
	"testing"

	"agent-mesh/internal/meshicon"
)

func TestMeshTrayIconPixmaps(t *testing.T) {
	rows := meshTrayIconPixmaps()
	if len(rows) != len(meshicon.TraySizes) {
		t.Fatalf("len=%d want %d", len(rows), len(meshicon.TraySizes))
	}
	for i, p := range rows {
		want := int32(meshicon.TraySizes[i])
		if p.Width != want || p.Height != want {
			t.Fatalf("[%d] size %dx%d want %d", i, p.Width, p.Height, want)
		}
		need := int(p.Width * p.Height * 4)
		if len(p.Data) != need {
			t.Fatalf("[%d] data %d want %d", i, len(p.Data), need)
		}
		cx, cy := int(p.Width/2), int(p.Height/2)
		off := (cy*int(p.Width) + cx) * 4
		if p.Data[off] == 0 {
			t.Fatalf("[%d] center alpha is 0", i)
		}
	}
}

func TestRGBAToARGBNetworkOrder(t *testing.T) {
	p := meshTrayIconPixmaps()[0]
	if len(p.Data) < 4 {
		t.Fatal("short")
	}
	// Rounded corners are transparent; sample near the center.
	cx, cy := int(p.Width/2), int(p.Height/2)
	off := (cy*int(p.Width) + cx) * 4
	if p.Data[off] != 0xff {
		t.Fatalf("center alpha %#x", p.Data[off])
	}
}
