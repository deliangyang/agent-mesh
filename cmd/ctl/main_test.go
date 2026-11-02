package main

import "testing"

func TestFormatPlatform(t *testing.T) {
	cases := []struct {
		os, arch, want string
	}{
		{"windows", "amd64", "windows/amd64"},
		{"darwin", "arm64", "darwin/arm64"},
		{"linux", "", "linux"},
		{"", "amd64", "amd64"},
		{"", "", "-"},
		{"  windows  ", " amd64 ", "windows/amd64"},
	}
	for _, tc := range cases {
		if got := formatPlatform(tc.os, tc.arch); got != tc.want {
			t.Fatalf("formatPlatform(%q,%q)=%q want %q", tc.os, tc.arch, got, tc.want)
		}
	}
}
