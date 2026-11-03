# 在 Windows 上部署节点

Windows 上的 node 是托盘程序，负责驱动本机的 coding agent 命令行。没有 `install-node.sh`。用 GitHub Release 的 zip，或在别的机器上 `make node-windows` 交叉编译。

对外的 **中继** 仍建议放在 Linux / macOS。Windows 机器只跑节点，并安装 Claude / Cursor / Codex / OpenCode / Pi / WorkBuddy / Qoder CN。

[English](windows.md) · 简体中文

## 1 · 拿到二进制

普通电脑用 **amd64**，骁龙 / ARM 版 Windows 用 **arm64**。

打过 `v*` 标签后，从 GitHub Release 下载：

```
https://github.com/deliangyang/agent-mesh/releases/latest/download/agent-mesh-node_windows_amd64.zip
https://github.com/deliangyang/agent-mesh/releases/latest/download/agent-mesh-node_windows_arm64.zip
```

解压到长期保留的目录，例如 `%LOCALAPPDATA%\Programs\agent-mesh\`。包里是 `mesh-node.exe` 和 `mesh-ctl.exe`。

从源码交叉编译（Linux / macOS / WSL）：

```sh
make node-windows
```

会生成 `bin/mesh-node-windows-amd64.exe` 和 `bin/mesh-node-windows-arm64.exe`。拷到 Windows 后可改名为 `mesh-node.exe`。

未签名的 exe 可能被 SmartScreen 拦截，选「更多信息 → 仍要运行」。

## 2 · 第一次启动

双击 `mesh-node.exe`：

- 进系统托盘（没有黑色控制台窗口）
- 用 Edge/Chrome 的 app 窗口打开本机 Vue 界面 `http://127.0.0.1:7700/`
- 生成 `%USERPROFILE%\.agent-mesh\node.key`
- 日志追加到 `%USERPROFILE%\.agent-mesh\node.log`

首次启动时托盘会弹出短气泡提示（节点已在运行）。左键托盘图标会再打开界面。关掉浏览器窗口 **不会** 结束节点。右键「退出」才退出。若节点已在运行，再次启动 `mesh-node.exe` 只会重新打开界面（不会起第二个进程）。托盘进程会设置 AppUserModelID `AgentMesh.Node`，便于任务栏固定/归组。

不连中继（直连）：浏览器打开 `http://127.0.0.1:7700/` 即可。

### 可选：Wails / WebView2 桌面壳

Release 里的 `mesh-node.exe` 仍是无 CGO 的托盘版。在本机装好 C 工具链（MinGW 或 Zig）后，可以编一个原生 WebView2 壳，界面仍是同一套 Vue：

```powershell
make desktop-windows
# → bin/agent-mesh.exe
```

它会先起本地 node，再把 WebView2 导航到 `http://127.0.0.1:<port>/`（Wails AssetServer 不支持 WebSocket，目录推送仍走 node）。关掉窗口即退出进程。需要托盘常驻时继续用 `mesh-node.exe`。

## 3 · 挂到中继

只双击是直连模式。要向中继注册，用参数或环境变量。

快捷方式「目标」（属性 → 快捷方式）：

```
C:\Users\你\AppData\Local\Programs\agent-mesh\mesh-node.exe -listen 127.0.0.1:7700 -relay https://mesh.example.com -password change-me
```

PowerShell：

```powershell
$env:MESH_RELAY = "https://mesh.example.com"
$env:MESH_PASSWORD = "change-me"
.\mesh-node.exe -listen 127.0.0.1:7700
```

用户级环境变量（`MESH_RELAY`、`MESH_PASSWORD`、`MESH_ADVERTISE`、`AGENT_MESH_HOME`）改完后重新登录才会进新开的进程。

默认监听 `127.0.0.1:7700`，只本机可访问。若写成 `-listen 0.0.0.0:7700`，需要在防火墙放行 TCP 7700。

可选参数：`-name`（目录里显示的节点名）、`-advertise`（中继回连地址）、`-engines`（逗号分隔的引擎列表）、`-no-tray`（不要托盘、不自动开界面，适合计划任务）。

## 4 · 开机自启

把带 `-relay` / `-password` 的快捷方式放到：

```
%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup
```

或用任务计划程序：触发器选「登录时」，操作填同一条 `mesh-node.exe` 命令行。不需要托盘时再加 `-no-tray`。

## 5 · 引擎

在同一 Windows 账户下安装厂商 CLI。节点只列出它找得到的二进制：

| | PATH | 其它位置 |
| :--- | :--- | :--- |
| 覆盖 | `CLAUDE_BIN` `CURSOR_AGENT_BIN` `CODEX_BIN` `OPENCODE_BIN` `PI_BIN` `QODERCN_BIN` | |
| 默认名 | `claude` `agent` / `cursor-agent` `codex` `opencode` `pi` `qodercn` | `%USERPROFILE%\.local\bin\*.exe`、`%APPDATA%\npm\*.cmd` |

打开 `http://127.0.0.1:7700/v1/models`（或中继网页）确认引擎已出现。

## 6 · `mesh-ctl`

```powershell
.\mesh-ctl.exe keygen
.\mesh-ctl.exe list --direct http://127.0.0.1:7700 --engine claude --all
.\mesh-ctl.exe send "list the files in this repo" --engine cursor --cwd $PWD --direct http://127.0.0.1:7700
```

客户端密钥在 `%USERPROFILE%\.agent-mesh\client.key`。

## 7 · 文件

| | |
| :--- | :--- |
| `%USERPROFILE%\.agent-mesh\node.key` | 节点身份 |
| `%USERPROFILE%\.agent-mesh\client.key` | `mesh-ctl` 身份 |
| `%USERPROFILE%\.agent-mesh\node.log` | 托盘模式日志 |
| `%USERPROFILE%\.agent-mesh\followups` | 追加消息队列 |
| `AGENT_MESH_HOME` | 覆盖上述目录 |

卸载：删掉程序目录和开机快捷方式。密钥会保留，除非你再删 `%USERPROFILE%\.agent-mesh`。

## 8 · 发布签名清单

GitHub Release 的 zip 默认未签名。公开 Windows 版本前：

1. 用 `make node-windows`（或发布脚本）构建，并冒烟 `mesh-node.exe -no-tray` 与 `mesh-ctl.exe info --direct …`。
2. 可选 Authenticode：用组织证书签名 `mesh-node.exe` 与 `mesh-ctl.exe`（`signtool sign /fd SHA256 …`）。
3. 未建立信誉前，SmartScreen 可能拦截首次下载；未签名构建请写明 **更多信息 → 仍要运行**。
4. 未签名用户优先托盘版 `mesh-node.exe`，而不是 `make desktop-windows`（WebView2 壳需要 C 工具链，且仍像新发布者）。
5. 在 Release 页为 zip 附上 `SHA256` 校验和。
