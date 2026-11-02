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
	relay := fs.String("relay", env("MESH_RELAY", ""), "relay URL")
	direct := fs.String("direct", env("MESH_DIRECT", ""), "direct node URL; skips the relay when set")
	nodeID := fs.String("node", env("MESH_NODE", ""), "target node ID")
	key := fs.String("key", defaultClientKey(), "client identity key")
	engine := fs.String("engine", "", "engine: workbuddy/claude/cursor/codex/opencode/pi/qoder-cn")
	all := fs.Bool("all", false, "include history")
	tail := fs.Int("tail", 80, "history line count")
	cwd := fs.String("cwd", "", "working directory for send")
	action := fs.String("action", "list", "followups action: list/push/update/delete")
	itemID := fs.String("id", "", "followups item ID")
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
			fatal("history needs a session id")
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
			fatal("watch needs a session id")
		}
		stream(ctx, cli, protocol.Request{
			ID:     "1",
			Method: protocol.MethodWatch,
			Stream: true,
			Params: protocol.MustJSON(protocol.WatchParams{Engine: needEngine(*engine), SessionID: args[0]}),
		})
	case "reply":
		if len(args) < 2 {
			fatal("reply needs a session id and text")
		}
		stream(ctx, cli, protocol.Request{
			ID:     "1",
			Method: protocol.MethodReply,
			Stream: true,
			Params: protocol.MustJSON(protocol.ReplyParams{Engine: needEngine(*engine), SessionID: args[0], Text: strings.Join(args[1:], " ")}),
		})
	case "send":
		if len(args) < 1 {
			fatal("send needs text")
		}
		stream(ctx, cli, protocol.Request{
			ID:     "1",
			Method: protocol.MethodSend,
			Stream: true,
			Params: protocol.MustJSON(protocol.SendParams{Engine: needEngine(*engine), CWD: *cwd, Text: strings.Join(args, " ")}),
		})
	case "commands":
		resp, err := cli.Call(ctx, protocol.Request{
			ID:     "1",
			Method: protocol.MethodCommands,
			Params: protocol.MustJSON(protocol.CommandsParams{Engine: needEngine(*engine), CWD: *cwd}),
		})
		must(err)
		fmt.Println(string(resp.Body))
	case "followups":
		p := protocol.FollowupsParams{
			Action: *action,
			Engine: needEngine(*engine),
			CWD:    *cwd,
			ID:     *itemID,
		}
		if len(args) > 0 {
			p.Session = args[0]
		}
		if *action == "push" || *action == "add" || *action == "update" {
			if len(args) < 2 {
				fatal("followups " + *action + " needs a session and text")
			}
			p.Text = strings.Join(args[1:], " ")
		}
		if p.Session == "" {
			fatal("followups needs a session")
		}
		resp, err := cli.Call(ctx, protocol.Request{
			ID:     "1",
			Method: protocol.MethodFollowups,
			Params: protocol.MustJSON(p),
		})
		must(err)
		fmt.Println(string(resp.Body))
	case "choice":
		if len(args) < 2 {
			fatal("choice needs a session and text")
		}
		resp, err := cli.Call(ctx, protocol.Request{
			ID:     "1",
			Method: protocol.MethodChoice,
			Params: protocol.MustJSON(protocol.ChoiceParams{
				Engine:  needEngine(*engine),
				Session: args[0],
				CWD:     *cwd,
				Text:    strings.Join(args[1:], " "),
			}),
		})
		must(err)
		fmt.Println(string(resp.Body))
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
		fatal("pass --engine with workbuddy/claude/cursor/codex/opencode/pi")
	}
	return e
}

func defaultClientKey() string {
	if env := strings.TrimSpace(os.Getenv("AGENT_MESH_HOME")); env != "" {
		return filepath.Join(env, "client.key")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agent-mesh", "client.key")
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
	fmt.Fprintln(os.Stderr, "error:", msg)
	os.Exit(1)
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage:
  ctl keygen
  ctl nodes --relay http://127.0.0.1:8443
  ctl list|status|engines|models --relay URL [--node ID] [--engine E] [--all]
  ctl history SESSION --engine E
  ctl watch SESSION --engine E
  ctl reply SESSION TEXT --engine E
  ctl send TEXT --engine E [--cwd DIR]
  ctl commands --engine E [--cwd DIR]
  ctl followups SESSION --engine E [--action list|push|update|delete] [--id ID] [TEXT]
  ctl choice SESSION TEXT --engine E [--cwd DIR]
  ctl ... --direct http://127.0.0.1:7700   # skip the relay and talk to the node directly
`)
}
