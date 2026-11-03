package watchfile

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"time"
)

// PollJSONL follows new jsonl lines until ctx is canceled.
//
// It reopens the file each poll so Windows writers that lock or replace the
// file still surface new lines (a long-lived handle often stalls at EOF).
func PollJSONL(ctx context.Context, path string, fromEnd bool, interval time.Duration) (<-chan map[string]any, error) {
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	offset := int64(0)
	if fromEnd {
		if st, err := os.Stat(path); err != nil {
			return nil, err
		} else {
			offset = st.Size()
		}
	} else if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	out := make(chan map[string]any, 32)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			next, err := readNewJSONL(path, offset, out, ctx)
			if err == nil {
				offset = next
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
		}
	}()
	return out, nil
}

func readNewJSONL(path string, offset int64, out chan<- map[string]any, ctx context.Context) (int64, error) {
	st, err := os.Stat(path)
	if err != nil {
		return offset, err
	}
	size := st.Size()
	if size < offset {
		// truncated or replaced
		offset = 0
	}
	if size == offset {
		return offset, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return offset, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return offset, err
	}
	r := bufio.NewReader(f)
	pos := offset
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			// Keep incomplete trailing lines for the next poll.
			if err == io.EOF && (len(line) == 0 || line[len(line)-1] != '\n') {
				break
			}
			pos += int64(len(line))
			var obj map[string]any
			if json.Unmarshal(trimNL(line), &obj) == nil {
				select {
				case out <- obj:
				case <-ctx.Done():
					return pos, ctx.Err()
				}
			}
		}
		if err != nil {
			break
		}
	}
	return pos, nil
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
