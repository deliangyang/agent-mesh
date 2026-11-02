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
	ctx    context.Context
	cancel context.CancelFunc
	log    *slog.Logger

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
	ident, err := e2e.LoadOrCreate(filepath.Join(driver.MeshHome(), "node.key"))
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
		listen = "127.0.0.1:7700"
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
	savedMesh := node.LoadMeshFile()
	if len(a.relays) == 0 {
		a.relays = savedMesh.Relays
	}
	if a.password == "" {
		a.password = savedMesh.Password
	}
	if a.advertise == "" {
		a.advertise = savedMesh.Advertise
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
	srv.StartMesh(a.ctx, a.relays, a.advertise, a.password)
	if len(a.relays) > 0 {
		go func() { _ = srv.ProbeMesh(a.ctx, a.relays, a.advertise, a.password) }()
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
	listen := a.listen
	ident := a.ident
	srv := a.srv
	a.mu.Unlock()
	st := Status{
		Listen: listen,
		OpenAI: "http://" + listen + "/v1",
		WS:     "ws://" + listen + "/v1/openai/ws",
	}
	if ident != nil {
		st.NodeID = ident.ID
	}
	if srv != nil {
		info := srv.Info()
		st.Name = info.Name
		st.Engines = info.Engines
		mesh := srv.MeshStatus()
		st.Advertise = mesh.Advertise
		st.Relays = mesh.Relays
		st.Password = mesh.Password
		st.Linked = mesh.Linked
		st.LinkError = mesh.LinkError
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
	if a.srv == nil {
		return fmt.Errorf("node is not running")
	}
	err := a.srv.ConfigureMesh(a.ctx, []string{relay}, advertise, password)
	mesh := a.srv.MeshStatus()
	a.mu.Lock()
	a.relays = mesh.Relays
	a.password = mesh.Password
	a.advertise = mesh.Advertise
	a.relayOK = mesh.Linked
	a.relayErr = mesh.LinkError
	a.cfg.Relays = mesh.Relays
	a.cfg.Password = mesh.Password
	a.cfg.Advertise = mesh.Advertise
	cfg := a.cfg
	a.mu.Unlock()
	saveConfig(cfg)
	return err
}

// SetRelay joins using one relay URL.
func (a *App) SetRelay(raw string) error {
	mesh := node.MeshStatus{}
	if a.srv != nil {
		mesh = a.srv.MeshStatus()
	}
	return a.SetMesh(raw, mesh.Password, mesh.Advertise)
}

// Relays lists current relays.
func (a *App) Relays() []string {
	if a.srv != nil {
		return a.srv.MeshStatus().Relays
	}
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
	return filepath.Join(driver.MeshHome(), "desktop.json")
}

func loadConfig() desktopConfig {
	raw, err := os.ReadFile(configPath())
	if err != nil {
		return desktopConfig{Listen: "127.0.0.1:7700", Enabled: map[string]bool{}}
	}
	var cfg desktopConfig
	_ = json.Unmarshal(raw, &cfg)
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:7700"
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
