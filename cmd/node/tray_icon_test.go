package main

import "testing"

func TestMeshTrayIconPixmaps(t *testing.T) {
	rows := meshTrayIconPixmaps()
	if len(rows) != 1 {
		t.Fatalf("len=%d", len(rows))
	}
	p := rows[0]
	if p.Width != 32 || p.Height != 32 {
		t.Fatalf("size %dx%d", p.Width, p.Height)
	}
	want := int(p.Width * p.Height * 4)
	if len(p.Data) != want {
		t.Fatalf("data %d want %d", len(p.Data), want)
	}
	// Center pixel should be ink (opaque, dark) or signal; not fully transparent.
	cx, cy := int(p.Width/2), int(p.Height/2)
	off := (cy*int(p.Width) + cx) * 4
	if p.Data[off] == 0 {
		t.Fatalf("center alpha is 0")
	}
}

func TestRGBAToARGBNetworkOrder(t *testing.T) {
	// Smoke: first pixel of generated icon has A then RGB.
	p := meshTrayIconPixmaps()[0]
	if len(p.Data) < 4 {
		t.Fatal("short")
	}
	// Corner is paper: high RGB, opaque.
	if p.Data[0] != 0xff {
		t.Fatalf("corner alpha %#x", p.Data[0])
	}
}
