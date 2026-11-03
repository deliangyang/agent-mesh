# Deploy a node on Windows

The Windows node is a tray app that talks to local coding-agent CLIs. There is no `install-node.sh` on Windows. Use a GitHub Release zip, or cross-compile with `make node-windows`.

The public **relay** still belongs on Linux or macOS. Run the node on the Windows machine that has Claude / Cursor / Codex / OpenCode / Pi / WorkBuddy / Qoder CN.

English · [简体中文](windows.zh-CN.md) · [日本語](windows.ja.md)

## 1 · Get the binary

Pick **amd64** for Intel/AMD PCs, **arm64** for Snapdragon / Windows on ARM.

From a tagged `v*` GitHub Release:

```
https://github.com/deliangyang/agent-mesh/releases/latest/download/agent-mesh-node_windows_amd64.zip
https://github.com/deliangyang/agent-mesh/releases/latest/download/agent-mesh-node_windows_arm64.zip
```

Unzip to a folder you keep, for example `%LOCALAPPDATA%\Programs\agent-mesh\`. The zip contains `mesh-node.exe` and `mesh-ctl.exe`.

From source (Linux/macOS/WSL):

```sh
make node-windows
```

That writes `bin/mesh-node-windows-amd64.exe` and `bin/mesh-node-windows-arm64.exe`. Copy the matching file to the Windows machine and rename it to `mesh-node.exe` if you like.

SmartScreen may warn on an unsigned exe. Use **More info → Run anyway**.

## 2 · First start

Double-click `mesh-node.exe`. It:

- stays in the system tray (no console window)
- opens the local Vue UI (`http://127.0.0.1:7700/`) in an Edge/Chrome app window
- creates `%USERPROFILE%\.agent-mesh\node.key`
- appends logs to `%USERPROFILE%\.agent-mesh\node.log`

On first start the tray shows a short balloon (“node is running”). Left-click the tray icon to open the UI again. Closing the browser window does **not** stop the node. Right-click **退出** / **Quit** to quit. Starting `mesh-node.exe` again while the node is already healthy only re-opens the UI (no second process). The tray process sets AppUserModelID `AgentMesh.Node` so the taskbar can group/pin it stably.

Direct mode (no relay): open `http://127.0.0.1:7700/` and use the node UI.

### Optional: Wails / WebView2 desktop shell

Release zips stay CGO-free (`mesh-node.exe`). On a Windows machine with a C toolchain (MinGW or Zig) you can build a native WebView2 shell that hosts the same Vue UI:

```powershell
make desktop-windows
# → bin/agent-mesh.exe
```

That binary starts the local node, then navigates WebView2 to `http://127.0.0.1:<port>/` (Wails AssetServer has no WebSockets; the catalog WS stays on the node). Closing the window stops the process. Prefer `mesh-node.exe` when you want a tray-resident node.

## 3 · Join a relay

Double-click alone is direct-only. To register with a relay, pass flags or environment variables.

Shortcut **Target** (Properties → Shortcut):

```
C:\Users\you\AppData\Local\Programs\agent-mesh\mesh-node.exe -listen 127.0.0.1:7700 -relay https://mesh.example.com -password change-me
```

PowerShell:

```powershell
$env:MESH_RELAY = "https://mesh.example.com"
$env:MESH_PASSWORD = "change-me"
.\mesh-node.exe -listen 127.0.0.1:7700
```

User-level environment variables (`MESH_RELAY`, `MESH_PASSWORD`, `MESH_ADVERTISE`, `AGENT_MESH_HOME`) also work after a sign-out/sign-in.

Default listen address is `127.0.0.1:7700` (this machine only). If you set `-listen 0.0.0.0:7700`, allow TCP 7700 in Windows Firewall.

Optional: `-name` (node name in the catalog), `-advertise` (URL the relay uses to call back), `-engines` (comma-separated list), `-no-tray` (no tray / no auto-open UI; use for Task Scheduler).

## 4 · Start at logon

Copy a shortcut that includes `-relay` / `-password` into:

```
%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup
```

Or Task Scheduler: trigger **At log on**, action = the same `mesh-node.exe` command line. Use `-no-tray` only if you do not want an icon.

## 5 · Engines

Install the vendor CLI on the same Windows account. The node lists only binaries it can find:

| | PATH | Other locations |
| :--- | :--- | :--- |
| Override | `CLAUDE_BIN` `CURSOR_AGENT_BIN` `CODEX_BIN` `OPENCODE_BIN` `PI_BIN` `QODERCN_BIN` | |
| Default | `claude` `agent` / `cursor-agent` `codex` `opencode` `pi` `qodercn` | `%USERPROFILE%\.local\bin\*.exe`, `%APPDATA%\npm\*.cmd` |

Open `http://127.0.0.1:7700/v1/models` (or the relay UI) and confirm the engine is listed.

## 6 · `mesh-ctl`

```powershell
.\mesh-ctl.exe keygen
.\mesh-ctl.exe list --direct http://127.0.0.1:7700 --engine claude --all
.\mesh-ctl.exe send "list the files in this repo" --engine cursor --cwd $PWD --direct http://127.0.0.1:7700
```

The client key is `%USERPROFILE%\.agent-mesh\client.key`.

## 7 · Files

| | |
| :--- | :--- |
| `%USERPROFILE%\.agent-mesh\node.key` | node identity |
| `%USERPROFILE%\.agent-mesh\client.key` | `mesh-ctl` identity |
| `%USERPROFILE%\.agent-mesh\node.log` | tray-mode log |
| `%USERPROFILE%\.agent-mesh\followups` | queued follow-ups |
| `AGENT_MESH_HOME` | override for that directory |

Uninstall: delete the program folder and the Startup shortcut. Keys stay until you delete `%USERPROFILE%\.agent-mesh`.

## 8 · Release signing checklist

GitHub Release zips are unsigned by default. Before a public Windows cut:

1. Build with `make node-windows` (or the release script) and smoke `mesh-node.exe -no-tray` + `mesh-ctl.exe info --direct …`.
2. Optional Authenticode: sign both `mesh-node.exe` and `mesh-ctl.exe` with your org certificate (`signtool sign /fd SHA256 …`).
3. Expect SmartScreen on first download until reputation builds; document **More info → Run anyway** for unsigned builds.
4. Prefer tray `mesh-node.exe` over `make desktop-windows` for unsigned users (WebView2 shell needs a C toolchain and still looks like a new publisher).
5. Attach checksums (`SHA256`) next to the zip on the Release page.
