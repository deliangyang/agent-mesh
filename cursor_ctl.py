#!/usr/bin/env python3
"""接管本机 Cursor Agent CLI：列出进行中的任务，追加对话，并实时跟踪进度。

Cursor Agent（`agent` / `cursor-agent`）把会话写在
`~/.cursor/chats/<cwd 的 md5>/<uuid>/`（meta.json + store.db），
对话 jsonl 在 `~/.cursor/projects/<目录编码>/agent-transcripts/<uuid>/<uuid>.jsonl`。

正在跑的交互进程能从 /proc 对上打开的 store.db 和终端。

本工具：

- 扫描本机 `agent` 进程，列出在线会话
- 对已有会话走 `agent --resume -p --output-format stream-json` 追加对话
- 新开一轮走 `agent -p`
- 跟踪 jsonl / stream-json，实时打印对话进度

Usage:
    python3 cursor_ctl.py list
    python3 cursor_ctl.py list --all
    python3 cursor_ctl.py status
    python3 cursor_ctl.py watch SESSION_ID
    python3 cursor_ctl.py reply SESSION_ID "请继续，补上测试"
    python3 cursor_ctl.py send "帮我看看这个目录" --cwd /path/to/project
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import select
import shutil
import sqlite3
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
FIRST_OUTPUT_SEC = 45.0
USER_QUERY_RE = re.compile(r"<user_query>(.*?)</user_query>", re.S)
CONVERSATION_ID_RE = re.compile(
    r"conversationId\"\s*:\s*\"([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\"",
    re.I,
)
UUID_DIR_RE = re.compile(
    r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$",
    re.I,
)


class CursorError(RuntimeError):
    pass


# ---------------------------------------------------------------------------
# 路径与发现
# ---------------------------------------------------------------------------


def cursor_config_dir() -> Path:
    env = os.environ.get("CURSOR_CONFIG_DIR", "").strip()
    if env:
        return Path(os.path.expanduser(env))
    xdg = os.environ.get("XDG_CONFIG_HOME", "").strip()
    if xdg:
        return Path(os.path.expanduser(xdg)) / "cursor"
    return Path.home() / ".cursor"


def cursor_data_dir() -> Path:
    env = os.environ.get("CURSOR_DATA_DIR", "").strip()
    if env:
        return Path(os.path.expanduser(env))
    return Path.home() / ".cursor"


def find_agent_bin() -> str:
    for key in ("CURSOR_AGENT_BIN", "AGENT_BIN"):
        env = os.environ.get(key, "").strip()
        if env:
            return env
    for name in ("agent", "cursor-agent"):
        found = shutil.which(name)
        if found:
            return found
    for fallback in (
        Path.home() / ".local" / "bin" / "agent",
        Path.home() / ".local" / "bin" / "cursor-agent",
    ):
        if fallback.is_file():
            return str(fallback)
    raise CursorError("找不到 agent / cursor-agent，请把它加入 PATH 或设置 CURSOR_AGENT_BIN。")


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
    text = re.sub(r"[^a-zA-Z0-9]", "-", cwd or "")
    text = re.sub(r"-+", "-", text).strip("-")
    return text


def cwd_hash(cwd: str) -> str:
    resolved = str(Path(cwd).resolve()) if cwd else cwd
    return hashlib.md5(resolved.encode("utf-8")).hexdigest()


def find_jsonl(data_dir: Path, session_id: str, cwd: str = "") -> Optional[Path]:
    projects = data_dir / "projects"
    if cwd:
        candidate = (
            projects
            / encode_project_dir(cwd)
            / "agent-transcripts"
            / session_id
            / f"{session_id}.jsonl"
        )
        if candidate.is_file():
            return candidate
    if not projects.is_dir() or not session_id:
        return None
    matches = list(projects.glob(f"*/agent-transcripts/{session_id}/{session_id}.jsonl"))
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
            elif kind in {"thinking", "reasoning"} and (item.get("thinking") or item.get("text")):
                parts.append(str(item.get("thinking") or item.get("text") or ""))
            elif item.get("text"):
                parts.append(str(item["text"]))
            elif item.get("result"):
                parts.append(str(item.get("result")))
        return "".join(parts)
    if isinstance(content, dict):
        return str(content.get("text") or content.get("result") or "")
    return str(content)


def extract_user_text(text: str) -> str:
    if not text:
        return ""
    match = USER_QUERY_RE.search(text)
    if match:
        return match.group(1).strip()
    stripped = text.strip()
    if stripped.startswith("<local-command-caveat>"):
        return ""
    if stripped.startswith("<user_info>") or stripped.startswith("<available_subagent_types>"):
        return ""
    if "system-reminder" in text and "<user_query>" not in text:
        return ""
    if text.startswith("<timestamp>") and "<user_query>" not in text:
        return re.sub(r"<timestamp>.*?</timestamp>", "", text, flags=re.S).strip()
    return stripped


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
    tty: str = ""
    log_path: str = ""
    store_path: str = ""
    updated_at: float = 0.0
    extra_args: str = ""


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
    tty: str = ""
    log_path: str = ""
    store_path: str = ""
    jsonl_path: str = ""
    kind: str = ""
    source: str = ""

    @property
    def running(self) -> bool:
        if self.busy:
            return True
        if not self.jsonl_path:
            return False
        try:
            return time.time() - Path(self.jsonl_path).stat().st_mtime <= JSONL_ACTIVE_SEC
        except OSError:
            return False


def read_proc_cmdline(pid: int) -> str:
    try:
        raw = Path(f"/proc/{pid}/cmdline").read_bytes()
    except OSError:
        return ""
    return raw.replace(b"\x00", b" ").decode("utf-8", "replace").strip()


def read_proc_cwd(pid: int) -> str:
    try:
        return os.readlink(f"/proc/{pid}/cwd")
    except OSError:
        return ""


def iter_proc_fds(pid: int) -> Iterable[str]:
    fd_dir = Path(f"/proc/{pid}/fd")
    try:
        entries = list(fd_dir.iterdir())
    except OSError:
        return
    for entry in entries:
        try:
            yield os.readlink(entry)
        except OSError:
            continue


def is_cursor_agent_proc(cmdline: str) -> bool:
    if "share/cursor-agent" not in cmdline:
        return False
    if "index.js" not in cmdline:
        return False
    if "cursor_ctl.py" in cmdline:
        return False
    return True


def classify_kind(cmdline: str) -> str:
    args = cmdline.split()
    tail = " ".join(args[3:]) if len(args) > 3 else ""
    if "--print" in args or "-p" in args:
        return "print"
    if tail.strip() == "ls" or tail.startswith("ls "):
        return "ls"
    if "--resume" in tail or tail.strip() == "resume":
        return "resume"
    if "persist" in args:
        return "persist"
    if "acp" in args:
        return "acp"
    return "interactive"


def find_log_path(pid: int, fds: List[str]) -> str:
    for item in fds:
        if "/cursor-agent-logs-" in item and item.endswith(".log"):
            return item
    uid = os.getuid()
    log_dir = Path(f"/tmp/cursor-agent-logs-{uid}")
    if not log_dir.is_dir():
        return ""
    matches = list(log_dir.glob(f"session-*-{pid}-*.log"))
    if matches:
        return str(max(matches, key=lambda p: p.stat().st_mtime))
    return ""


def session_id_from_store_path(path: str) -> str:
    name = Path(path).parent.name
    if UUID_DIR_RE.match(name):
        return name
    return ""


def session_id_from_log(log_path: str) -> str:
    if not log_path or not Path(log_path).is_file():
        return ""
    found = ""
    try:
        with Path(log_path).open("r", encoding="utf-8", errors="replace") as handle:
            for raw in handle:
                match = CONVERSATION_ID_RE.search(raw)
                if match:
                    found = match.group(1)
                    if "conversationClassification.init" in raw or "Loading existing" in raw:
                        return found
    except OSError:
        return found
    return found


def discover_live() -> List[LiveSession]:
    out: List[LiveSession] = []
    proc_root = Path("/proc")
    try:
        pids = [int(p.name) for p in proc_root.iterdir() if p.name.isdigit()]
    except OSError:
        return out
    for pid in pids:
        cmdline = read_proc_cmdline(pid)
        if not is_cursor_agent_proc(cmdline):
            continue
        cwd = read_proc_cwd(pid)
        fds = list(iter_proc_fds(pid))
        store = ""
        tty = ""
        for item in fds:
            if not store and item.endswith("/store.db"):
                store = item
            if not tty and "/pts/" in item:
                tty = item
        log_path = find_log_path(pid, fds)
        session_id = session_id_from_store_path(store) or session_id_from_log(log_path)
        started = 0.0
        try:
            started = Path(f"/proc/{pid}").stat().st_ctime
        except OSError:
            pass
        updated = started
        for path_str in (store, log_path, store + "-wal" if store else ""):
            if not path_str:
                continue
            try:
                updated = max(updated, Path(path_str).stat().st_mtime)
            except OSError:
                continue
        args = cmdline.split()
        extra = " ".join(args[3:]) if len(args) > 3 else ""
        out.append(
            LiveSession(
                pid=pid,
                session_id=session_id,
                cwd=cwd,
                kind=classify_kind(cmdline),
                started_at=started,
                tty=tty,
                log_path=log_path,
                store_path=store,
                updated_at=updated,
                extra_args=extra.strip(),
                status="idle",
            )
        )
    out.sort(key=lambda s: (-s.updated_at, -s.started_at))
    return out


def load_json_file(path: Path) -> Dict[str, Any]:
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {}
    return data if isinstance(data, dict) else {}


def decode_store_meta_value(value: Any) -> Dict[str, Any]:
    if value is None:
        return {}
    if isinstance(value, bytes):
        try:
            text = value.decode("utf-8")
        except UnicodeDecodeError:
            return {}
    else:
        text = str(value)
    text = text.strip()
    if not text:
        return {}
    if re.fullmatch(r"[0-9a-fA-F]+", text) and len(text) % 2 == 0:
        try:
            text = bytes.fromhex(text).decode("utf-8")
        except (ValueError, UnicodeDecodeError):
            return {}
    try:
        data = json.loads(text)
    except json.JSONDecodeError:
        return {}
    return data if isinstance(data, dict) else {}


def read_store_meta(store_path: Path) -> Dict[str, Any]:
    if not store_path.is_file():
        return {}
    try:
        con = sqlite3.connect(f"file:{store_path}?mode=ro", uri=True)
        try:
            row = con.execute("select value from meta where key = '0'").fetchone()
        finally:
            con.close()
    except sqlite3.Error:
        return {}
    if not row:
        return {}
    meta = decode_store_meta_value(row[0])
    meta.pop("blobEncryptionKey", None)
    return meta


def iter_chat_dirs(config_dir: Path) -> Iterable[Path]:
    roots: List[Path] = [config_dir / "chats"]
    legacy = Path.home() / ".config" / "cursor" / "chats"
    try:
        if legacy.is_dir() and legacy.resolve() != roots[0].resolve():
            roots.append(legacy)
    except OSError:
        if legacy.is_dir():
            roots.append(legacy)
    seen = set()
    for root in roots:
        if not root.is_dir():
            continue
        try:
            hash_dirs = list(root.iterdir())
        except OSError:
            continue
        for hash_dir in hash_dirs:
            if not hash_dir.is_dir():
                continue
            try:
                chats = list(hash_dir.iterdir())
            except OSError:
                continue
            for chat_dir in chats:
                if not chat_dir.is_dir():
                    continue
                if not UUID_DIR_RE.match(chat_dir.name):
                    continue
                key = str(chat_dir)
                if key in seen:
                    continue
                seen.add(key)
                yield chat_dir


def merge_sessions(
    config_dir: Path,
    data_dir: Path,
    live: List[LiveSession],
) -> List[SessionInfo]:
    by_id: Dict[str, SessionInfo] = {}
    for item in live:
        meta: Dict[str, Any] = {}
        title = item.name
        model = ""
        cwd = item.cwd
        updated = item.updated_at
        if item.store_path:
            chat_dir = Path(item.store_path).parent
            file_meta = load_json_file(chat_dir / "meta.json")
            title = str(file_meta.get("title") or title)
            cwd = str(file_meta.get("cwd") or cwd)
            updated = max(updated, ts_to_epoch(file_meta.get("updatedAtMs")))
            store_meta = read_store_meta(Path(item.store_path))
            title = str(store_meta.get("name") or title)
            model = str(store_meta.get("lastUsedModel") or "")
            meta = store_meta
        jsonl = find_jsonl(data_dir, item.session_id, cwd) if item.session_id else None
        sid = item.session_id or f"pid-{item.pid}"
        info = SessionInfo(
            session_id=sid,
            title=title,
            cwd=cwd,
            status=item.status or "idle",
            name=str(meta.get("name") or title),
            model=model,
            updated_at=updated or item.started_at,
            live=True,
            pid=item.pid,
            tty=item.tty,
            log_path=item.log_path,
            store_path=item.store_path,
            jsonl_path=str(jsonl) if jsonl else "",
            kind=item.kind,
            source="live",
        )
        info.busy = info.running
        info.status = "进行中" if info.running else "在线"
        by_id[sid] = info

    for chat_dir in iter_chat_dirs(config_dir):
        sid = chat_dir.name
        file_meta = load_json_file(chat_dir / "meta.json")
        store = chat_dir / "store.db"
        cwd = str(file_meta.get("cwd") or "")
        title = str(file_meta.get("title") or "")
        updated = ts_to_epoch(file_meta.get("updatedAtMs") or file_meta.get("createdAtMs"))
        try:
            updated = max(updated, store.stat().st_mtime if store.is_file() else 0.0)
        except OSError:
            pass
        jsonl = find_jsonl(data_dir, sid, cwd)
        if jsonl:
            try:
                updated = max(updated, jsonl.stat().st_mtime)
            except OSError:
                pass
        if sid in by_id:
            info = by_id[sid]
            info.title = info.title or title
            info.cwd = info.cwd or cwd
            info.store_path = info.store_path or (str(store) if store.is_file() else "")
            info.jsonl_path = info.jsonl_path or (str(jsonl) if jsonl else "")
            info.updated_at = max(info.updated_at, updated)
            continue
        by_id[sid] = SessionInfo(
            session_id=sid,
            title=title,
            cwd=cwd,
            status="历史",
            name=title,
            updated_at=updated,
            store_path=str(store) if store.is_file() else "",
            jsonl_path=str(jsonl) if jsonl else "",
            source="chats",
        )

    result = list(by_id.values())
    result.sort(key=lambda s: (not s.live, not s.running, -s.updated_at))
    return result


def find_session(sessions: List[SessionInfo], session_id: str) -> SessionInfo:
    needle = session_id.strip()
    if not needle:
        raise CursorError("请提供会话 ID。")
    if needle.isdigit():
        pid = int(needle)
        hits = [s for s in sessions if s.pid == pid]
        if len(hits) == 1:
            return hits[0]
        if len(hits) > 1:
            raise CursorError(f"pid {pid} 对应多个会话。")
    exact = [s for s in sessions if s.session_id == needle]
    if exact:
        return exact[0]
    prefix = [s for s in sessions if s.session_id.startswith(needle)]
    if len(prefix) == 1:
        return prefix[0]
    titles = [s for s in sessions if needle in (s.title or "") or needle in (s.name or "")]
    if len(titles) == 1:
        return titles[0]
    cwds = [s for s in sessions if s.cwd and needle == Path(s.cwd).name]
    if len(cwds) == 1:
        return cwds[0]
    if len(prefix) > 1 or len(titles) > 1:
        raise CursorError(f"会话 ID 不唯一: {session_id}")
    raise CursorError(f"未找到会话: {session_id}")


# ---------------------------------------------------------------------------
# 进度格式化
# ---------------------------------------------------------------------------


def format_content_blocks(content: Any, verbose: bool = False) -> Optional[str]:
    if not isinstance(content, list):
        text = extract_user_text(content_to_text(content))
        return text or None
    lines: List[str] = []
    for item in content:
        if not isinstance(item, dict):
            continue
        kind = item.get("type")
        if kind == "tool_use":
            name = item.get("name") or "tool"
            args = item.get("input") or item.get("arguments") or {}
            if isinstance(args, dict):
                preview = (
                    args.get("command")
                    or args.get("path")
                    or args.get("description")
                    or json.dumps(args, ensure_ascii=False)
                )
            else:
                preview = str(args)
            lines.append(f"[工具调用] {name} {truncate(str(preview), 200)}")
        elif kind in {"tool-result", "tool_result"}:
            text = str(item.get("result") or item.get("text") or content_to_text(item))
            lines.append(f"[工具结果] {truncate(text, 200)}")
        elif kind in {"thinking", "reasoning"} and verbose:
            think = item.get("thinking") or item.get("text") or ""
            if think:
                lines.append(f"[思考] {truncate(str(think), 200)}")
        elif kind in {"text", "input_text", "output_text"}:
            text = item.get("text") or ""
            if text:
                lines.append(truncate(extract_user_text(str(text)) or str(text), 400 if verbose else 240))
    return "\n".join(line for line in lines if line) or None


def format_jsonl_event(obj: Dict[str, Any], verbose: bool = False) -> Optional[str]:
    role = str(obj.get("role") or obj.get("type") or "")
    msg = obj.get("message") if isinstance(obj.get("message"), dict) else obj
    content = msg.get("content") if isinstance(msg, dict) else None
    if role in {"user"}:
        text = extract_user_text(content_to_text(content))
        if not text:
            return None
        return f"[用户] {truncate(text, 400 if verbose else 240)}"
    if role in {"assistant"}:
        formatted = format_content_blocks(content, verbose=verbose)
        if not formatted:
            return None
        if formatted.startswith("["):
            return formatted
        return f"[助手] {formatted}"
    if role in {"tool"}:
        return f"[工具结果] {truncate(content_to_text(content), 200)}"
    if obj.get("type") in {"ai-title", "custom-title"}:
        return f"[标题] {obj.get('aiTitle') or obj.get('customTitle') or obj.get('title') or ''}"
    return None


def format_tool_call(tool: Any) -> str:
    if tool is None:
        return ""
    if isinstance(tool, str):
        return tool
    if not isinstance(tool, dict):
        return truncate(str(tool), 200)
    case = ""
    inner: Any = tool
    node = tool.get("tool")
    if isinstance(node, dict):
        case = str(node.get("case") or node.get("type") or "")
        inner = node.get("value") or node
    name = (
        tool.get("name")
        or tool.get("toolName")
        or tool.get("tool_name")
        or case.replace("ToolCall", "")
        or case
    )
    args = inner.get("args") if isinstance(inner, dict) else None
    if args is None and isinstance(inner, dict):
        args = inner.get("command") or inner.get("path") or inner.get("query")
    preview = ""
    if isinstance(args, dict):
        preview = str(args.get("command") or args.get("path") or json.dumps(args, ensure_ascii=False))
    elif args:
        preview = str(args)
    elif isinstance(inner, dict):
        for key in ("command", "path", "query", "url", "filePath"):
            if inner.get(key):
                preview = str(inner[key])
                break
    return f"{name} {truncate(preview, 200)}".strip()


def format_stream_event(obj: Dict[str, Any], verbose: bool = False) -> Optional[str]:
    kind = obj.get("type")
    if kind == "system" and obj.get("subtype") == "init":
        sid = obj.get("session_id") or obj.get("sessionId") or ""
        model = obj.get("model") or ""
        cwd = obj.get("cwd") or ""
        return f"[初始化] session={sid} model={model} cwd={cwd}".strip()
    if kind in {"user", "assistant"}:
        return format_jsonl_event(obj, verbose=verbose)
    if kind == "thinking":
        if obj.get("subtype") == "delta":
            text = obj.get("text") or ""
            return text if verbose and text else None
        if verbose:
            return "[思考结束]"
        return None
    if kind == "tool_call":
        subtype = obj.get("subtype") or ""
        label = "工具开始" if subtype == "started" else "工具结束"
        detail = format_tool_call(obj.get("tool_call"))
        return f"[{label}] {detail}".strip()
    if kind == "result":
        subtype = obj.get("subtype") or ""
        result = obj.get("result")
        if isinstance(result, str) and result.strip():
            return f"[结束] {subtype} {truncate(result, 300)}"
        return f"[结束] {subtype or 'done'}"
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
# 发送：agent -p --resume
# ---------------------------------------------------------------------------


def handle_stream_line(raw: str, verbose: bool, streamed: bool) -> bool:
    """打印一行 stream-json。返回当前是否仍在输出增量文本。"""
    raw = raw.rstrip("\n")
    if not raw.strip():
        return streamed
    try:
        obj = json.loads(raw)
    except json.JSONDecodeError:
        if streamed:
            print(flush=True)
        print(raw, flush=True)
        return False
    if not isinstance(obj, dict):
        return streamed
    kind = obj.get("type")
    if kind == "assistant":
        msg = obj.get("message") or {}
        text = content_to_text(msg.get("content") if isinstance(msg, dict) else obj.get("content"))
        if not text:
            return streamed
        if obj.get("timestamp_ms"):
            print(text, end="", flush=True)
            return True
        if streamed:
            print(flush=True)
            return False
        print(f"[助手] {text}", flush=True)
        return False
    if kind == "thinking" and obj.get("subtype") == "delta":
        if verbose:
            print(obj.get("text") or "", end="", flush=True)
            return True
        return streamed
    if streamed:
        print(flush=True)
    line = format_stream_event(obj, verbose=verbose)
    if line:
        print(line, flush=True)
    return False


def stream_print_process(
    proc: subprocess.Popen[str],
    verbose: bool = False,
    first_output_timeout: float = FIRST_OUTPUT_SEC,
) -> int:
    assert proc.stdout is not None
    streamed = False
    stdout = proc.stdout
    fd = stdout.fileno()
    try:
        ready, _, _ = select.select([fd], [], [], first_output_timeout)
        if not ready:
            print(
                f"等待 {first_output_timeout:.0f} 秒仍无输出，正在结束子进程。"
                "若提示词是 ls / resume 等，已被当成 agent 子命令；本工具会改走标准输入。",
                file=sys.stderr,
            )
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
            return EXIT_FAILURE
        leftover = ""
        while True:
            chunk = os.read(fd, 8192)
            if not chunk:
                break
            leftover += chunk.decode("utf-8", errors="replace")
            while "\n" in leftover:
                line, leftover = leftover.split("\n", 1)
                streamed = handle_stream_line(line, verbose, streamed)
        if leftover.strip():
            streamed = handle_stream_line(leftover, verbose, streamed)
        if streamed:
            print(flush=True)
    except KeyboardInterrupt:
        proc.terminate()
        print("\n已中断。")
    return proc.wait()


def build_print_cmd(
    agent_bin: str,
    *,
    resume: Optional[str] = None,
    session_id: Optional[str] = None,
    workspace: Optional[str] = None,
    mode: Optional[str] = None,
    model: Optional[str] = None,
    skip_permissions: bool = False,
    extra: Optional[List[str]] = None,
) -> List[str]:
    cmd = [
        agent_bin,
        "-p",
        "--output-format",
        "stream-json",
        "--stream-partial-output",
        "--trust",
        "--approve-mcps",
    ]
    if resume:
        cmd.extend(["--resume", resume])
    if session_id:
        cmd.extend(["--new-session-id", session_id])
    if workspace:
        cmd.extend(["--workspace", workspace])
    if mode:
        cmd.extend(["--mode", mode])
    if model:
        cmd.extend(["--model", model])
    if skip_permissions:
        cmd.append("--force")
    if extra:
        cmd.extend(extra)
    return cmd


def start_print_agent(cmd: List[str], prompt: str, cwd: str) -> subprocess.Popen[str]:
    """提示词走标准输入，避免 ls/resume 等被 commander 解析成子命令。"""
    env = os.environ.copy()
    env.setdefault("CI", "1")
    proc = subprocess.Popen(
        cmd,
        cwd=cwd,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        bufsize=0,
        env=env,
    )
    assert proc.stdin is not None
    payload = prompt if prompt.endswith("\n") else prompt + "\n"
    try:
        proc.stdin.write(payload)
        proc.stdin.close()
    except BrokenPipeError:
        pass
    return proc


def history_from_store(store_path: Path, tail: int) -> List[Dict[str, Any]]:
    if not store_path.is_file():
        return []
    events: List[Dict[str, Any]] = []
    try:
        con = sqlite3.connect(f"file:{store_path}?mode=ro", uri=True)
        try:
            rows = con.execute("select data from blobs order by rowid").fetchall()
        finally:
            con.close()
    except sqlite3.Error:
        return []
    for (data,) in rows:
        if not isinstance(data, (bytes, str)):
            continue
        if isinstance(data, bytes):
            if not data.lstrip().startswith(b"{") and not data.lstrip().startswith(b"["):
                continue
            try:
                text = data.decode("utf-8")
            except UnicodeDecodeError:
                continue
        else:
            text = data
        try:
            obj = json.loads(text)
        except json.JSONDecodeError:
            continue
        if isinstance(obj, dict) and obj.get("role") in {"user", "assistant", "tool"}:
            events.append(obj)
    if tail > 0:
        events = events[-tail:]
    return events


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
        "model": info.model,
        "updated_at": fmt_time(info.updated_at),
        "jsonl_path": info.jsonl_path,
        "store_path": info.store_path,
        "tty": info.tty,
        "log_path": info.log_path,
    }


def cmd_list(args: argparse.Namespace) -> int:
    config_dir = cursor_config_dir()
    data_dir = cursor_data_dir()
    bin_path = find_agent_bin()
    live = discover_live()
    sessions = merge_sessions(config_dir, data_dir, live)
    if not args.all:
        sessions = [s for s in sessions if s.live or s.running]
    if args.json:
        print_json(
            {
                "agent_bin": bin_path,
                "config_dir": str(config_dir),
                "data_dir": str(data_dir),
                "live": [session_to_dict(s) for s in sessions if s.live],
                "sessions": [session_to_dict(s) for s in sessions],
            }
        )
        return EXIT_SUCCESS

    print(f"Cursor 配置目录: {config_dir}")
    print(f"数据目录: {data_dir}")
    print(f"可执行文件: {bin_path}")
    print(f"在线会话: {len(live)}")
    for item in live:
        print(
            f"  [{item.kind:<12}] pid={item.pid} {item.session_id or '-'}  {item.cwd or '-'}"
        )
    print()
    if not sessions:
        print("当前没有正在进行的任务。用 --all 查看历史会话。")
        return EXIT_SUCCESS

    print(f"{'状态':<8} {'更新时间':<20} {'会话 ID':<38} 标题 / 目录")
    for info in sessions:
        if info.running and info.busy:
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
    config_dir = cursor_config_dir()
    data_dir = cursor_data_dir()
    live = discover_live()
    sessions = [s for s in merge_sessions(config_dir, data_dir, live) if s.live]
    if args.json:
        print_json([session_to_dict(s) for s in sessions])
        return EXIT_SUCCESS
    if not sessions:
        print("没有发现正在运行的 Cursor Agent 会话。")
        return EXIT_NOT_FOUND
    for item in sessions:
        print(f"pid={item.pid}  {item.session_id}")
        print(f"  名称: {item.title or item.name or '-'}  状态: {item.status or 'idle'}  类型: {item.kind}")
        print(f"  目录: {item.cwd or '-'}")
        print(f"  模型: {item.model or '-'}")
        print(f"  终端: {item.tty or '-'}")
        print(f"  存储: {item.store_path or '-'}")
        print(f"  jsonl: {item.jsonl_path or '-'}")
        print(f"  日志: {item.log_path or '-'}")
        print(f"  启动: {fmt_time(item.updated_at)}")
    return EXIT_SUCCESS


def wait_for_jsonl(path: Path, timeout: float = 8.0) -> Optional[Path]:
    deadline = time.time() + timeout
    while time.time() < deadline:
        if path.is_file():
            return path
        time.sleep(0.2)
    return path if path.is_file() else None


def cmd_watch(args: argparse.Namespace) -> int:
    config_dir = cursor_config_dir()
    data_dir = cursor_data_dir()
    sessions = merge_sessions(config_dir, data_dir, discover_live())
    info = find_session(sessions, args.session_id)
    path = Path(info.jsonl_path) if info.jsonl_path else find_jsonl(data_dir, info.session_id, info.cwd)
    if path is None or not path.is_file():
        expected = (
            data_dir
            / "projects"
            / encode_project_dir(info.cwd or "")
            / "agent-transcripts"
            / info.session_id
            / f"{info.session_id}.jsonl"
        )
        print(f"会话 {info.session_id} 还没有 jsonl，等待生成…")
        print(f"预期文件 {expected}")
        path = wait_for_jsonl(expected, timeout=max(args.interval * 10, 8.0))
        if path is None:
            print("没有 jsonl 记录，无法跟踪进度。", file=sys.stderr)
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
    config_dir = cursor_config_dir()
    data_dir = cursor_data_dir()
    sessions = merge_sessions(config_dir, data_dir, discover_live())
    info = find_session(sessions, args.session_id)
    path = Path(info.jsonl_path) if info.jsonl_path else find_jsonl(data_dir, info.session_id, info.cwd)
    printed = 0
    if path is not None and path.is_file():
        lines = path.read_text(encoding="utf-8", errors="replace").splitlines()
        keep = lines[-args.tail :] if args.tail > 0 else lines
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
    elif info.store_path:
        print(f"没有 jsonl，改从 store.db 读取: {info.store_path}")
        for obj in history_from_store(Path(info.store_path), args.tail):
            line = format_jsonl_event(obj, verbose=args.verbose)
            if line:
                print(line)
                printed += 1
    else:
        print(f"会话 {info.session_id} 还没有对话记录。")
        return EXIT_SUCCESS
    if printed == 0:
        print("没有可展示的对话内容。")
    return EXIT_SUCCESS


def resolve_text(args: argparse.Namespace) -> str:
    if args.text:
        return " ".join(args.text).strip()
    if not sys.stdin.isatty():
        return sys.stdin.read().strip()
    return ""


def follow_jsonl(path: Path, interval: float, timeout: float, verbose: bool) -> None:
    watcher = JsonlWatcher(path)
    watcher.seek_end()
    idle_rounds = 0
    try:
        while True:
            events = watcher.poll()
            if events:
                idle_rounds = 0
                for obj in events:
                    line = format_jsonl_event(obj, verbose=verbose)
                    if line:
                        print(line, flush=True)
            else:
                idle_rounds += 1
            time.sleep(interval)
            if timeout > 0 and idle_rounds * interval >= timeout:
                print("等待超时，停止跟踪（会话可能仍在跑）。")
                break
    except KeyboardInterrupt:
        print("\n已停止跟踪。")


def cmd_reply(args: argparse.Namespace) -> int:
    text = resolve_text(args)
    if not text:
        print("请提供要追加的对话内容。", file=sys.stderr)
        return EXIT_INVALID_ARGS
    config_dir = cursor_config_dir()
    data_dir = cursor_data_dir()
    bin_path = find_agent_bin()
    sessions = merge_sessions(config_dir, data_dir, discover_live())
    info = find_session(sessions, args.session_id)
    if not UUID_DIR_RE.match(info.session_id):
        print(f"会话 {info.session_id} 还没有稳定的 UUID，无法 --resume。", file=sys.stderr)
        return EXIT_NOT_FOUND
    jsonl = Path(info.jsonl_path) if info.jsonl_path else find_jsonl(data_dir, info.session_id, info.cwd)

    print(f"目标会话: {info.session_id}")
    print(f"目录: {info.cwd or '-'}")
    print(f"状态: {'在线 ' + (info.kind or 'idle') if info.live else '离线'}")
    if info.live and info.tty:
        print(f"终端: {info.tty}  (pid={info.pid})，追加会写入同一会话存储。")

    cwd = info.cwd or os.getcwd()
    cmd = build_print_cmd(
        bin_path,
        resume=info.session_id,
        workspace=cwd,
        mode=args.mode,
        model=args.model,
        skip_permissions=args.yes,
    )
    print("命令:", " ".join(cmd), "（提示词走标准输入）")
    print("--- 进度 ---")
    proc = start_print_agent(cmd, text, cwd)
    code = stream_print_process(proc, verbose=args.verbose)
    if args.watch and code == 0:
        path = jsonl or find_jsonl(data_dir, info.session_id, info.cwd)
        if path is None:
            print("没有 jsonl，无法继续跟踪。")
        else:
            print("--- 继续跟踪 jsonl ---")
            follow_jsonl(path, args.interval, args.timeout, args.verbose)
    return EXIT_SUCCESS if code == 0 else EXIT_FAILURE


def cmd_send(args: argparse.Namespace) -> int:
    text = resolve_text(args)
    if not text:
        print("请提供要发送的内容。", file=sys.stderr)
        return EXIT_INVALID_ARGS
    bin_path = find_agent_bin()
    cwd = args.cwd or os.getcwd()
    cmd = build_print_cmd(
        bin_path,
        resume=args.resume,
        session_id=args.session_id,
        workspace=cwd,
        mode=args.mode,
        model=args.model,
        skip_permissions=args.yes,
    )
    print(f"工作目录: {cwd}")
    print("已启动 agent -p（提示词走标准输入），等待回复…")
    print("--- 进度 ---")
    proc = start_print_agent(cmd, text, cwd)
    code = stream_print_process(proc, verbose=args.verbose)
    return EXIT_SUCCESS if code == 0 else EXIT_FAILURE


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="接管本机 Cursor Agent CLI：列出任务、追加对话、实时跟踪进度。",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  %(prog)s list
  %(prog)s list --all
  %(prog)s status
  %(prog)s watch 3e22a1a4-8293-4ddc-838f-d9ac4e27d50a
  %(prog)s reply 3e22a1a4 --watch -- 请补上单元测试
  %(prog)s send --cwd /home/newdisk/work/fx-ai --yes -- 列出当前目录
        """,
    )
    sub = parser.add_subparsers(dest="command", required=True)

    p_list = sub.add_parser("list", help="列出正在进行（或全部）的任务")
    p_list.add_argument("--all", action="store_true", help="包含历史会话")
    p_list.add_argument("--json", action="store_true", help="以 JSON 输出")
    p_list.set_defaults(func=cmd_list)

    p_status = sub.add_parser("status", help="查看本机 Cursor Agent 会话")
    p_status.add_argument("--json", action="store_true", help="以 JSON 输出")
    p_status.set_defaults(func=cmd_status)

    p_watch = sub.add_parser("watch", help="实时跟踪某个会话的 jsonl 进度")
    p_watch.add_argument("session_id", help="会话 ID、标题或 pid，支持前缀")
    p_watch.add_argument("--from-start", action="store_true", help="从文件开头开始打印")
    p_watch.add_argument("--interval", type=float, default=0.3, help="轮询间隔秒数")
    p_watch.add_argument("-v", "--verbose", action="store_true", help="输出思考过程")
    p_watch.set_defaults(func=cmd_watch)

    p_hist = sub.add_parser("history", help="打印会话最近的对话摘要")
    p_hist.add_argument("session_id", help="会话 ID、标题或 pid，支持前缀")
    p_hist.add_argument("--tail", type=int, default=80, help="读取末尾多少条记录")
    p_hist.add_argument("-v", "--verbose", action="store_true", help="输出思考过程")
    p_hist.set_defaults(func=cmd_history)

    p_reply = sub.add_parser("reply", help="向已有会话追加一条用户消息")
    p_reply.add_argument("session_id", help="会话 ID、标题或 pid，支持前缀")
    p_reply.add_argument("text", nargs="*", help="要追加的内容，也可从标准输入读入")
    p_reply.add_argument("--watch", action="store_true", help="发送结束后继续跟踪 jsonl")
    p_reply.add_argument("--interval", type=float, default=0.3, help="跟踪轮询间隔")
    p_reply.add_argument("--timeout", type=float, default=0, help="跟踪空闲超时秒数，0 表示不限")
    p_reply.add_argument("--mode", choices=("ask", "plan"), help="ask 只读问答，plan 只规划")
    p_reply.add_argument("--model", help="指定模型")
    p_reply.add_argument("--yes", action="store_true", help="跳过权限确认（--force）")
    p_reply.add_argument("-v", "--verbose", action="store_true", help="输出思考过程")
    p_reply.set_defaults(func=cmd_reply)

    p_send = sub.add_parser("send", help="新开一轮 Cursor Agent 对话（agent -p）")
    p_send.add_argument("text", nargs="*", help="要发送的内容，也可从标准输入读入")
    p_send.add_argument("--cwd", help="工作目录，默认当前目录")
    p_send.add_argument("--session-id", help="指定新会话 UUID（--new-session-id）")
    p_send.add_argument("--resume", help="按会话 ID 续聊")
    p_send.add_argument("--mode", choices=("ask", "plan"), help="ask 只读问答，plan 只规划")
    p_send.add_argument("--model", help="指定模型")
    p_send.add_argument("--yes", action="store_true", help="跳过权限确认（--force）")
    p_send.add_argument("-v", "--verbose", action="store_true", help="输出思考过程")
    p_send.set_defaults(func=cmd_send)
    return parser


def main(argv: Optional[List[str]] = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    try:
        return int(args.func(args))
    except CursorError as exc:
        print(f"错误: {exc}", file=sys.stderr)
        return EXIT_FAILURE
    except KeyboardInterrupt:
        print("\n已中断。", file=sys.stderr)
        return 130


if __name__ == "__main__":
    sys.exit(main())
