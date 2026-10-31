#!/usr/bin/env python3
"""接管本机 Claude Code：列出进行中的任务，追加对话，并实时跟踪进度。

Claude Code 把会话写在 ~/.claude/projects/<cwd 编码>/<session>.jsonl，
正在跑的交互进程会在 ~/.claude/sessions/<pid>.json 登记 Unix 套接字。

本工具：

- `claude agents --json` 列出本机交互/后台会话
- 向 live 会话的 Unix 套接字注入用户消息（auth + user）
- 对未占用的会话走 `claude -p --resume` + stream-json
- 跟踪 jsonl / stream-json，实时打印对话进度

Usage:
    python3 claude_ctl.py list
    python3 claude_ctl.py list --all
    python3 claude_ctl.py status
    python3 claude_ctl.py watch SESSION_ID
    python3 claude_ctl.py reply SESSION_ID "请继续，补上测试"
    python3 claude_ctl.py send "帮我看看这个目录" --cwd /path/to/project
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shutil
import socket
import subprocess
import sys
import time
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional, Tuple


EXIT_SUCCESS = 0
EXIT_FAILURE = 1
EXIT_INVALID_ARGS = 2
EXIT_NOT_FOUND = 3

JSONL_ACTIVE_SEC = 20.0
USER_QUERY_RE = re.compile(r"<user_query>(.*?)</user_query>", re.S)
COMMAND_NAME_RE = re.compile(r"<command-name>(.*?)</command-name>", re.S)


class ClaudeError(RuntimeError):
    pass


# ---------------------------------------------------------------------------
# 路径与发现
# ---------------------------------------------------------------------------


def claude_dir() -> Path:
    env = os.environ.get("CLAUDE_CONFIG_DIR", "").strip()
    if env:
        return Path(os.path.expanduser(env))
    return Path.home() / ".claude"


def find_claude_bin() -> str:
    env = os.environ.get("CLAUDE_BIN", "").strip()
    if env:
        return env
    found = shutil.which("claude")
    if found:
        return found
    fallback = Path.home() / ".local" / "bin" / "claude"
    if fallback.is_file():
        return str(fallback)
    raise ClaudeError("找不到 claude 可执行文件，请把它加入 PATH 或设置 CLAUDE_BIN。")


def pid_alive(pid: int) -> bool:
    if pid <= 0:
        return False
    try:
        os.kill(pid, 0)
        return True
    except OSError:
        return Path(f"/proc/{pid}").exists()


def ts_to_epoch(value: Any) -> float:
    if value is None or value == "":
        return 0.0
    if isinstance(value, (int, float)):
        n = float(value)
        if n > 1e12:
            return n / 1000.0
        if n > 1e10:
            return n / 1000.0
        return n
    text = str(value).strip()
    if not text:
        return 0.0
    try:
        if text.endswith("Z"):
            text = text[:-1] + "+00:00"
        return datetime.fromisoformat(text).timestamp()
    except ValueError:
        try:
            return ts_to_epoch(float(text))
        except ValueError:
            return 0.0


def fmt_time(value: Any) -> str:
    epoch = ts_to_epoch(value)
    if epoch <= 0:
        return "-"
    return datetime.fromtimestamp(epoch).strftime("%Y-%m-%d %H:%M:%S")


def encode_project_dir(cwd: str) -> str:
    return cwd.replace("/", "-") if cwd.startswith("/") else cwd.replace("/", "-")


def find_jsonl(root: Path, session_id: str, cwd: str = "") -> Optional[Path]:
    projects = root / "projects"
    if cwd:
        candidate = projects / encode_project_dir(cwd) / f"{session_id}.jsonl"
        if candidate.is_file():
            return candidate
    matches = list(projects.glob(f"**/{session_id}.jsonl")) if projects.is_dir() else []
    if matches:
        return max(matches, key=lambda p: p.stat().st_mtime)
    return None


def content_to_text(content: Any) -> str:
    if content is None:
        return ""
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts: List[str] = []
        for item in content:
            if isinstance(item, str):
                parts.append(item)
                continue
            if not isinstance(item, dict):
                continue
            kind = item.get("type")
            if kind in {"text", "input_text", "output_text"}:
                parts.append(str(item.get("text") or ""))
            elif kind == "thinking" and item.get("thinking"):
                parts.append(str(item.get("thinking")))
            elif item.get("text"):
                parts.append(str(item["text"]))
        return "".join(parts)
    if isinstance(content, dict):
        return str(content.get("text") or "")
    return str(content)


def extract_user_text(text: str) -> str:
    if not text:
        return ""
    match = USER_QUERY_RE.search(text)
    if match:
        return match.group(1).strip()
    cmd = COMMAND_NAME_RE.search(text)
    if cmd:
        return cmd.group(1).strip()
    if text.startswith("<local-command-caveat>"):
        return ""
    if "system-reminder" in text and "<user_query>" not in text:
        return ""
    return text.strip()


def truncate(text: str, limit: int = 240) -> str:
    text = re.sub(r"\s+", " ", text).strip()
    if len(text) <= limit:
        return text
    return text[: limit - 1] + "…"


# ---------------------------------------------------------------------------
# 会话模型
# ---------------------------------------------------------------------------


@dataclass
class LiveSession:
    pid: int
    session_id: str
    cwd: str
    name: str = ""
    status: str = ""
    kind: str = "interactive"
    started_at: float = 0.0
    socket_path: str = ""
    peer_token: str = ""
    updated_at: float = 0.0


@dataclass
class SessionInfo:
    session_id: str
    title: str = ""
    cwd: str = ""
    status: str = ""
    name: str = ""
    model: str = ""
    updated_at: float = 0.0
    live: bool = False
    busy: bool = False
    pid: int = 0
    socket_path: str = ""
    peer_token: str = ""
    jsonl_path: str = ""
    kind: str = ""
    source: str = ""

    @property
    def running(self) -> bool:
        if self.busy:
            return True
        if self.jsonl_path:
            try:
                if time.time() - Path(self.jsonl_path).stat().st_mtime <= JSONL_ACTIVE_SEC:
                    return True
            except OSError:
                pass
        return False


def load_session_sidecar(root: Path, pid: int, socket_path: str) -> Tuple[str, str]:
    """返回 (socket_path, peer_token)。"""
    sessions_dir = root / "sessions"
    meta_path = sessions_dir / f"{pid}.json"
    sock = socket_path
    token = ""
    if meta_path.is_file():
        try:
            data = json.loads(meta_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            data = {}
        sock = sock or str(data.get("messagingSocketPath") or "")
    digest = ""
    if sock:
        digest = hashlib.sha256(sock.encode("utf-8")).hexdigest()
        key_path = sessions_dir / f"{pid}.{digest}.key"
        if key_path.is_file():
            try:
                key = json.loads(key_path.read_text(encoding="utf-8"))
                token = str(key.get("peerToken") or "")
            except (OSError, json.JSONDecodeError):
                token = ""
    if not token:
        for path in sessions_dir.glob(f"{pid}.*.key"):
            try:
                key = json.loads(path.read_text(encoding="utf-8"))
            except (OSError, json.JSONDecodeError):
                continue
            if key.get("peerToken"):
                token = str(key["peerToken"])
                break
    return sock, token


def claude_agents_json(claude_bin: str) -> List[Dict[str, Any]]:
    try:
        proc = subprocess.run(
            [claude_bin, "agents", "--json"],
            check=False,
            capture_output=True,
            text=True,
            timeout=15,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise ClaudeError(f"调用 claude agents 失败: {exc}") from exc
    if proc.returncode != 0:
        err = (proc.stderr or proc.stdout or "").strip()
        raise ClaudeError(err or f"claude agents 退出码 {proc.returncode}")
    text = proc.stdout.strip()
    if not text:
        return []
    try:
        data = json.loads(text)
    except json.JSONDecodeError as exc:
        raise ClaudeError(f"claude agents --json 不是合法 JSON: {exc}") from exc
    if isinstance(data, list):
        return data
    if isinstance(data, dict):
        for key in ("sessions", "agents", "data"):
            if isinstance(data.get(key), list):
                return data[key]
    return []


def discover_live(root: Path, claude_bin: str) -> List[LiveSession]:
    out: List[LiveSession] = []
    try:
        rows = claude_agents_json(claude_bin)
    except ClaudeError:
        rows = []
        sessions_dir = root / "sessions"
        if sessions_dir.is_dir():
            for path in sessions_dir.glob("*.json"):
                try:
                    data = json.loads(path.read_text(encoding="utf-8"))
                except (OSError, json.JSONDecodeError):
                    continue
                pid = int(data.get("pid") or 0)
                if pid and pid_alive(pid):
                    rows.append(data)
    for row in rows:
        pid = int(row.get("pid") or 0)
        session_id = str(row.get("sessionId") or "")
        if not session_id:
            continue
        sock, token = load_session_sidecar(root, pid, str(row.get("messagingSocketPath") or ""))
        out.append(
            LiveSession(
                pid=pid,
                session_id=session_id,
                cwd=str(row.get("cwd") or ""),
                name=str(row.get("name") or ""),
                status=str(row.get("status") or ""),
                kind=str(row.get("kind") or "interactive"),
                started_at=ts_to_epoch(row.get("startedAt")),
                socket_path=sock,
                peer_token=token,
                updated_at=ts_to_epoch(row.get("updatedAt") or row.get("statusUpdatedAt") or row.get("startedAt")),
            )
        )
    out.sort(key=lambda s: (s.status != "busy", -s.updated_at, -s.started_at))
    return out


def iter_jsonl_sessions(root: Path) -> Iterable[Path]:
    projects = root / "projects"
    if not projects.is_dir():
        return []
    for path in projects.glob("**/*.jsonl"):
        if path.parent.name == "subagents":
            continue
        yield path


def jsonl_title(path: Path) -> Tuple[str, str, float]:
    """从 jsonl 提取标题、cwd、最后活动时间。"""
    title = ""
    cwd = ""
    mtime = path.stat().st_mtime
    try:
        with path.open("r", encoding="utf-8", errors="replace") as handle:
            for raw in handle:
                raw = raw.strip()
                if not raw:
                    continue
                try:
                    obj = json.loads(raw)
                except json.JSONDecodeError:
                    continue
                if not isinstance(obj, dict):
                    continue
                cwd = cwd or str(obj.get("cwd") or "")
                kind = obj.get("type")
                if kind in {"ai-title", "custom-title"}:
                    title = str(obj.get("aiTitle") or obj.get("customTitle") or title)
                if kind == "user" and not obj.get("isMeta") and not title:
                    msg = obj.get("message") or {}
                    text = extract_user_text(content_to_text(msg.get("content")))
                    if text and not text.startswith("/"):
                        title = truncate(text, 60)
                ts = obj.get("timestamp")
                if ts:
                    mtime = max(mtime, ts_to_epoch(ts))
    except OSError:
        pass
    if not title:
        title = path.stem
    return title, cwd, mtime


def merge_sessions(root: Path, live: List[LiveSession]) -> List[SessionInfo]:
    by_id: Dict[str, SessionInfo] = {}
    for item in live:
        jsonl = find_jsonl(root, item.session_id, item.cwd)
        title = item.name
        model = ""
        if jsonl:
            t, cwd, mtime = jsonl_title(jsonl)
            title = t or title
            item.cwd = item.cwd or cwd
        info = SessionInfo(
            session_id=item.session_id,
            title=title,
            cwd=item.cwd,
            status=item.status or "idle",
            name=item.name,
            model=model,
            updated_at=item.updated_at or item.started_at,
            live=True,
            busy=item.status in {"busy", "running", "working"},
            pid=item.pid,
            socket_path=item.socket_path,
            peer_token=item.peer_token,
            jsonl_path=str(jsonl) if jsonl else "",
            kind=item.kind,
            source="live",
        )
        by_id[item.session_id] = info

    for path in iter_jsonl_sessions(root):
        sid = path.stem
        if sid in by_id:
            if not by_id[sid].jsonl_path:
                by_id[sid].jsonl_path = str(path)
            continue
        title, cwd, mtime = jsonl_title(path)
        by_id[sid] = SessionInfo(
            session_id=sid,
            title=title,
            cwd=cwd,
            status="completed",
            updated_at=mtime,
            jsonl_path=str(path),
            source="jsonl",
        )

    result = list(by_id.values())
    result.sort(key=lambda s: (not s.running, not s.live, -s.updated_at))
    return result


def find_session(sessions: List[SessionInfo], session_id: str) -> SessionInfo:
    exact = [s for s in sessions if s.session_id == session_id]
    if exact:
        return exact[0]
    prefix = [s for s in sessions if s.session_id.startswith(session_id)]
    if len(prefix) == 1:
        return prefix[0]
    names = [s for s in sessions if session_id in (s.name or "")]
    if len(names) == 1:
        return names[0]
    if len(prefix) > 1 or len(names) > 1:
        raise ClaudeError(f"会话 ID 不唯一: {session_id}")
    raise ClaudeError(f"未找到会话: {session_id}")


# ---------------------------------------------------------------------------
# 进度格式化
# ---------------------------------------------------------------------------


def format_jsonl_event(obj: Dict[str, Any], verbose: bool = False) -> Optional[str]:
    kind = obj.get("type")
    if kind in {"file-history-snapshot", "mode", "last-prompt", "queue-operation"}:
        return None
    if kind == "user":
        if obj.get("isMeta"):
            return None
        msg = obj.get("message") or {}
        content = msg.get("content")
        if isinstance(content, list) and content and isinstance(content[0], dict) and content[0].get("type") == "tool_result":
            text = content_to_text(content)
            return f"[工具结果] {truncate(text, 200)}"
        text = extract_user_text(content_to_text(content))
        if not text:
            return None
        return f"[用户] {truncate(text, 400 if verbose else 240)}"
    if kind == "assistant":
        msg = obj.get("message") or {}
        content = msg.get("content")
        if isinstance(content, list):
            lines: List[str] = []
            for item in content:
                if not isinstance(item, dict):
                    continue
                block = item.get("type")
                if block == "tool_use":
                    name = item.get("name") or "tool"
                    args = item.get("input") or {}
                    if isinstance(args, dict):
                        preview = args.get("command") or args.get("description") or json.dumps(args, ensure_ascii=False)
                    else:
                        preview = str(args)
                    lines.append(f"[工具调用] {name} {truncate(str(preview), 200)}")
                elif block == "thinking" and verbose:
                    think = item.get("thinking") or ""
                    if think:
                        lines.append(f"[思考] {truncate(str(think), 200)}")
                elif block == "text":
                    text = item.get("text") or ""
                    if text:
                        lines.append(f"[助手] {truncate(str(text), 400 if verbose else 240)}")
            return "\n".join(lines) if lines else None
        text = content_to_text(content)
        return f"[助手] {truncate(text, 240)}" if text else None
    if kind in {"ai-title", "custom-title"}:
        return f"[标题] {obj.get('aiTitle') or obj.get('customTitle') or ''}"
    if kind == "progress" and verbose:
        return f"[进度] {truncate(json.dumps(obj, ensure_ascii=False), 200)}"
    return None


def format_stream_event(obj: Dict[str, Any], verbose: bool = False) -> Optional[str]:
    kind = obj.get("type")
    if kind == "system" and obj.get("subtype") == "init":
        sid = obj.get("session_id") or obj.get("sessionId") or ""
        model = obj.get("model") or ""
        cwd = obj.get("cwd") or ""
        return f"[初始化] session={sid} model={model} cwd={cwd}".strip()
    if kind == "assistant":
        return format_jsonl_event(obj, verbose=verbose)
    if kind == "user":
        return format_jsonl_event(obj, verbose=verbose)
    if kind == "result":
        subtype = obj.get("subtype") or obj.get("result") or ""
        result = obj.get("result")
        if isinstance(result, str) and result.strip():
            return f"[结束] {subtype} {truncate(result, 300)}"
        return f"[结束] {subtype or 'done'}"
    if kind == "stream_event":
        event = obj.get("event") or {}
        delta = event.get("delta") or {}
        text = delta.get("text") or ""
        if text:
            return text
        if verbose:
            et = event.get("type") or ""
            if et:
                return f"[流] {et}"
        return None
    if kind == "error":
        return f"[错误] {obj.get('error') or obj.get('message') or obj}"
    return None


class JsonlWatcher:
    def __init__(self, path: Path) -> None:
        self.path = path
        self._offset = 0
        if path.is_file():
            self._offset = path.stat().st_size

    def seek_end(self) -> None:
        if self.path.is_file():
            self._offset = self.path.stat().st_size

    def seek_start(self) -> None:
        self._offset = 0

    def poll(self) -> List[Dict[str, Any]]:
        if not self.path.is_file():
            return []
        size = self.path.stat().st_size
        if size < self._offset:
            self._offset = 0
        if size == self._offset:
            return []
        events: List[Dict[str, Any]] = []
        with self.path.open("r", encoding="utf-8", errors="replace") as handle:
            handle.seek(self._offset)
            for line in handle:
                line = line.strip()
                if not line:
                    continue
                try:
                    obj = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if isinstance(obj, dict):
                    events.append(obj)
            self._offset = handle.tell()
        return events


# ---------------------------------------------------------------------------
# 发送：live 套接字 / claude -p
# ---------------------------------------------------------------------------


def inject_live_prompt(session: SessionInfo, text: str) -> None:
    if not session.socket_path:
        raise ClaudeError("该会话没有 messaging 套接字，无法注入。")
    if not session.peer_token:
        raise ClaudeError("找不到该会话的 peerToken（sessions/<pid>.*.key）。")
    if not Path(session.socket_path).exists():
        raise ClaudeError(f"套接字不存在: {session.socket_path}")
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    sock.settimeout(5)
    try:
        sock.connect(session.socket_path)
        lines = [
            {"type": "auth", "token": session.peer_token},
            {
                "type": "user",
                "message": {"role": "user", "content": text},
                "priority": "next",
            },
        ]
        for item in lines:
            sock.sendall((json.dumps(item, ensure_ascii=False) + "\n").encode("utf-8"))
        try:
            sock.shutdown(socket.SHUT_WR)
        except OSError:
            pass
    except OSError as exc:
        raise ClaudeError(f"写入 Claude 套接字失败: {exc}") from exc
    finally:
        sock.close()


def stream_print_process(proc: subprocess.Popen[str], verbose: bool = False) -> int:
    assert proc.stdout is not None
    try:
        for raw in proc.stdout:
            raw = raw.strip()
            if not raw:
                continue
            try:
                obj = json.loads(raw)
            except json.JSONDecodeError:
                print(raw, flush=True)
                continue
            if not isinstance(obj, dict):
                continue
            if obj.get("type") == "stream_event":
                event = obj.get("event") or {}
                delta = (event.get("delta") or {}).get("text") or ""
                if delta:
                    print(delta, end="", flush=True)
                    continue
                if verbose:
                    line = format_stream_event(obj, verbose=True)
                    if line:
                        print(line, flush=True)
                continue
            line = format_stream_event(obj, verbose=verbose)
            if line:
                print(line, flush=True)
    except KeyboardInterrupt:
        proc.terminate()
        print("\n已中断。")
    return proc.wait()


def build_print_cmd(
    claude_bin: str,
    prompt: str,
    *,
    resume: Optional[str] = None,
    session_id: Optional[str] = None,
    permission_mode: Optional[str] = None,
    skip_permissions: bool = False,
    extra: Optional[List[str]] = None,
) -> List[str]:
    cmd = [
        claude_bin,
        "-p",
        "--output-format",
        "stream-json",
        "--include-partial-messages",
        "--verbose",
    ]
    if resume:
        cmd.extend(["--resume", resume])
    if session_id:
        cmd.extend(["--session-id", session_id])
    if permission_mode:
        cmd.extend(["--permission-mode", permission_mode])
    if skip_permissions:
        cmd.append("--dangerously-skip-permissions")
    if extra:
        cmd.extend(extra)
    cmd.append(prompt)
    return cmd


# ---------------------------------------------------------------------------
# 命令
# ---------------------------------------------------------------------------


def print_json(data: Any) -> None:
    print(json.dumps(data, ensure_ascii=False, indent=2))


def session_to_dict(info: SessionInfo) -> Dict[str, Any]:
    return {
        "session_id": info.session_id,
        "title": info.title,
        "name": info.name,
        "cwd": info.cwd,
        "status": info.status,
        "live": info.live,
        "busy": info.busy,
        "running": info.running,
        "pid": info.pid,
        "kind": info.kind,
        "updated_at": fmt_time(info.updated_at),
        "jsonl_path": info.jsonl_path,
        "socket_path": info.socket_path,
    }


def cmd_list(args: argparse.Namespace) -> int:
    root = claude_dir()
    bin_path = find_claude_bin()
    live = discover_live(root, bin_path)
    sessions = merge_sessions(root, live)
    if not args.all:
        sessions = [s for s in sessions if s.live or s.running]
    if args.json:
        print_json(
            {
                "claude_bin": bin_path,
                "live": [session_to_dict(s) for s in sessions if s.live],
                "sessions": [session_to_dict(s) for s in sessions],
            }
        )
        return EXIT_SUCCESS

    print(f"Claude 目录: {root}")
    print(f"可执行文件: {bin_path}")
    print(f"在线会话: {len(live)}")
    for item in live:
        print(
            f"  [{item.status or 'idle':<6}] pid={item.pid} {item.session_id}  {item.name or '-'}  {item.cwd}"
        )
    print()
    if not sessions:
        print("当前没有正在进行的任务。用 --all 查看历史会话。")
        return EXIT_SUCCESS

    print(f"{'状态':<8} {'更新时间':<20} {'会话 ID':<38} 标题 / 目录")
    for info in sessions:
        if info.running:
            flag = "进行中"
        elif info.live:
            flag = "在线"
        else:
            flag = info.status or "历史"
        title = info.title or info.name or "(无标题)"
        print(f"{flag:<8} {fmt_time(info.updated_at):<20} {info.session_id:<38} {title}")
        extra = []
        if info.cwd:
            extra.append(info.cwd)
        if info.pid:
            extra.append(f"pid={info.pid}")
        if extra:
            print("         " + "  |  ".join(extra))
    return EXIT_SUCCESS


def cmd_status(args: argparse.Namespace) -> int:
    root = claude_dir()
    bin_path = find_claude_bin()
    live = discover_live(root, bin_path)
    if args.json:
        print_json([session_to_dict(s) for s in merge_sessions(root, live) if s.live])
        return EXIT_SUCCESS
    if not live:
        print("没有发现正在运行的 Claude Code 会话。")
        return EXIT_NOT_FOUND
    for item in live:
        sock_ok = Path(item.socket_path).exists() if item.socket_path else False
        print(f"pid={item.pid}  {item.session_id}")
        print(f"  名称: {item.name or '-'}  状态: {item.status or 'idle'}  类型: {item.kind}")
        print(f"  目录: {item.cwd or '-'}")
        print(f"  套接字: {item.socket_path or '-'}  ({'可用' if sock_ok else '缺失'})")
        print(f"  注入密钥: {'已找到' if item.peer_token else '缺失'}")
        print(f"  启动: {fmt_time(item.started_at)}")
    return EXIT_SUCCESS


def cmd_watch(args: argparse.Namespace) -> int:
    root = claude_dir()
    sessions = merge_sessions(root, discover_live(root, find_claude_bin()))
    info = find_session(sessions, args.session_id)
    path = Path(info.jsonl_path) if info.jsonl_path else find_jsonl(root, info.session_id, info.cwd)
    if path is None:
        print(f"会话 {info.session_id} 没有 jsonl 记录，无法跟踪进度。", file=sys.stderr)
        return EXIT_NOT_FOUND
    print(f"跟踪 {info.session_id}")
    print(f"文件  {path}")
    print(f"目录  {info.cwd or '-'}")
    print("---")
    watcher = JsonlWatcher(path)
    if args.from_start:
        watcher.seek_start()
    else:
        watcher.seek_end()
    try:
        while True:
            for obj in watcher.poll():
                line = format_jsonl_event(obj, verbose=args.verbose)
                if line:
                    print(line, flush=True)
            time.sleep(args.interval)
    except KeyboardInterrupt:
        print("\n已停止跟踪。")
        return EXIT_SUCCESS


def cmd_history(args: argparse.Namespace) -> int:
    root = claude_dir()
    sessions = merge_sessions(root, discover_live(root, find_claude_bin()))
    info = find_session(sessions, args.session_id)
    path = Path(info.jsonl_path) if info.jsonl_path else find_jsonl(root, info.session_id, info.cwd)
    if path is None or not path.is_file():
        print(f"未找到会话记录: {info.session_id}", file=sys.stderr)
        return EXIT_NOT_FOUND
    lines = path.read_text(encoding="utf-8", errors="replace").splitlines()
    keep = lines[-args.tail :] if args.tail > 0 else lines
    printed = 0
    for raw in keep:
        try:
            obj = json.loads(raw)
        except json.JSONDecodeError:
            continue
        if not isinstance(obj, dict):
            continue
        line = format_jsonl_event(obj, verbose=args.verbose)
        if line:
            print(line)
            printed += 1
    if printed == 0:
        print("没有可展示的对话内容。")
    return EXIT_SUCCESS


def resolve_text(args: argparse.Namespace) -> str:
    if args.text:
        return " ".join(args.text).strip()
    if not sys.stdin.isatty():
        return sys.stdin.read().strip()
    return ""


def cmd_reply(args: argparse.Namespace) -> int:
    text = resolve_text(args)
    if not text:
        print("请提供要追加的对话内容。", file=sys.stderr)
        return EXIT_INVALID_ARGS
    root = claude_dir()
    bin_path = find_claude_bin()
    sessions = merge_sessions(root, discover_live(root, bin_path))
    info = find_session(sessions, args.session_id)
    jsonl = Path(info.jsonl_path) if info.jsonl_path else find_jsonl(root, info.session_id, info.cwd)

    print(f"目标会话: {info.session_id}")
    print(f"目录: {info.cwd or '-'}")
    print(f"状态: {'在线 ' + (info.status or 'idle') if info.live else '离线'}")

    if info.live and info.socket_path and info.peer_token:
        inject_live_prompt(info, text)
        print("已通过 Unix 套接字注入到正在运行的会话。")
        if args.watch:
            if jsonl is None:
                print("没有 jsonl，无法跟踪进度。可以另开终端 watch。")
                return EXIT_SUCCESS
            print("--- 进度 ---")
            watcher = JsonlWatcher(jsonl)
            watcher.seek_end()
            idle_rounds = 0
            try:
                while True:
                    events = watcher.poll()
                    if events:
                        idle_rounds = 0
                        for obj in events:
                            line = format_jsonl_event(obj, verbose=args.verbose)
                            if line:
                                print(line, flush=True)
                    else:
                        idle_rounds += 1
                    time.sleep(args.interval)
                    if args.timeout > 0 and idle_rounds * args.interval >= args.timeout:
                        print("等待超时，停止跟踪（会话可能仍在跑）。")
                        break
            except KeyboardInterrupt:
                print("\n已停止跟踪。")
        return EXIT_SUCCESS

    print("会话不在线，改走 claude -p --resume。")
    cmd = build_print_cmd(
        bin_path,
        text,
        resume=info.session_id,
        permission_mode=args.permission_mode,
        skip_permissions=args.yes,
    )
    cwd = info.cwd or os.getcwd()
    print("命令:", " ".join(cmd[:8]), "...")
    proc = subprocess.Popen(
        cmd,
        cwd=cwd,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        bufsize=1,
    )
    code = stream_print_process(proc, verbose=args.verbose)
    return EXIT_SUCCESS if code == 0 else EXIT_FAILURE


def cmd_send(args: argparse.Namespace) -> int:
    text = resolve_text(args)
    if not text:
        print("请提供要发送的内容。", file=sys.stderr)
        return EXIT_INVALID_ARGS
    bin_path = find_claude_bin()
    cwd = args.cwd or os.getcwd()
    cmd = build_print_cmd(
        bin_path,
        text,
        resume=args.resume,
        session_id=args.session_id,
        permission_mode=args.permission_mode,
        skip_permissions=args.yes,
    )
    print(f"工作目录: {cwd}")
    print("已启动 claude -p，等待回复…")
    print("--- 进度 ---")
    proc = subprocess.Popen(
        cmd,
        cwd=cwd,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        bufsize=1,
    )
    code = stream_print_process(proc, verbose=args.verbose)
    return EXIT_SUCCESS if code == 0 else EXIT_FAILURE


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="接管本机 Claude Code：列出任务、追加对话、实时跟踪进度。",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  %(prog)s list
  %(prog)s list --all
  %(prog)s status
  %(prog)s watch a6250c2e-2a10-4a5b-bd02-c6b3828a0111
  %(prog)s reply a6250c2e --watch -- 请补上单元测试
  %(prog)s send --cwd /home/newdisk/work/fx-ai --yes -- 列出当前目录
        """,
    )
    sub = parser.add_subparsers(dest="command", required=True)

    p_list = sub.add_parser("list", help="列出正在进行（或全部）的任务")
    p_list.add_argument("--all", action="store_true", help="包含历史会话")
    p_list.add_argument("--json", action="store_true", help="以 JSON 输出")
    p_list.set_defaults(func=cmd_list)

    p_status = sub.add_parser("status", help="查看本机 Claude Code 会话和套接字")
    p_status.add_argument("--json", action="store_true", help="以 JSON 输出")
    p_status.set_defaults(func=cmd_status)

    p_watch = sub.add_parser("watch", help="实时跟踪某个会话的 jsonl 进度")
    p_watch.add_argument("session_id", help="会话 ID 或名称，支持前缀")
    p_watch.add_argument("--from-start", action="store_true", help="从文件开头开始打印")
    p_watch.add_argument("--interval", type=float, default=0.3, help="轮询间隔秒数")
    p_watch.add_argument("-v", "--verbose", action="store_true", help="输出思考过程")
    p_watch.set_defaults(func=cmd_watch)

    p_hist = sub.add_parser("history", help="打印会话最近的对话摘要")
    p_hist.add_argument("session_id", help="会话 ID 或名称，支持前缀")
    p_hist.add_argument("--tail", type=int, default=80, help="读取末尾多少行 jsonl")
    p_hist.add_argument("-v", "--verbose", action="store_true", help="输出思考过程")
    p_hist.set_defaults(func=cmd_history)

    p_reply = sub.add_parser("reply", help="向已有会话追加一条用户消息")
    p_reply.add_argument("session_id", help="会话 ID 或名称，支持前缀")
    p_reply.add_argument("text", nargs="*", help="要追加的内容，也可从标准输入读入")
    p_reply.add_argument("--watch", action="store_true", help="发送后持续打印进度")
    p_reply.add_argument("--interval", type=float, default=0.3, help="跟踪轮询间隔")
    p_reply.add_argument("--timeout", type=float, default=0, help="跟踪空闲超时秒数，0 表示不限")
    p_reply.add_argument("--permission-mode", help="离线续聊时传给 claude -p")
    p_reply.add_argument("--yes", action="store_true", help="离线续聊时跳过权限确认")
    p_reply.add_argument("-v", "--verbose", action="store_true", help="输出思考过程")
    p_reply.set_defaults(func=cmd_reply)

    p_send = sub.add_parser("send", help="新开一轮 Claude Code 对话（claude -p）")
    p_send.add_argument("text", nargs="*", help="要发送的内容，也可从标准输入读入")
    p_send.add_argument("--cwd", help="工作目录，默认当前目录")
    p_send.add_argument("--session-id", help="指定新会话 UUID")
    p_send.add_argument("--resume", help="按会话 ID 续聊（不走 live 套接字）")
    p_send.add_argument("--permission-mode", help="传给 claude -p 的权限模式")
    p_send.add_argument("--yes", action="store_true", help="跳过权限确认")
    p_send.add_argument("-v", "--verbose", action="store_true", help="输出思考过程")
    p_send.set_defaults(func=cmd_send)
    return parser


def main(argv: Optional[List[str]] = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    try:
        return int(args.func(args))
    except ClaudeError as exc:
        print(f"错误: {exc}", file=sys.stderr)
        return EXIT_FAILURE
    except KeyboardInterrupt:
        print("\n已中断。", file=sys.stderr)
        return 130


if __name__ == "__main__":
    sys.exit(main())
