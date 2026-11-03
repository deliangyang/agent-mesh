# Windows でノードをデプロイする

Windows のノードはトレイアプリで、ローカルの coding-agent CLI を駆動します。Windows に `install-node.sh` はありません。GitHub Release の zip を使うか、`make node-windows` でクロスコンパイルします。

公開 **リレー** は Linux / macOS 上に置きます。Claude / Cursor / Codex / OpenCode / Pi / WorkBuddy / Qoder CN がある Windows マシンでノードを実行してください。

[English](windows.md) · [简体中文](windows.zh-CN.md) · 日本語

## 1 · バイナリを入手

Intel/AMD PC は **amd64**、Snapdragon / Windows on ARM は **arm64**。

タグ付き `v*` GitHub Release から:

```
https://github.com/deliangyang/agent-mesh/releases/latest/download/agent-mesh-node_windows_amd64.zip
https://github.com/deliangyang/agent-mesh/releases/latest/download/agent-mesh-node_windows_arm64.zip
```

例: `%LOCALAPPDATA%\Programs\agent-mesh\` に展開。zip には `mesh-node.exe` と `mesh-ctl.exe` があります。

ソースから（Linux/macOS/WSL）:

```sh
make node-windows
```

`bin/mesh-node-windows-amd64.exe` と `bin/mesh-node-windows-arm64.exe` ができます。Windows マシンへコピーし、必要なら `mesh-node.exe` にリネームします。

未署名 exe では SmartScreen が出ることがあります。**詳細情報 → 実行** を使ってください。

## 2 · 初回起動

`mesh-node.exe` をダブルクリックすると:

- システムトレイに常駐（コンソールなし）
- ローカル Vue UI（`http://127.0.0.1:7700/`）を Edge/Chrome アプリウィンドウで開く
- `%USERPROFILE%\.agent-mesh\node.key` を作成
- ログを `%USERPROFILE%\.agent-mesh\node.log` に追記

初回起動時、トレイに短いバルーン（ノード起動）が出ます。トレイアイコンを左クリックで UI を再度開けます。ブラウザを閉じてもノードは止まりません。右クリックの **終了** / **Quit** で終了。健全なノードが既に動いているときに再起動すると UI だけ開き直します。トレイは AppUserModelID `AgentMesh.Node` を設定します。

直接モード（リレーなし）: `http://127.0.0.1:7700/` を開きます。

### 任意: Wails / WebView2 デスクトップシェル

Release zip は CGO 不要（`mesh-node.exe`）。C ツールチェーンがある Windows では、同じ Vue UI を載せるネイティブ WebView2 シェルをビルドできます:

```powershell
make desktop-windows
# → bin/agent-mesh.exe
```

このバイナリはローカルノードを起動し、WebView2 で `http://127.0.0.1:<port>/` を開きます。ウィンドウを閉じるとプロセスが終了します。トレイ常駐が欲しければ `mesh-node.exe` を使ってください。

## 3 · リレーに参加

ダブルクリックのみでは直接モードです。リレー登録にはフラグか環境変数を使います。

ショートカットの **リンク先**:

```
C:\Users\you\AppData\Local\Programs\agent-mesh\mesh-node.exe -listen 127.0.0.1:7700 -relay https://mesh.example.com -password change-me
```

PowerShell:

```powershell
$env:MESH_RELAY = "https://mesh.example.com"
$env:MESH_PASSWORD = "change-me"
.\mesh-node.exe -listen 127.0.0.1:7700
```

ユーザー環境変数（`MESH_RELAY`、`MESH_PASSWORD`、`MESH_ADVERTISE`、`AGENT_MESH_HOME`）もサインアウト/サインイン後に有効です。

既定の listen は `127.0.0.1:7700`。`-listen 0.0.0.0:7700` にする場合はファイアウォールで TCP 7700 を許可してください。

任意: `-name`、`-advertise`、`-engines`、`-no-tray`（タスクスケジューラ向け）。

## 4 · ログオン時に起動

`-relay` / `-password` 付きショートカットを次へコピー:

```
%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup
```

またはタスクスケジューラで **ログオン時** に同じコマンドラインを実行。アイコン不要なら `-no-tray`。

## 5 · エンジン

同じ Windows アカウントにベンダー CLI を入れます。見つかったバイナリだけが一覧されます:

| | PATH | その他 |
| :--- | :--- | :--- |
| 上書き | `CLAUDE_BIN` `CURSOR_AGENT_BIN` `CODEX_BIN` `OPENCODE_BIN` `PI_BIN` `QODERCN_BIN` | |
| 既定 | `claude` `agent` / `cursor-agent` `codex` `opencode` `pi` `qodercn` | `%USERPROFILE%\.local\bin\*.exe`、`%APPDATA%\npm\*.cmd` |

`http://127.0.0.1:7700/v1/models`（またはリレー UI）で確認します。

## 6 · `mesh-ctl`

```powershell
.\mesh-ctl.exe keygen
.\mesh-ctl.exe list --direct http://127.0.0.1:7700 --engine claude --all
.\mesh-ctl.exe send "list the files in this repo" --engine cursor --cwd $PWD --direct http://127.0.0.1:7700
```

クライアント鍵は `%USERPROFILE%\.agent-mesh\client.key`。

## 7 · ファイル

| | |
| :--- | :--- |
| `%USERPROFILE%\.agent-mesh\node.key` | ノード識別子 |
| `%USERPROFILE%\.agent-mesh\client.key` | `mesh-ctl` 識別子 |
| `%USERPROFILE%\.agent-mesh\node.log` | トレイモードのログ |
| `%USERPROFILE%\.agent-mesh\followups` | フォローアップキュー |
| `AGENT_MESH_HOME` | そのディレクトリの上書き |

アンインストール: プログラムフォルダとスタートアップショートカットを削除。鍵は `%USERPROFILE%\.agent-mesh` を消すまで残ります。

## 8 · Release 署名チェックリスト

GitHub Release zip は既定で未署名です。公開 Windows カット前に:

1. `make node-windows`（またはリリーススクリプト）でビルドし、`mesh-node.exe -no-tray` と `mesh-ctl.exe info --direct …` をスモーク。
2. 任意 Authenticode: `mesh-node.exe` と `mesh-ctl.exe` を組織証明書で署名（`signtool sign /fd SHA256 …`）。
3. 評判が付くまで初回ダウンロードで SmartScreen が出る想定。未署名は **詳細情報 → 実行** を案内。
4. 未署名ユーザーには `make desktop-windows` よりトレイの `mesh-node.exe` を推奨。
5. Release ページに zip と一緒に `SHA256` チェックサムを添付。
