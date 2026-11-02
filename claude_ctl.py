#!/usr/bin/env python3
"""Drive local Claude Code: list running tasks, follow up, and stream progress.

Claude Code writes sessions to ~/.claude/projects/<encoded cwd>/<session>.jsonl.
A live interactive process registers a Unix socket in ~/.claude/sessions/<pid>.json.

This tool:

- lists local interactive/background sessions with `claude agents --json`
- injects user messages (auth + user) into a live session socket
- resumes an idle session with `claude -p --resume` + stream-json
- follows jsonl / stream-json and prints progress

Usage:
    python3 claude_ctl.py list
    python3 claude_ctl.py list --all
    python3 claude_ctl.py status
    python3 claude_ctl.py watch SESSION_ID
    python3 claude_ctl.py reply SESSION_ID "please continue and add tests"
    python3 claude_ctl.py send "please look at this directory" --cwd /path/to/project
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
# paths and discovery
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
    raise ClaudeError("cannot find the claude binary; add it to PATH or set CLAUDE_BIN")


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
# session model
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
    """Returns (socket_path, peer_token)."""
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
        raise ClaudeError(f"claude agents failed: {exc}") from exc
    if proc.returncode != 0:
        err = (proc.stderr or proc.stdout or "").strip()
        raise ClaudeError(err or f"claude agents exit code {proc.returncode}")
    text = proc.stdout.strip()
    if not text:
        return []
    try:
        data = json.loads(text)
    except json.JSONDecodeError as exc:
        raise ClaudeError(f"claude agents --json is not valid JSON: {exc}") from exc
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
    """Read title, cwd, and last activity from jsonl."""
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
        raise ClaudeError(f"session id is not unique: {session_id}")
    raise ClaudeError(f"session not found: {session_id}")


# ---------------------------------------------------------------------------
# progress formatting
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
            return f"[tool result] {truncate(text, 200)}"
        text = extract_user_text(content_to_text(content))
        if not text:
            return None
        return f"[user] {truncate(text, 400 if verbose else 240)}"
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
                    lines.append(f"[tool call] {name} {truncate(str(preview), 200)}")
                elif block == "thinking" and verbose:
                    think = item.get("thinking") or ""
                    if think:
                        lines.append(f"[thinking] {truncate(str(think), 200)}")
                elif block == "text":
                    text = item.get("text") or ""
                    if text:
                        lines.append(f"[assistant] {truncate(str(text), 400 if verbose else 240)}")
            return "\n".join(lines) if lines else None
        text = content_to_text(content)
        return f"[assistant] {truncate(text, 240)}" if text else None
    if kind in {"ai-title", "custom-title"}:
        return f"[title] {obj.get('aiTitle') or obj.get('customTitle') or ''}"
    if kind == "progress" and verbose:
        return f"[progress] {truncate(json.dumps(obj, ensure_ascii=False), 200)}"
    return None


def format_stream_event(obj: Dict[str, Any], verbose: bool = False) -> Optional[str]:
    kind = obj.get("type")
    if kind == "system" and obj.get("subtype") == "init":
        sid = obj.get("session_id") or obj.get("sessionId") or ""
        model = obj.get("model") or ""
        cwd = obj.get("cwd") or ""
        return f"[init] session={sid} model={model} cwd={cwd}".strip()
    if kind == "assistant":
        return format_jsonl_event(obj, verbose=verbose)
    if kind == "user":
        return format_jsonl_event(obj, verbose=verbose)
    if kind == "result":
        subtype = obj.get("subtype") or obj.get("result") or ""
        result = obj.get("result")
        if isinstance(result, str) and result.strip():
            return f"[done] {subtype} {truncate(result, 300)}"
        return f"[done] {subtype or 'done'}"
    if kind == "stream_event":
        event = obj.get("event") or {}
        delta = event.get("delta") or {}
        text = delta.get("text") or ""
        if text:
            return text
        if verbose:
            et = event.get("type") or ""
            if et:
                return f"[stream] {et}"
        return None
    if kind == "error":
        return f"[error] {obj.get('error') or obj.get('message') or obj}"
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
# send: live socket / claude -p
# ---------------------------------------------------------------------------


def inject_live_prompt(session: SessionInfo, text: str) -> None:
    if not session.socket_path:
        raise ClaudeError("this session has no messaging socket; cannot inject")
    if not session.peer_token:
        raise ClaudeError("cannot find this session's peerToken (sessions/<pid>.*.key)")
    if not Path(session.socket_path).exists():
        raise ClaudeError(f"socket does not exist: {session.socket_path}")
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
        raise ClaudeError(f"failed to write to the Claude socket: {exc}") from exc
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
        print("\ninterrupted.")
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
# commands
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

    print(f"Claude dir: {root}")
    print(f"binary: {bin_path}")
    print(f"live sessions: {len(live)}")
    for item in live:
        print(
            f"  [{item.status or 'idle':<6}] pid={item.pid} {item.session_id}  {item.name or '-'}  {item.cwd}"
        )
    print()
    if not sessions:
        print("no running tasks; pass --all to list history")
        return EXIT_SUCCESS

    print(f"{'Status':<8} {'Updated':<20} {'Session ID':<38} Title / cwd")
    for info in sessions:
        if info.running:
            flag = "In progress"
        elif info.live:
            flag = "online"
        else:
            flag = info.status or "history"
        title = info.title or info.name or "(untitled)"
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
        print("no running Claude Code session found")
        return EXIT_NOT_FOUND
    for item in live:
        sock_ok = Path(item.socket_path).exists() if item.socket_path else False
        print(f"pid={item.pid}  {item.session_id}")
        print(f"  name: {item.name or '-'}  status: {item.status or 'idle'}  kind: {item.kind}")
        print(f"  cwd: {item.cwd or '-'}")
        print(f"  socket: {item.socket_path or '-'}  ({'ok' if sock_ok else 'missing'})")
        print(f"  inject key: {'found' if item.peer_token else 'missing'}")
        print(f"  started: {fmt_time(item.started_at)}")
    return EXIT_SUCCESS


def cmd_watch(args: argparse.Namespace) -> int:
    root = claude_dir()
    sessions = merge_sessions(root, discover_live(root, find_claude_bin()))
    info = find_session(sessions, args.session_id)
    path = Path(info.jsonl_path) if info.jsonl_path else find_jsonl(root, info.session_id, info.cwd)
    if path is None:
        print(f"session {info.session_id} has no jsonl; cannot follow progress", file=sys.stderr)
        return EXIT_NOT_FOUND
    print(f"following {info.session_id}")
    print(f"file  {path}")
    print(f"cwd  {info.cwd or '-'}")
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
        print("\nstopped following.")
        return EXIT_SUCCESS


def cmd_history(args: argparse.Namespace) -> int:
    root = claude_dir()
    sessions = merge_sessions(root, discover_live(root, find_claude_bin()))
    info = find_session(sessions, args.session_id)
    path = Path(info.jsonl_path) if info.jsonl_path else find_jsonl(root, info.session_id, info.cwd)
    if path is None or not path.is_file():
        print(f"session record not found: {info.session_id}", file=sys.stderr)
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
        print("nothing to show")
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
        print("provide the follow-up text", file=sys.stderr)
        return EXIT_INVALID_ARGS
    root = claude_dir()
    bin_path = find_claude_bin()
    sessions = merge_sessions(root, discover_live(root, bin_path))
    info = find_session(sessions, args.session_id)
    jsonl = Path(info.jsonl_path) if info.jsonl_path else find_jsonl(root, info.session_id, info.cwd)

    print(f"target session: {info.session_id}")
    print(f"cwd: {info.cwd or '-'}")
    print(f"status: {'online ' + (info.status or 'idle') if info.live else 'offline'}")

    if info.live and info.socket_path and info.peer_token:
        inject_live_prompt(info, text)
        print("injected into the running session over the Unix socket")
        if args.watch:
            if jsonl is None:
                print("no jsonl; cannot follow progress. Watch from another terminal if needed.")
                return EXIT_SUCCESS
            print("--- progress ---")
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
                        print("timed out waiting; stopped following (the session may still be running)")
                        break
            except KeyboardInterrupt:
                print("\nstopped following.")
        return EXIT_SUCCESS

    print("session is offline; falling back to claude -p --resume")
    cmd = build_print_cmd(
        bin_path,
        text,
        resume=info.session_id,
        permission_mode=args.permission_mode,
        skip_permissions=args.yes,
    )
    cwd = info.cwd or os.getcwd()
    print("command:", " ".join(cmd[:8]), "...")
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
        print("provide the text to send", file=sys.stderr)
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
    print(f"working directory: {cwd}")
    print("started claude -p; waiting for a reply…")
    print("--- progress ---")
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
        description="Drive local Claude Code: list tasks, follow up, and stream progress.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  %(prog)s list
  %(prog)s list --all
  %(prog)s status
  %(prog)s watch a6250c2e-2a10-4a5b-bd02-c6b3828a0111
  %(prog)s reply a6250c2e --watch -- please add unit tests
  %(prog)s send --cwd /path/to/project --yes -- list this directory
        """,
    )
    sub = parser.add_subparsers(dest="command", required=True)

    p_list = sub.add_parser("list", help="List running (or all) tasks")
    p_list.add_argument("--all", action="store_true", help="include history")
    p_list.add_argument("--json", action="store_true", help="JSON output")
    p_list.set_defaults(func=cmd_list)

    p_status = sub.add_parser("status", help="List local Claude Code sessions and sockets")
    p_status.add_argument("--json", action="store_true", help="JSON output")
    p_status.set_defaults(func=cmd_status)

    p_watch = sub.add_parser("watch", help="Follow a session's jsonl in real time")
    p_watch.add_argument("session_id", help="session id or name; prefixes allowed")
    p_watch.add_argument("--from-start", action="store_true", help="print from the start of the file")
    p_watch.add_argument("--interval", type=float, default=0.3, help="poll interval in seconds")
    p_watch.add_argument("-v", "--verbose", action="store_true", help="print thinking")
    p_watch.set_defaults(func=cmd_watch)

    p_hist = sub.add_parser("history", help="Print a recent transcript summary")
    p_hist.add_argument("session_id", help="session id or name; prefixes allowed")
    p_hist.add_argument("--tail", type=int, default=80, help="how many trailing jsonl lines to read")
    p_hist.add_argument("-v", "--verbose", action="store_true", help="print thinking")
    p_hist.set_defaults(func=cmd_history)

    p_reply = sub.add_parser("reply", help="Append a user message to an existing session")
    p_reply.add_argument("session_id", help="session id or name; prefixes allowed")
    p_reply.add_argument("text", nargs="*", help="text to append; stdin is also accepted")
    p_reply.add_argument("--watch", action="store_true", help="keep printing progress after send")
    p_reply.add_argument("--interval", type=float, default=0.3, help="follow poll interval")
    p_reply.add_argument("--timeout", type=float, default=0, help="idle follow timeout in seconds; 0 means no limit")
    p_reply.add_argument("--permission-mode", help="passed to claude -p on offline resume")
    p_reply.add_argument("--yes", action="store_true", help="skip permission prompts on offline resume")
    p_reply.add_argument("-v", "--verbose", action="store_true", help="print thinking")
    p_reply.set_defaults(func=cmd_reply)

    p_send = sub.add_parser("send", help="Start a new Claude Code turn (claude -p)")
    p_send.add_argument("text", nargs="*", help="text to send; stdin is also accepted")
    p_send.add_argument("--cwd", help="working directory; defaults to cwd")
    p_send.add_argument("--session-id", help="New session UUID")
    p_send.add_argument("--resume", help="Resume by session id (not the live socket)")
    p_send.add_argument("--permission-mode", help="permission mode for claude -p")
    p_send.add_argument("--yes", action="store_true", help="skip permission prompts")
    p_send.add_argument("-v", "--verbose", action="store_true", help="print thinking")
    p_send.set_defaults(func=cmd_send)
    return parser


def main(argv: Optional[List[str]] = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    try:
        return int(args.func(args))
    except ClaudeError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return EXIT_FAILURE
    except KeyboardInterrupt:
        print("\ninterrupted.", file=sys.stderr)
        return 130


if __name__ == "__main__":
    sys.exit(main())
