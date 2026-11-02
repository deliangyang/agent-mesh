# 在 macOS 上部署节点

macOS 与 Linux 共用 `install-node.sh`：安装 `mesh-node` / `mesh-ctl`、写入 launchd plist（`RunAtLoad` + `KeepAlive`），图形会话下还会在 `~/Applications` 生成 `Agent Mesh.app`。

[English](macos.md) · 简体中文

公共 **relay** 可跑在同一台 Mac 或 Linux 上；**node** 应跑在已安装 Claude / Cursor / Codex / OpenCode / Pi / WorkBuddy / Qoder CN 的那台 Mac。

## 1 · 安装

按发布架构选择（Apple Silicon = `darwin_arm64`，Intel = `darwin_amd64`）：

```sh
curl -fsSL https://raw.githubusercontent.com/deliangyang/agent-mesh/main/scripts/install-node.sh | bash
```

本地克隆：

```sh
./scripts/install-node.sh
```

常用参数：

| 参数 | 作用 |
| :--- | :--- |
| `--desktop` / `--headless` | 强制桌面入口 / 仅服务 |
| `--no-service` | 不写 launchd |
| `--prefix DIR` | 安装前缀（默认 `~/.local`） |

环境变量：`MESH_RELAY` `MESH_PASSWORD` `MESH_ADVERTISE` `AGENT_MESH_HOME` `AGENT_MESH_PREFIX`。

## 2 · 首次启动

- 图形安装会打开 `Agent Mesh.app`（Chrome/Edge 应用窗口指向本机 node）
- 无图形 / SSH：launchd 后台拉起 `mesh-node`
- 身份：`~/.agent-mesh/node.key`

直连：打开 `http://127.0.0.1:7700/`。

## 3 · 接入中继

```sh
export MESH_RELAY=https://mesh.example.com
export MESH_PASSWORD=change-me
# 重新安装或编辑 ~/.agent-mesh/node.env 后：
launchctl kickstart -k "gui/$(id -u)/com.agentmesh.node"
```

或手动启动：

```sh
mesh-node -listen 127.0.0.1:7700 -relay https://mesh.example.com -password change-me
```

## 4 · 引擎

在同一 macOS 账号下安装厂商 CLI。可用 `CLAUDE_BIN` `CURSOR_AGENT_BIN` 等覆盖。目录里的节点会带上 `os` / `arch`（如 `darwin` / `arm64`）。

## 5 · `mesh-ctl`

```sh
mesh-ctl keygen
mesh-ctl list --direct http://127.0.0.1:7700 --engine claude --all
```

客户端密钥：`~/.agent-mesh/client.key`。

## 6 · 文件

| | |
| :--- | :--- |
| `~/.agent-mesh/node.key` | 节点身份 |
| `~/.agent-mesh/node.env` | launchd 使用的环境 |
| `~/Library/LaunchAgents/com.agentmesh.node.plist` | 开机单元 |
| `~/Applications/Agent Mesh.app` | 桌面入口 |

卸载：移除 LaunchAgent、App 与安装前缀下的二进制；删除 `~/.agent-mesh` 才会清密钥。

Windows 托盘 zip 见 [Windows](windows.zh-CN.md)。
