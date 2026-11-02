//go:build !windows

package main

import (
	"os"
	"testing"
)

func TestHasDisplay(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	if hasDisplay() {
		t.Fatalf("empty")
	}
	t.Setenv("DISPLAY", ":0")
	if !hasDisplay() {
		t.Fatalf("DISPLAY")
	}
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	if !hasDisplay() {
		t.Fatalf("WAYLAND")
	}
	_ = os.Getenv("HOME")
}
