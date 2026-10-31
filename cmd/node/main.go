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
	listen := flag.String("listen", "127.0.0.1:7700", "节点监听地址")
	advertise := flag.String("advertise", "", "中继回连本节点的 URL，默认 http://listen")
	relays := flag.String("relay", "", "中继地址，逗号分隔，可空（仅直连）")
	key := flag.String("key", defaultKey(), "身份密钥文件")
	name := flag.String("name", hostname(), "节点名称")
	engines := flag.String("engines", "workbuddy,claude,cursor,codex", "启用的引擎")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
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
	go srv.RegisterLoop(ctx, relayList, *advertise, "")
	if err := srv.ListenAndServe(ctx); err != nil {
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
	return filepath.Join(os.Getenv("HOME"), ".agent-mesh", "node.key")
}
