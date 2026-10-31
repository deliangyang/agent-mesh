package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agent-mesh/internal/client"
	"agent-mesh/internal/e2e"
	"agent-mesh/internal/protocol"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	relay := fs.String("relay", env("MESH_RELAY", ""), "中继 URL")
	direct := fs.String("direct", env("MESH_DIRECT", ""), "直连节点 URL，设置后不走中继")
	nodeID := fs.String("node", env("MESH_NODE", ""), "目标节点 ID")
	key := fs.String("key", filepath.Join(os.Getenv("HOME"), ".agent-mesh", "client.key"), "客户端身份密钥")
	engine := fs.String("engine", "", "引擎：workbuddy/claude/cursor/codex")
	all := fs.Bool("all", false, "包含历史会话")
	tail := fs.Int("tail", 80, "history 条数")
	cwd := fs.String("cwd", "", "send 工作目录")
	_ = fs.Parse(os.Args[2:])
	args := fs.Args()

	if cmd == "keygen" {
		id, err := e2e.LoadOrCreate(*key)
		must(err)
		fmt.Println("id", id.ID)
		fmt.Println("key", *key)
		return
	}

	ident, err := e2e.LoadOrCreate(*key)
	must(err)
	cli := client.New(ident, *relay, *direct)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if cmd == "nodes" {
		rows, err := cli.Nodes(ctx)
		must(err)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		must(enc.Encode(rows))
		return
	}

	must(cli.Handshake(ctx, *nodeID))
	switch cmd {
	case "engines":
		resp, err := cli.Call(ctx, protocol.Request{ID: "1", Method: protocol.MethodEngines})
		must(err)
		fmt.Println(string(resp.Body))
	case "models":
		resp, err := cli.Call(ctx, protocol.Request{
			ID:     "1",
			Method: protocol.MethodModels,
			Params: protocol.MustJSON(protocol.ModelsParams{Engine: *engine}),
		})
		must(err)
		fmt.Println(string(resp.Body))
	case "list", "status":
		req := protocol.Request{ID: "1", Method: protocol.MethodList, Params: protocol.MustJSON(protocol.ListParams{Engine: *engine, All: *all})}
		if cmd == "status" {
			req.Method = protocol.MethodStatus
		}
		resp, err := cli.Call(ctx, req)
		must(err)
		fmt.Println(string(resp.Body))
	case "history":
		if len(args) < 1 {
			fatal("history 需要 session id")
		}
		resp, err := cli.Call(ctx, protocol.Request{
			ID:     "1",
			Method: protocol.MethodHistory,
			Params: protocol.MustJSON(protocol.HistoryParams{Engine: needEngine(*engine), SessionID: args[0], Tail: *tail}),
		})
		must(err)
		fmt.Println(string(resp.Body))
	case "watch":
		if len(args) < 1 {
			fatal("watch 需要 session id")
		}
		stream(ctx, cli, protocol.Request{
			ID:     "1",
			Method: protocol.MethodWatch,
			Stream: true,
			Params: protocol.MustJSON(protocol.WatchParams{Engine: needEngine(*engine), SessionID: args[0]}),
		})
	case "reply":
		if len(args) < 2 {
			fatal("reply 需要 session id 和文本")
		}
		stream(ctx, cli, protocol.Request{
			ID:     "1",
			Method: protocol.MethodReply,
			Stream: true,
			Params: protocol.MustJSON(protocol.ReplyParams{Engine: needEngine(*engine), SessionID: args[0], Text: strings.Join(args[1:], " ")}),
		})
	case "send":
		if len(args) < 1 {
			fatal("send 需要文本")
		}
		stream(ctx, cli, protocol.Request{
			ID:     "1",
			Method: protocol.MethodSend,
			Stream: true,
			Params: protocol.MustJSON(protocol.SendParams{Engine: needEngine(*engine), CWD: *cwd, Text: strings.Join(args, " ")}),
		})
	default:
		usage()
		os.Exit(2)
	}
}

func stream(ctx context.Context, cli *client.Client, req protocol.Request) {
	ch, err := cli.Stream(ctx, req)
	must(err)
	for ev := range ch {
		if ev.Text != "" {
			fmt.Println(ev.Text)
		}
		if ev.Done {
			return
		}
	}
}

func needEngine(e string) string {
	if e == "" {
		fatal("请用 --engine 指定 workbuddy/claude/cursor/codex")
	}
	return e
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func must(err error) {
	if err != nil {
		fatal(err.Error())
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "错误:", msg)
	os.Exit(1)
}

func usage() {
	fmt.Fprintf(os.Stderr, `用法:
  ctl keygen
  ctl nodes --relay http://127.0.0.1:8443
  ctl list|status|engines|models --relay URL [--node ID] [--engine E] [--all]
  ctl history SESSION --engine E
  ctl watch SESSION --engine E
  ctl reply SESSION TEXT --engine E
  ctl send TEXT --engine E [--cwd DIR]
  ctl ... --direct http://127.0.0.1:7700   # 不经过中继，直连节点
`)
}
