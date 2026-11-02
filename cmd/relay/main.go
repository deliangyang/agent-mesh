package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"agent-mesh/internal/relay"
)

func main() {
	listen := flag.String("listen", "0.0.0.0:8443", "relay listen address")
	web := flag.Bool("web", false, "serve the web client")
	password := flag.String("password", os.Getenv("MESH_PASSWORD"), "shared password for the web UI and client API")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if *web && *password == "" {
		log.Error("-web requires -password or MESH_PASSWORD")
		os.Exit(2)
	}
	srv := relay.New(*listen, relay.NewMemory(), log)
	srv.Web = *web
	srv.Password = *password
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := srv.ListenAndServe(ctx); err != nil {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}
