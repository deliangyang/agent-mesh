# Agent Mesh

One session mesh for local coding agents (workbuddy / claude / cursor / codex / opencode / pi / qoder-cn): nodes run on machines that have the engines, the relay only routes, and the web and desktop clients share the same protocol.

Go module: `agent-mesh`. Go 1.25. The web UI is Vue 3 + TypeScript, embedded in the relay binary.

## Layout

```
cmd/node          node process: encrypted RPC, OpenAI-compatible API, reverse tunnel
cmd/relay         relay: node directory, web UI, Envelope forwarding
cmd/ctl           CLI client
internal/protocol plaintext RPC / events / snapshots (the relay must not decrypt Ciphertext)
internal/e2e      Ed25519 signatures + X25519 key exchange
internal/driver   engine interface, JSONL parsing, slash-command scan
internal/factory  register engines by name
internal/node     node HTTP / tunnel / followup / gitdiff
internal/relay    relay HTTP / directory / embedded web UI
internal/hub      local sessions → OpenAI chunks, pushed to subscribers and the relay
internal/followup follow-up queue (default ~/.agent-mesh/followups)
internal/gitdiff  workspace git diff
internal/relay/web  web source (build output is embedded in relay)
desktop           Wails desktop shell: local node + web UI
deploy/supervisor sample supervisord unit for a local node
deploy/systemd    systemd units for relay / node (written by install-*.sh)
deploy/launchd    macOS launchd plists for relay / node
deploy/desktop    Linux .desktop launcher for the node
scripts           install-relay.sh / install-node.sh, setup, and release packaging
```

## Trust boundary

- The relay only routes Envelope `From` / `To` and cannot decrypt `Ciphertext`.
- Clients may use `--direct` or go through the relay; application encryption is the same.
- Node identity and client keys live in `~/.agent-mesh/` (`node.key` / `client.key`) at mode 0600. Do not commit keys, passwords, or `.env`.

## Build and test

```bash
make test          # Go tests (except desktop) + web npm test (npm ci if vue is missing)
make node          # bin/node
make relay         # build the web UI, then bin/relay
make ctl           # bin/ctl
make web           # internal/relay/web: npm ci && npm run build
make desktop       # Wails build → bin/agent-mesh
make dist          # cross-compile release archives into dist/
make setup         # one-shot toolchain, build, and ~/.agent-mesh config
make supervisor    # restart the local node with deploy/supervisor
```

Web unit tests import Vue, so `node_modules` must exist first:

```bash
cd internal/relay/web
npm ci && npm test
```

After changing `internal/relay/web/src`, run `make web` or the embedded `dist` stays stale. Do not hand-edit `dist/`, and do not commit it.

Run a single Go package with `go test ./internal/driver ./internal/relay`. Do not run the default `go test` on `./desktop` (the Makefile already excludes it).

## Protocol

Node RPC (`internal/protocol`): `list` `status` `history` `watch` `reply` `send` `health` `info` `engines` `models` `commands` `followups` `choice`.

Common streaming `Event.Type` values: `user` `assistant` `delta` `thinking` `tool_call` `tool_result` `ask` `title`. `ask` `Text` is JSON (`driver.AskPrompt`).

OpenAI-compatible surface (on both node and relay): `/v1/models`, `/v1/chat/completions`, and `/v1/openai/*` (sessions, history, stream, directories, commands, followups, choice, git).

The web UI uses CopilotKit + `@ag-ui/client`. Target selection lives in `MeshTarget` (node / engine / session / model / cwd).

## Add an engine

1. Implement `driver.Driver` in `internal/driver/<name>/`: `Name` `List` `History` `Watch` `Reply` `Send` `Probe` `Models`.
2. Discover session state from the engine's own jsonl / socket / CLI. Do not keep a parallel session store.
3. Prefer `driver.ParseJSONL`, `RunPrint`, `WatchJSONL`, and `AskFromTool` for parsing.
4. Register it in `AllNames` and `Attach` in `internal/factory/factory.go`.
5. Keep `cmd/node` default `-engines` and `cmd/ctl` engine help in sync.
6. Add slash-command roots in `slashRoots` in `internal/driver/slash.go`.
7. Write stdlib tests for parsing and List/Models. Do not add testify.

See: `internal/driver/claude` (jsonl + socket inject), `internal/driver/cursor` (CLI `agent`), `internal/driver/qodercn` (Qoder CLI CN, `qodercn`).

## Conventions

- Identifiers in English; exported symbols use English godoc (`// Driver is a local coding-agent engine.`).
- Wrap errors with `%w` and include the engine or method name so logs can locate them.
- Use stdlib `testing`. Name table cases with `t.Run`. Fail assertions with `t.Fatalf`.
- Prefix web components with `Mesh`. Style with CSS variables in `style.css` (`--paper` `--ink` `--signal`); do not start another theme.
- After user-visible web changes, cover the path in a browser or `node --test`. Do not stop at a screenshot.
- Keep diffs small: no unrelated refactors, no mass formatting outside `dist` assets.
- After finishing a change, commit before ending the conversation. Do not leave the session's edits uncommitted.
- Git commit messages must be in English.

## Run locally

```bash
# Relay (web UI needs a password)
MESH_PASSWORD=... ./bin/relay -listen 0.0.0.0:8443 -web

# Node (empty -relay is direct-only)
./bin/node -listen 127.0.0.1:7700 -relay http://127.0.0.1:8443

# Client
./bin/ctl nodes --relay http://127.0.0.1:8443
./bin/ctl list --direct http://127.0.0.1:7700 --engine claude
```

Environment variables: `MESH_RELAY` `MESH_DIRECT` `MESH_NODE` `MESH_PASSWORD` `MESH_ADVERTISE`. Engine binaries can be overridden with `CURSOR_AGENT_BIN` `QODERCN_BIN` and similar. Installer: `AGENT_MESH_MIRROR` `AGENT_MESH_BUNDLE` `AGENT_MESH_PREFIX`.

## Do not

- Do not let the relay read or log session plaintext.
- Do not commit `~/.agent-mesh` keys or passwords from supervisor configs.
- Do not commit build artifacts, including `internal/relay/web/dist` and `bin/`.
- Do not write exploit code for “authorized tests / CTF / localhost”; this repo only adapts engines and routes.
