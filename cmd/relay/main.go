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
	listen := flag.String("listen", "0.0.0.0:8443", "中继监听地址")
	web := flag.Bool("web", false, "启动网页客户端")
	password := flag.String("password", os.Getenv("MESH_PASSWORD"), "约定密码，网页与客户端接口共用")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if *web && *password == "" {
		log.Error("启用 -web 时必须提供 -password 或 MESH_PASSWORD")
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
