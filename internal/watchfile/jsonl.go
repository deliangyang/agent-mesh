package watchfile

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"time"
)

// PollJSONL follows new jsonl lines until ctx is canceled.
func PollJSONL(ctx context.Context, path string, fromEnd bool, interval time.Duration) (<-chan map[string]any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if fromEnd {
		if _, err := f.Seek(0, 2); err != nil {
			f.Close()
			return nil, err
		}
	}
	out := make(chan map[string]any, 32)
	go func() {
		defer close(out)
		defer f.Close()
		r := bufio.NewReader(f)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			line, err := r.ReadBytes('\n')
			if len(line) > 0 {
				var obj map[string]any
				if json.Unmarshal(trimNL(line), &obj) == nil {
					select {
					case out <- obj:
					case <-ctx.Done():
						return
					}
				}
			}
			if err != nil {
				select {
				case <-ctx.Done():
					return
				case <-time.After(interval):
				}
			}
		}
	}()
	return out, nil
}

func trimNL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

// TailJSONL reads the last few lines.
func TailJSONL(path string, n int, handle func(map[string]any)) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := splitLines(raw)
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	for _, line := range lines {
		var obj map[string]any
		if json.Unmarshal(line, &obj) != nil {
			continue
		}
		handle(obj)
	}
	return nil
}

func splitLines(raw []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, c := range raw {
		if c == '\n' {
			if i > start {
				lines = append(lines, raw[start:i])
			}
			start = i + 1
		}
	}
	if start < len(raw) {
		lines = append(lines, raw[start:])
	}
	return lines
}
