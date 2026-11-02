package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/e2e"
	"agent-mesh/internal/factory"
	"agent-mesh/internal/node"
	"agent-mesh/internal/protocol"
)

// App is the desktop backend: local node plus engine/session/model controls.
type App struct {
	ctx       context.Context
	cancel    context.CancelFunc
	regCancel context.CancelFunc
	log       *slog.Logger

	mu        sync.Mutex
	ident     *e2e.Identity
	set       *driver.Set
	srv       *node.Server
	listen    string
	advertise string
	password  string
	relays    []string
	cfg       desktopConfig
	boot      bootArgs
	headless  bool
	relayOK   bool
	relayErr  string
}

type bootArgs struct {
	Relay     string
	Password  string
	Advertise string
	Headless  bool
}

type desktopConfig struct {
	Listen    string          `json:"listen"`
	Advertise string          `json:"advertise"`
	Relays    []string        `json:"relays"`
	Password  string          `json:"password,omitempty"`
	Enabled   map[string]bool `json:"enabled"`
}

// Status is desktop status.
type Status struct {
	NodeID    string   `json:"node_id"`
	Name      string   `json:"name"`
	Listen    string   `json:"listen"`
	Advertise string   `json:"advertise"`
	OpenAI    string   `json:"openai"`
	WS        string   `json:"ws"`
	Relays    []string `json:"relays"`
	Password  string   `json:"password,omitempty"`
	Engines   []string `json:"engines"`
	Linked    bool     `json:"linked"`
	LinkError string   `json:"link_error,omitempty"`
}

// NewApp creates the app.
func NewApp() *App {
	return &App{log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
}

func (a *App) runHeadless() {
	a.headless = true
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	a.startup(ctx)
	a.log.Info("headless node started", "listen", a.listen, "advertise", a.advertise, "relay", strings.Join(a.relays, ","))
	<-ctx.Done()
	a.shutdown(context.Background())
}

func (a *App) startup(ctx context.Context) {
	a.headless = a.headless || a.boot.Headless
	a.ctx, a.cancel = context.WithCancel(ctx)
	cfg := loadConfig()
	a.cfg = cfg
	ident, err := e2e.LoadOrCreate(filepath.Join(os.Getenv("HOME"), ".agent-mesh", "node.key"))
	if err != nil {
		a.log.Error("identity", "err", err)
		return
	}
	a.ident = ident
	set := factory.Default()
	for name, on := range cfg.Enabled {
		_ = set.SetEnabled(name, on)
	}
	a.set = set
	listen := cfg.Listen
	if listen == "" {
		listen = "0.0.0.0:7700"
	}
	if !portFree(listen) {
		listen = freeListen("0.0.0.0")
	}
	a.listen = listen
	a.relays = cfg.Relays
	a.password = cfg.Password
	a.advertise = cfg.Advertise
	if a.boot.Relay != "" {
		a.relays = []string{strings.TrimRight(strings.TrimSpace(a.boot.Relay), "/")}
	}
	if a.boot.Password != "" {
		a.password = a.boot.Password
	}
	if a.boot.Advertise != "" {
		a.advertise = a.boot.Advertise
	}
	if a.advertise == "" {
		a.advertise = node.GuessAdvertise(listen)
	}
	a.cfg.Relays = a.relays
	a.cfg.Password = a.password
	a.cfg.Advertise = a.advertise
	saveConfig(a.cfg)
	srv := node.New(ident, listen, hostname(), set, a.log)
	a.srv = srv
	go func() {
		if err := srv.ListenAndServe(a.ctx); err != nil {
			a.log.Error("node serve", "err", err)
		}
	}()
	a.startRegister()
	if len(a.relays) > 0 {
		go a.linkOnce()
	}
	go a.forwardChunks()
}

func (a *App) shutdown(ctx context.Context) {
	if a.cancel != nil {
		a.cancel()
	}
}

func (a *App) forwardChunks() {
	for {
		if a.srv == nil || a.srv.Hub == nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		break
	}
	id, ch := a.srv.Hub.Subscribe(128)
	defer a.srv.Hub.Unsubscribe(id)
	for {
		select {
		case <-a.ctx.Done():
			return
		case chunk, ok := <-ch:
			if !ok {
				return
			}
			_ = chunk
		}
	}
}

// NodeStatus is local node info.
func (a *App) NodeStatus() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := Status{
		Listen:    a.listen,
		Advertise: a.advertise,
		Relays:    a.relays,
		Password:  a.password,
		OpenAI:    "http://" + a.listen + "/v1",
		WS:        "ws://" + a.listen + "/v1/openai/ws",
		Linked:    a.relayOK,
		LinkError: a.relayErr,
	}
	if a.ident != nil {
		st.NodeID = a.ident.ID
	}
	if a.srv != nil {
		info := a.srv.Info()
		st.Name = info.Name
		st.Engines = info.Engines
	}
	return st
}

// Engines lists local engines.
func (a *App) Engines() []protocol.EngineInfo {
	if a.set == nil {
		return nil
	}
	return a.set.Engines(a.ctx)
}

// SetEngineEnabled toggles an engine.
func (a *App) SetEngineEnabled(name string, enabled bool) error {
	if a.set == nil {
		return nil
	}
	if err := a.set.SetEnabled(name, enabled); err != nil {
		return err
	}
	a.mu.Lock()
	if a.cfg.Enabled == nil {
		a.cfg.Enabled = map[string]bool{}
	}
	a.cfg.Enabled[name] = enabled
	cfg := a.cfg
	a.mu.Unlock()
	saveConfig(cfg)
	return nil
}

// Models lists models for one engine or every engine.
func (a *App) Models(engine string) ([]protocol.Model, error) {
	if a.set == nil {
		return nil, nil
	}
	return a.set.Models(a.ctx, engine)
}

// Sessions lists sessions.
func (a *App) Sessions(engine string, all bool) ([]protocol.Session, error) {
	if a.set == nil {
		return nil, nil
	}
	rows, err := a.set.ListAll(a.ctx, engine, all)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].NodeID = a.ident.ID
		rows[i].NodeName = hostname()
	}
	return rows, nil
}

// History is session history.
func (a *App) History(engine, sessionID string, tail int) ([]protocol.Event, error) {
	d, err := a.set.Get(engine)
	if err != nil {
		return nil, err
	}
	if tail <= 0 {
		tail = 80
	}
	return d.History(a.ctx, sessionID, tail)
}

// Reply appends to an existing session.
func (a *App) Reply(engine, sessionID, text string) error {
	d, err := a.set.Get(engine)
	if err != nil {
		return err
	}
	evs, err := d.Reply(a.ctx, sessionID, text, "")
	if err != nil {
		return err
	}
	go func() {
		for range a.srv.Hub.Pump(engine, sessionID, engine, evs) {
		}
	}()
	return nil
}

// Send starts a new turn.
func (a *App) Send(engine, cwd, text string) error {
	d, err := a.set.Get(engine)
	if err != nil {
		return err
	}
	evs, err := d.Send(a.ctx, cwd, text, "", "")
	if err != nil {
		return err
	}
	go func() {
		for range a.srv.Hub.Pump(engine, "", engine, evs) {
		}
	}()
	return nil
}

// Directories lists local workspaces.
func (a *App) Directories() []protocol.WorkDir {
	if a.set == nil {
		return nil
	}
	return a.set.Directories(a.ctx)
}

// SetMesh sets relay, password, and public URL.
func (a *App) SetMesh(relay, password, advertise string) error {
	var relays []string
	for _, p := range strings.Split(relay, ",") {
		p = strings.TrimRight(strings.TrimSpace(p), "/")
		if p != "" {
			relays = append(relays, p)
		}
	}
	advertise = strings.TrimRight(strings.TrimSpace(advertise), "/")
	a.mu.Lock()
	a.relays = relays
	a.password = password
	if advertise != "" {
		a.advertise = advertise
	} else {
		a.advertise = node.GuessAdvertise(a.listen)
	}
	a.cfg.Relays = relays
	a.cfg.Password = password
	a.cfg.Advertise = a.advertise
	cfg := a.cfg
	a.mu.Unlock()
	saveConfig(cfg)
	a.startRegister()
	return a.linkOnce()
}

// SetRelay joins using one relay URL.
func (a *App) SetRelay(raw string) error {
	a.mu.Lock()
	pw := a.password
	adv := a.advertise
	a.mu.Unlock()
	return a.SetMesh(raw, pw, adv)
}

func (a *App) linkOnce() error {
	a.mu.Lock()
	srv := a.srv
	relays := append([]string(nil), a.relays...)
	advertise := a.advertise
	password := a.password
	ctx := a.ctx
	a.mu.Unlock()
	if srv == nil {
		return nil
	}
	if len(relays) == 0 {
		a.mu.Lock()
		a.relayOK = false
		a.relayErr = "relay URL is required"
		a.mu.Unlock()
		return fmt.Errorf("relay URL is required")
	}
	wait := context.Background()
	if ctx != nil {
		wait = ctx
	}
	err := srv.RegisterOnce(wait, relays, advertise, password)
	a.mu.Lock()
	if err != nil {
		a.relayOK = false
		a.relayErr = err.Error()
	} else {
		a.relayOK = true
		a.relayErr = ""
	}
	a.mu.Unlock()
	return err
}

func (a *App) startRegister() {
	a.mu.Lock()
	if a.regCancel != nil {
		a.regCancel()
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.regCancel = cancel
	srv := a.srv
	relays := append([]string(nil), a.relays...)
	advertise := a.advertise
	password := a.password
	a.mu.Unlock()
	if srv != nil {
		go srv.RegisterLoop(ctx, relays, advertise, password)
	}
}

// Relays lists current relays.
func (a *App) Relays() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.relays...)
}

func hostname() string {
	h, _ := os.Hostname()
	if h == "" {
		return "node"
	}
	return h
}

func configPath() string {
	return filepath.Join(os.Getenv("HOME"), ".agent-mesh", "desktop.json")
}

func loadConfig() desktopConfig {
	raw, err := os.ReadFile(configPath())
	if err != nil {
		return desktopConfig{Listen: "0.0.0.0:7700", Enabled: map[string]bool{}}
	}
	var cfg desktopConfig
	_ = json.Unmarshal(raw, &cfg)
	if cfg.Listen == "" {
		cfg.Listen = "0.0.0.0:7700"
	}
	if cfg.Enabled == nil {
		cfg.Enabled = map[string]bool{}
	}
	return cfg
}

func saveConfig(cfg desktopConfig) {
	_ = os.MkdirAll(filepath.Dir(configPath()), 0o700)
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	_ = os.WriteFile(configPath(), raw, 0o600)
}

func portFree(addr string) bool {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func freeListen(host string) string {
	ln, err := net.Listen("tcp", host+":0")
	if err != nil {
		return host + ":7700"
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}
