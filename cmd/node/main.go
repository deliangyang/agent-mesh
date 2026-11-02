package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/e2e"
	"agent-mesh/internal/factory"
	"agent-mesh/internal/node"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:7700", "node listen address")
	advertise := flag.String("advertise", "", "URL the relay uses to call back; LAN address is probed by default")
	relays := flag.String("relay", "", "relay URLs, comma-separated; empty for direct-only")
	password := flag.String("password", os.Getenv("MESH_PASSWORD"), "relay password")
	key := flag.String("key", defaultKey(), "identity key file")
	name := flag.String("name", hostname(), "node name")
	engines := flag.String("engines", "workbuddy,claude,cursor,codex,opencode,pi,qoder-cn", "enabled engines")
	noTray := flag.Bool("no-tray", false, "Windows: do not show a tray icon")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if !*noTray {
		log = withGUILog(log)
	}
	ident, err := e2e.LoadOrCreate(*key)
	if err != nil {
		log.Error("load identity", "err", err)
		os.Exit(1)
	}
	set := driver.NewSet()
	factory.Attach(set, strings.Split(*engines, ","))
	srv := node.New(ident, *listen, *name, set, log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var relayList []string
	for _, r := range strings.Split(*relays, ",") {
		r = strings.TrimRight(strings.TrimSpace(r), "/")
		if r != "" {
			relayList = append(relayList, r)
		}
	}
	adv := strings.TrimSpace(*advertise)
	if adv == "" {
		adv = node.GuessAdvertise(*listen)
	}
	go srv.RegisterLoop(ctx, relayList, adv, *password)
	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe(ctx)
		errCh <- err
		if err != nil {
			stop()
		}
	}()
	if !*noTray {
		_ = runWindowsTray(ctx, stop, nodeUIURL(*listen))
	}
	if err := <-errCh; err != nil {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}

func hostname() string {
	h, _ := os.Hostname()
	if h == "" {
		return "node"
	}
	return h
}

func defaultKey() string {
	return filepath.Join(driver.MeshHome(), "node.key")
}
