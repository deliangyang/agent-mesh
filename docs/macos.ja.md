# macOS でノードをデプロイする

macOS は Linux と同じ `install-node.sh` を使います。`mesh-node` / `mesh-ctl` を入れ、launchd plist（`RunAtLoad` + `KeepAlive`）を書き、GUI セッションでは `~/Applications` に `Agent Mesh.app` も作ります。

[English](macos.md) · [简体中文](macos.zh-CN.md) · 日本語

公開 **リレー** は同じ Mac でも Linux ホストでも構いません。**ノード** は Claude / Cursor / Codex / OpenCode / Pi / WorkBuddy / Qoder CN がある Mac で動かします。

## 1 · インストール

タグ付きリリースから（Apple Silicon = `darwin_arm64`、Intel = `darwin_amd64`）:

```sh
curl -fsSL https://raw.githubusercontent.com/deliangyang/agent-mesh/main/scripts/install-node.sh | bash
```

ローカル clone から:

```sh
./scripts/install-node.sh
```

便利なフラグ:

| フラグ | 効果 |
| :--- | :--- |
| `--desktop` / `--headless` | GUI アプリ強制 / サービスのみ |
| `--no-service` | launchd をスキップ |
| `--prefix DIR` | `DIR` 配下にインストール（既定 `~/.local`） |

環境変数: `MESH_RELAY` `MESH_PASSWORD` `MESH_ADVERTISE` `AGENT_MESH_HOME` `AGENT_MESH_PREFIX`。

## 2 · 初回起動

- GUI インストールは `Agent Mesh.app` を開く（ローカルノード向け Chrome/Edge アプリウィンドウ）。
- ヘッドレス / SSH: launchd がウィンドウなしで `mesh-node` を起動。
- 識別子: `~/.agent-mesh/node.key`
- ログ: `launchctl` / 設定時は `~/.agent-mesh/node.log`

直接モード: `http://127.0.0.1:7700/`。

## 3 · リレーに参加

```sh
export MESH_RELAY=https://mesh.example.com
export MESH_PASSWORD=change-me
# 再インストールするか ~/.agent-mesh/node.env を編集してから:
launchctl kickstart -k "gui/$(id -u)/com.agentmesh.node"
```

手動起動時はフラグでも可:

```sh
mesh-node -listen 127.0.0.1:7700 -relay https://mesh.example.com -password change-me
```

## 4 · エンジン

同じ macOS アカウントにベンダー CLI を入れます。上書き: `CLAUDE_BIN` `CURSOR_AGENT_BIN` `CODEX_BIN` `OPENCODE_BIN` `PI_BIN` `QODERCN_BIN`。ノードは `PATH`、`~/.local/bin`、`~/.npm-global/bin` も見ます。

`http://127.0.0.1:7700/v1/models` またはリレー UI で確認。カタログのノードは `os` / `arch`（例: `darwin` / `arm64`）を広告します。

## 5 · `mesh-ctl`

```sh
mesh-ctl keygen
mesh-ctl list --direct http://127.0.0.1:7700 --engine claude --all
mesh-ctl send "list the files in this repo" --engine cursor --cwd "$PWD" --direct http://127.0.0.1:7700
```

クライアント鍵: `~/.agent-mesh/client.key`。

## 6 · ファイル

| | |
| :--- | :--- |
| `~/.agent-mesh/node.key` | ノード識別子 |
| `~/.agent-mesh/client.key` | `mesh-ctl` 識別子 |
| `~/.agent-mesh/node.env` | launchd 用リレー / listen 環境 |
| `~/Library/LaunchAgents/com.agentmesh.node.plist` | 起動ユニット |
| `~/Applications/Agent Mesh.app` | デスクトップランチャ（GUI インストール） |
| `AGENT_MESH_HOME` | 設定ディレクトリの上書き |

アンインストール: LaunchAgent、アプリバンドル、インストール prefix 下のバイナリを削除。鍵は `~/.agent-mesh` を消すまで残ります。

## 7 · Release 署名チェックリスト

`darwin_*` アーカイブは公証しない限り未署名です。公開 macOS カット前に:

1. `darwin/amd64` と `darwin/arm64` 向けにクロス/CI ビルドし、テスト Mac で `install-node.sh --no-service` をスモーク。
2. 任意: Developer ID Application で codesign し、`notarytool submit` + staple。
3. 未署名ダウンロードは Gatekeeper がブロック。右クリック → **開く**、または生バイナリより install スクリプトを推奨。
4. launchd plist はユーザーレベル（`~/Library/LaunchAgents`）。ノードに root は不要。
5. Release ページに各 tarball の `SHA256` を掲載。

StatusNotifier ホストがある Linux デスクトップでは、`DISPLAY` または `WAYLAND_DISPLAY` があるとき `mesh-node` が pure-Go D-Bus でトレイ（開く / 終了）も登録します。アイコンは埋め込み Agent Mesh pixmap。ウォッチャが無い場合は macOS と同様に一度 UI を開きます。

PC 向けトレイ zip は [Windows](windows.ja.md) を参照。
