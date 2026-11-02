// Package followup persists a session follow-up queue on the engine machine.
package followup

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"agent-mesh/internal/driver"
)

// Item is a queued follow-up.
type Item struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Created int64  `json:"created"`
}

// File is a snapshot of one session's follow-up queue.
type File struct {
	Engine  string `json:"engine"`
	Session string `json:"session"`
	CWD     string `json:"cwd,omitempty"`
	Items   []Item `json:"items"`
}

var (
	mu   sync.Mutex
	root string
)

// SetRoot is the test data dir. Empty falls back to ~/.agent-mesh/followups.
func SetRoot(dir string) {
	mu.Lock()
	defer mu.Unlock()
	root = dir
}

func baseDir() string {
	if root != "" {
		return root
	}
	if env := strings.TrimSpace(os.Getenv("AGENT_MESH_HOME")); env != "" {
		return filepath.Join(env, "followups")
	}
	return filepath.Join(driver.Home(), ".agent-mesh", "followups")
}

func safeName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, string(filepath.Separator), "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	s = strings.ReplaceAll(s, "..", "_")
	if s == "" {
		return "_"
	}
	if len(s) > 160 {
		s = s[:160]
		for len(s) > 0 && !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}

func pathOf(engine, session string) string {
	return filepath.Join(baseDir(), safeName(engine), safeName(session)+".json")
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func readFile(path string) (File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return File{}, nil
		}
		return File{}, err
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return File{}, err
	}
	if f.Items == nil {
		f.Items = []Item{}
	}
	return f, nil
}

func writeFile(path string, f File) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if len(f.Items) == 0 {
		_ = os.Remove(path)
		return nil
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// List reads a session queue.
func List(engine, session string) (File, error) {
	if engine == "" || session == "" {
		return File{Engine: engine, Session: session, Items: []Item{}}, nil
	}
	mu.Lock()
	defer mu.Unlock()
	f, err := readFile(pathOf(engine, session))
	if err != nil {
		return File{}, err
	}
	f.Engine = engine
	f.Session = session
	if f.Items == nil {
		f.Items = []Item{}
	}
	return f, nil
}

// Push appends an item and returns the snapshot after write.
func Push(engine, session, cwd, text string) (File, error) {
	text = strings.TrimSpace(text)
	if engine == "" || session == "" {
		return File{}, fmt.Errorf("engine and session are required")
	}
	if text == "" {
		return File{}, fmt.Errorf("text is required")
	}
	mu.Lock()
	defer mu.Unlock()
	path := pathOf(engine, session)
	f, err := readFile(path)
	if err != nil {
		return File{}, err
	}
	f.Engine = engine
	f.Session = session
	if cwd != "" {
		f.CWD = cwd
	}
	f.Items = append(f.Items, Item{ID: newID(), Text: text, Created: time.Now().Unix()})
	if err := writeFile(path, f); err != nil {
		return File{}, err
	}
	return f, nil
}

// Update updates one item's text.
func Update(engine, session, id, text string) (File, error) {
	text = strings.TrimSpace(text)
	if engine == "" || session == "" || id == "" {
		return File{}, fmt.Errorf("engine, session, and id are required")
	}
	if text == "" {
		return File{}, fmt.Errorf("text is required")
	}
	mu.Lock()
	defer mu.Unlock()
	path := pathOf(engine, session)
	f, err := readFile(path)
	if err != nil {
		return File{}, err
	}
	found := false
	for i := range f.Items {
		if f.Items[i].ID == id {
			f.Items[i].Text = text
			found = true
			break
		}
	}
	if !found {
		return File{}, fmt.Errorf("follow-up %s does not exist", id)
	}
	f.Engine = engine
	f.Session = session
	if err := writeFile(path, f); err != nil {
		return File{}, err
	}
	return f, nil
}

// Delete deletes one item.
func Delete(engine, session, id string) (File, error) {
	if engine == "" || session == "" || id == "" {
		return File{}, fmt.Errorf("engine, session, and id are required")
	}
	mu.Lock()
	defer mu.Unlock()
	path := pathOf(engine, session)
	f, err := readFile(path)
	if err != nil {
		return File{}, err
	}
	out := f.Items[:0]
	found := false
	for _, it := range f.Items {
		if it.ID == id {
			found = true
			continue
		}
		out = append(out, it)
	}
	if !found {
		return File{}, fmt.Errorf("follow-up %s does not exist", id)
	}
	f.Items = out
	f.Engine = engine
	f.Session = session
	if err := writeFile(path, f); err != nil {
		return File{}, err
	}
	if f.Items == nil {
		f.Items = []Item{}
	}
	return f, nil
}

// Pop pops the oldest item. ok is false when the queue is empty.
func Pop(engine, session string) (Item, File, bool, error) {
	if engine == "" || session == "" {
		return Item{}, File{}, false, fmt.Errorf("engine and session are required")
	}
	mu.Lock()
	defer mu.Unlock()
	path := pathOf(engine, session)
	f, err := readFile(path)
	if err != nil {
		return Item{}, File{}, false, err
	}
	if len(f.Items) == 0 {
		f.Engine = engine
		f.Session = session
		f.Items = []Item{}
		return Item{}, f, false, nil
	}
	it := f.Items[0]
	f.Items = append([]Item{}, f.Items[1:]...)
	f.Engine = engine
	f.Session = session
	if err := writeFile(path, f); err != nil {
		return Item{}, File{}, false, err
	}
	if f.Items == nil {
		f.Items = []Item{}
	}
	return it, f, true, nil
}

// Unshift pushes an item back to the front (used when send fails).
func Unshift(engine, session, cwd string, it Item) error {
	if engine == "" || session == "" || it.Text == "" {
		return fmt.Errorf("engine, session, and text are required")
	}
	if it.ID == "" {
		it.ID = newID()
	}
	mu.Lock()
	defer mu.Unlock()
	path := pathOf(engine, session)
	f, err := readFile(path)
	if err != nil {
		return err
	}
	f.Engine = engine
	f.Session = session
	if cwd != "" {
		f.CWD = cwd
	}
	for _, cur := range f.Items {
		if cur.ID == it.ID {
			return nil
		}
	}
	f.Items = append([]Item{it}, f.Items...)
	return writeFile(path, f)
}

// ListAll scans every non-empty queue on disk.
func ListAll() ([]File, error) {
	mu.Lock()
	defer mu.Unlock()
	dir := baseDir()
	engs, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []File
	for _, eng := range engs {
		if !eng.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(dir, eng.Name()))
		if err != nil {
			continue
		}
		for _, fi := range files {
			if fi.IsDir() || !strings.HasSuffix(fi.Name(), ".json") {
				continue
			}
			f, err := readFile(filepath.Join(dir, eng.Name(), fi.Name()))
			if err != nil || len(f.Items) == 0 {
				continue
			}
			if f.Engine == "" {
				f.Engine = eng.Name()
			}
			if f.Session == "" {
				f.Session = strings.TrimSuffix(fi.Name(), ".json")
			}
			out = append(out, f)
		}
	}
	return out, nil
}
