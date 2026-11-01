package driver

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"agent-mesh/internal/protocol"
)

// Driver 一种本地智能代理引擎。
type Driver interface {
	Name() string
	List(ctx context.Context, all bool) ([]protocol.Session, error)
	History(ctx context.Context, sessionID string, tail int) ([]protocol.Event, error)
	Watch(ctx context.Context, sessionID string) (<-chan protocol.Event, error)
	Reply(ctx context.Context, sessionID, text string) (<-chan protocol.Event, error)
	Send(ctx context.Context, cwd, text, resume string) (<-chan protocol.Event, error)
	Probe(ctx context.Context) protocol.EngineInfo
	Models(ctx context.Context) ([]protocol.Model, error)
}

// Set 可插拔引擎集合。
type Set struct {
	mu      sync.RWMutex
	drivers map[string]Driver
	enabled map[string]bool
}

// NewSet 创建空集合。
func NewSet() *Set {
	return &Set{drivers: make(map[string]Driver), enabled: make(map[string]bool)}
}

// Register 注册引擎，默认启用。
func (s *Set) Register(d Driver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.drivers[d.Name()] = d
	if _, ok := s.enabled[d.Name()]; !ok {
		s.enabled[d.Name()] = true
	}
}

// SetEnabled 开关引擎。关闭后不再出现在会话列表与模型列表中。
func (s *Set) SetEnabled(name string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.drivers[name]; !ok {
		return fmt.Errorf("unknown engine %s", name)
	}
	s.enabled[name] = on
	return nil
}

// Enabled 引擎是否启用。
func (s *Set) Enabled(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled[name]
}

// Get 按名取引擎（不论是否启用）。
func (s *Set) Get(name string) (Driver, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.drivers[name]
	if !ok {
		return nil, fmt.Errorf("unknown engine %s", name)
	}
	return d, nil
}

// Names 已注册引擎名。
func (s *Set) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.drivers))
	for k := range s.drivers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *Set) snapshot() []Driver {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Driver, 0, len(s.drivers))
	names := make([]string, 0, len(s.drivers))
	for k := range s.drivers {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		out = append(out, s.drivers[k])
	}
	return out
}

// Engines 探测全部引擎状态。
func (s *Set) Engines(ctx context.Context) []protocol.EngineInfo {
	ds := s.snapshot()
	out := make([]protocol.EngineInfo, len(ds))
	var wg sync.WaitGroup
	for i, d := range ds {
		wg.Add(1)
		go func(i int, d Driver) {
			defer wg.Done()
			info := d.Probe(ctx)
			info.Name = d.Name()
			info.Enabled = s.Enabled(d.Name())
			if info.Title == "" {
				info.Title = TitleOf(d.Name())
			}
			out[i] = info
		}(i, d)
	}
	wg.Wait()
	return out
}

// Models 汇总模型。engine 为空则全部已启用引擎。
func (s *Set) Models(ctx context.Context, engine string) ([]protocol.Model, error) {
	ds := s.snapshot()
	type slot struct {
		rows []protocol.Model
		err  error
		name string
		skip bool
	}
	slots := make([]slot, len(ds))
	var wg sync.WaitGroup
	for i, d := range ds {
		name := d.Name()
		if engine != "" && name != engine {
			slots[i].skip = true
			continue
		}
		if engine == "" && !s.Enabled(name) {
			slots[i].skip = true
			continue
		}
		wg.Add(1)
		go func(i int, d Driver, name string) {
			defer wg.Done()
			rows, err := d.Models(ctx)
			slots[i] = slot{rows: rows, err: err, name: name}
		}(i, d, name)
	}
	wg.Wait()
	var out []protocol.Model
	for _, item := range slots {
		if item.skip {
			continue
		}
		if item.err != nil {
			if engine != "" {
				return nil, fmt.Errorf("%s models: %w", item.name, item.err)
			}
			continue
		}
		out = append(out, item.rows...)
	}
	return out, nil
}

// ListAll 汇总全部已启用引擎会话。
func (s *Set) ListAll(ctx context.Context, engine string, all bool) ([]protocol.Session, error) {
	ds := s.snapshot()
	if engine != "" {
		for _, d := range ds {
			if d.Name() != engine {
				continue
			}
			if !s.Enabled(engine) {
				return nil, fmt.Errorf("engine %s disabled", engine)
			}
			rows, err := d.List(ctx, all)
			if err != nil {
				return nil, fmt.Errorf("%s list: %w", engine, err)
			}
			if rows == nil {
				rows = []protocol.Session{}
			}
			return rows, nil
		}
		return []protocol.Session{}, nil
	}
	results := make([][]protocol.Session, len(ds))
	var wg sync.WaitGroup
	for i, d := range ds {
		if !s.Enabled(d.Name()) {
			continue
		}
		wg.Add(1)
		go func(i int, d Driver) {
			defer wg.Done()
			rows, err := d.List(ctx, all)
			if err == nil {
				results[i] = rows
			}
		}(i, d)
	}
	wg.Wait()
	out := []protocol.Session{}
	for _, rows := range results {
		out = append(out, rows...)
	}
	return out, nil
}

// DirsOf 从会话汇总开发目录。
func DirsOf(rows []protocol.Session) []protocol.WorkDir {
	type key struct{ eng, path string }
	seen := map[key]int{}
	order := make([]key, 0)
	for _, r := range rows {
		if r.CWD == "" {
			continue
		}
		k := key{r.Engine, r.CWD}
		if _, ok := seen[k]; !ok {
			order = append(order, k)
		}
		seen[k]++
	}
	out := make([]protocol.WorkDir, 0, len(order))
	for _, k := range order {
		out = append(out, protocol.WorkDir{Path: k.path, Engine: k.eng, Count: seen[k]})
	}
	return out
}

// Directories 从会话中汇总开发目录。
func (s *Set) Directories(ctx context.Context) []protocol.WorkDir {
	rows, err := s.ListAll(ctx, "", true)
	if err != nil {
		return []protocol.WorkDir{}
	}
	return DirsOf(rows)
}

type slashMemo struct {
	mu    sync.Mutex
	key   string
	at    time.Time
	items protocol.SlashList
}

var slashCache slashMemo

const slashTTL = 8 * time.Second

// SlashItems 当前引擎在 cwd 下可调用的斜杠命令与技能。
func (s *Set) SlashItems(ctx context.Context, engine, cwd string) protocol.SlashList {
	if engine != "" {
		if _, err := s.Get(engine); err != nil {
			return protocol.SlashList{Items: []protocol.SlashItem{}}
		}
	}
	if ctx != nil && ctx.Err() != nil {
		return protocol.SlashList{Items: []protocol.SlashItem{}}
	}
	key := engine + "\x00" + cwd
	slashCache.mu.Lock()
	if slashCache.key == key && time.Since(slashCache.at) < slashTTL {
		out := slashCache.items
		slashCache.mu.Unlock()
		return out
	}
	slashCache.mu.Unlock()
	out := protocol.SlashList{Items: ScanSlash(engine, cwd, Home())}
	if out.Items == nil {
		out.Items = []protocol.SlashItem{}
	}
	slashCache.mu.Lock()
	slashCache.key = key
	slashCache.at = time.Now()
	slashCache.items = out
	slashCache.mu.Unlock()
	return out
}
