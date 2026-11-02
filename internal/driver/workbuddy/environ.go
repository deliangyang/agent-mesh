package workbuddy

import (
	"bytes"
	"encoding/binary"
	"strings"
	"unicode/utf16"
)

// readProcEnviron returns the environment block of pid when the OS allows it.
func readProcEnviron(pid int) map[string]string {
	if pid <= 0 {
		return nil
	}
	return processEnviron(pid)
}

// parseEnvironNUL parses a NUL-separated KEY=VAL block (Linux /proc/*/environ).
func parseEnvironNUL(raw []byte) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	out := map[string]string{}
	for _, part := range bytes.Split(raw, []byte{0}) {
		if len(part) == 0 {
			continue
		}
		k, v, ok := strings.Cut(string(part), "=")
		if ok {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseEnvironUTF16 parses a Windows UTF-16LE environment block (NUL-separated entries, double-NUL end).
func parseEnvironUTF16(raw []byte) map[string]string {
	if len(raw) < 2 {
		return nil
	}
	u16 := make([]uint16, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		u16[i/2] = binary.LittleEndian.Uint16(raw[i:])
	}
	out := map[string]string{}
	start := 0
	for i := 0; i < len(u16); i++ {
		if u16[i] != 0 {
			continue
		}
		if i == start {
			break // double NUL → end of block
		}
		s := string(utf16.Decode(u16[start:i]))
		if k, v, ok := strings.Cut(s, "="); ok {
			out[k] = v
		}
		start = i + 1
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
