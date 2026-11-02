# Deploy a node on macOS

macOS uses the same `install-node.sh` flow as Linux: it installs `mesh-node` / `mesh-ctl`, writes a launchd plist (`RunAtLoad` + `KeepAlive`), and on a graphical session also creates `Agent Mesh.app` under `~/Applications`.

English · [简体中文](macos.zh-CN.md)

The public **relay** can run on the same Mac or on a Linux host. Run the **node** on the Mac that has Claude / Cursor / Codex / OpenCode / Pi / WorkBuddy / Qoder CN.

## 1 · Install

From a tagged release (Apple Silicon = `darwin_arm64`, Intel = `darwin_amd64`):

```sh
curl -fsSL https://raw.githubusercontent.com/deliangyang/agent-mesh/main/scripts/install-node.sh | bash
```

Or from a local clone:

```sh
./scripts/install-node.sh
```

Useful flags:

| Flag | Effect |
| :--- | :--- |
| `--desktop` / `--headless` | force GUI app vs service-only |
| `--no-service` | skip launchd |
| `--prefix DIR` | install under `DIR` (default `~/.local`) |

Environment: `MESH_RELAY` `MESH_PASSWORD` `MESH_ADVERTISE` `AGENT_MESH_HOME` `AGENT_MESH_PREFIX`.

## 2 · First start

- GUI install opens `Agent Mesh.app` (Chrome/Edge app window against the local node).
- Headless / SSH: launchd starts `mesh-node` without a window.
- Identity: `~/.agent-mesh/node.key`
- Logs: `launchctl` / `~/.agent-mesh/node.log` when configured

Direct mode: open `http://127.0.0.1:7700/`.

## 3 · Join a relay

```sh
export MESH_RELAY=https://mesh.example.com
export MESH_PASSWORD=change-me
# reinstall or edit ~/.agent-mesh/node.env, then:
launchctl kickstart -k "gui/$(id -u)/com.agentmesh.node"
```

Or pass flags when starting manually:

```sh
mesh-node -listen 127.0.0.1:7700 -relay https://mesh.example.com -password change-me
```

## 4 · Engines

Install vendor CLIs on the same macOS account. Overrides: `CLAUDE_BIN` `CURSOR_AGENT_BIN` `CODEX_BIN` `OPENCODE_BIN` `PI_BIN` `QODERCN_BIN`. The node also looks in `PATH`, `~/.local/bin`, and `~/.npm-global/bin`.

Confirm with `http://127.0.0.1:7700/v1/models` or the relay UI. Catalog nodes advertise `os` / `arch` (e.g. `darwin` / `arm64`).

## 5 · `mesh-ctl`

```sh
mesh-ctl keygen
mesh-ctl list --direct http://127.0.0.1:7700 --engine claude --all
mesh-ctl send "list the files in this repo" --engine cursor --cwd "$PWD" --direct http://127.0.0.1:7700
```

Client key: `~/.agent-mesh/client.key`.

## 6 · Files

| | |
| :--- | :--- |
| `~/.agent-mesh/node.key` | node identity |
| `~/.agent-mesh/client.key` | `mesh-ctl` identity |
| `~/.agent-mesh/node.env` | relay / listen env for launchd |
| `~/Library/LaunchAgents/com.agentmesh.node.plist` | boot unit |
| `~/Applications/Agent Mesh.app` | desktop launcher (GUI installs) |
| `AGENT_MESH_HOME` | override for the config directory |

Uninstall: remove the LaunchAgent, the app bundle, and binaries under the install prefix. Keys remain until you delete `~/.agent-mesh`.

See also: [Windows](windows.md) for the tray zip on PCs.
