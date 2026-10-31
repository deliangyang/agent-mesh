#!/usr/bin/env python3
"""接管本机 WorkBuddy：列出进行中的任务，追加对话，并实时跟踪进度。

WorkBuddy 桌面端会拉起本地 CodeBuddy CLI 网关（ACP / REST）。
本工具发现这些网关、读取会话库和 jsonl 记录，然后通过：

- GET  /api/v1/sessions/live          查看当前会话
- POST /api/v1/sessions/{id}/reply    向正在进行的会话追加消息
- POST /api/v1/acp                    ACP（initialize / session/load / session/prompt）
- 跟踪 projects/<cwd>/<id>.jsonl      实时打印对话进度

Usage:
    python3 workbuddy_ctl.py list
    python3 workbuddy_ctl.py list --all
    python3 workbuddy_ctl.py status
    python3 workbuddy_ctl.py watch SESSION_ID
    python3 workbuddy_ctl.py reply SESSION_ID "请继续，补上测试"
    python3 workbuddy_ctl.py send "帮我看看这个目录" --cwd /path/to/project
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sqlite3
import sys
import threading
import time
import uuid
from dataclasses import dataclass, field
from datetime import datetime
from http.client import HTTPConnection, HTTPResponse
from pathlib import Path
from typing import Any, Callable, Dict, Iterable, Iterator, List, Optional, Tuple
from urllib.parse import urlparse


EXIT_SUCCESS = 0
EXIT_FAILURE = 1
EXIT_INVALID_ARGS = 2
EXIT_NOT_FOUND = 3

HEARTBEAT_ALIVE_SEC = 45.0
JSONL_ACTIVE_SEC = 20.0
DEFAULT_ACP_PROTOCOL = 1

USER_QUERY_RE = re.compile(r"<user_query>(.*?)</user_query>", re.S)


# ---------------------------------------------------------------------------
# 路径与发现
# ---------------------------------------------------------------------------


def workbuddy_dir() -> Path:
    env = os.environ.get("WORKBUDDY_CONFIG_DIR", "").strip()
    if env:
        return Path(os.path.expanduser(env))
    return Path.home() / ".workbuddy"


def now_ms() -> int:
    return int(time.time() * 1000)


def ts_to_epoch(value: Any) -> float:
    """WorkBuddy 时间戳可能是毫秒或秒。"""
    if value is None:
        return 0.0
    try:
        n = float(value)
    except (TypeError, ValueError):
        return 0.0
    if n > 1e12:
        return n / 1000.0
    if n > 1e10:
        return n / 1000.0
    return n


def fmt_time(value: Any) -> str:
    epoch = ts_to_epoch(value)
    if epoch <= 0:
        return "-"
    return datetime.fromtimestamp(epoch).strftime("%Y-%m-%d %H:%M:%S")


def age_text(value: Any) -> str:
    epoch = ts_to_epoch(value)
    if epoch <= 0:
        return "-"
    sec = max(0, time.time() - epoch)
    if sec < 60:
        return f"{int(sec)}秒前"
    if sec < 3600:
        return f"{int(sec // 60)}分钟前"
    if sec < 86400:
        return f"{int(sec // 3600)}小时前"
    return f"{int(sec // 86400)}天前"


def pid_alive(pid: int) -> bool:
    if pid <= 0:
        return False
    try:
        os.kill(pid, 0)
        return True
    except OSError:
        return False
    except Exception:
        return Path(f"/proc/{pid}").exists()


def read_proc_environ(pid: int) -> Dict[str, str]:
    path = Path(f"/proc/{pid}/environ")
    try:
        raw = path.read_bytes()
    except OSError:
        return {}
    out: Dict[str, str] = {}
    for item in raw.split(b"\0"):
        if not item or b"=" not in item:
            continue
        key, _, value = item.partition(b"=")
        try:
            out[key.decode()] = value.decode(errors="replace")
        except Exception:
            continue
    return out


def is_host_session_id(session_id: str) -> bool:
    return session_id.startswith("interactive-")


def is_host_cwd(cwd: str) -> bool:
    return "__workbuddy_cli_host__" in (cwd or "")


@dataclass
class LiveGateway:
    pid: int
    url: str
    session_id: str
    cwd: str
    kind: str
    last_heartbeat: float
    password: str = ""
    host: bool = False

    @property
    def alive(self) -> bool:
        if not pid_alive(self.pid):
            return False
        if self.last_heartbeat <= 0:
            return True
        return (time.time() - self.last_heartbeat) <= HEARTBEAT_ALIVE_SEC


def iter_session_pid_files(root: Path) -> Iterable[Path]:
    sessions_dir = root / "sessions"
    if not sessions_dir.is_dir():
        return []
    return sorted(sessions_dir.glob("*.json"))


def discover_gateways(root: Optional[Path] = None) -> List[LiveGateway]:
    root = root or workbuddy_dir()
    found: List[LiveGateway] = []
    for path in iter_session_pid_files(root):
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            continue
        pid = int(data.get("pid") or 0)
        url = (data.get("url") or data.get("endpoint") or "").strip()
        if not pid or not url:
            continue
        session_id = str(data.get("sessionId") or "")
        cwd = str(data.get("cwd") or "")
        env = read_proc_environ(pid) if pid_alive(pid) else {}
        password = (
            env.get("CODEBUDDY_GATEWAY_PASSWORD")
            or os.environ.get("CODEBUDDY_GATEWAY_PASSWORD")
            or os.environ.get("WORKBUDDY_GATEWAY_PASSWORD")
            or ""
        )
        gw = LiveGateway(
            pid=pid,
            url=url.rstrip("/"),
            session_id=session_id,
            cwd=cwd,
            kind=str(data.get("kind") or ""),
            last_heartbeat=ts_to_epoch(data.get("lastHeartbeat") or data.get("updatedAt")),
            password=password,
            host=is_host_session_id(session_id) or is_host_cwd(cwd),
        )
        if gw.alive:
            found.append(gw)
    found.sort(key=lambda g: (g.host, -g.last_heartbeat))
    return found


def find_jsonl(root: Path, session_id: str) -> Optional[Path]:
    projects = root / "projects"
    if not projects.is_dir():
        return None
    direct = list(projects.glob(f"**/{session_id}.jsonl"))
    if direct:
        return max(direct, key=lambda p: p.stat().st_mtime)
    return None


def extract_user_query(text: str) -> str:
    if not text:
        return ""
    match = USER_QUERY_RE.search(text)
    if match:
        return match.group(1).strip()
    return text.strip()


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
            for key in ("text", "input_text", "output_text"):
                if item.get(key):
                    parts.append(str(item[key]))
                    break
            else:
                if item.get("type") in {"text", "input_text", "output_text"}:
                    parts.append(str(item.get("text") or ""))
        return "".join(parts)
    if isinstance(content, dict):
        return str(content.get("text") or "")
    return str(content)


# ---------------------------------------------------------------------------
# HTTP / ACP
# ---------------------------------------------------------------------------


class GatewayError(RuntimeError):
    def __init__(self, message: str, status: int = 0, payload: Any = None) -> None:
        super().__init__(message)
        self.status = status
        self.payload = payload


class GatewayClient:
    """WorkBuddy 本地 CLI 网关 HTTP 客户端。"""

    def __init__(self, url: str, password: str, timeout: float = 15.0) -> None:
        parsed = urlparse(url)
        if parsed.scheme not in {"http", "https"}:
            raise GatewayError(f"不支持的网关地址: {url}")
        self.host = parsed.hostname or "127.0.0.1"
        self.port = parsed.port or (443 if parsed.scheme == "https" else 80)
        self.password = password
        self.timeout = timeout

    def _headers(self, extra: Optional[Dict[str, str]] = None) -> Dict[str, str]:
        headers = {
            "Authorization": f"Bearer {self.password}",
            "Accept": "application/json",
        }
        if extra:
            headers.update(extra)
        return headers

    def request(
        self,
        method: str,
        path: str,
        body: Any = None,
        headers: Optional[Dict[str, str]] = None,
        timeout: Optional[float] = None,
        raw: bool = False,
    ) -> Tuple[int, Dict[str, str], bytes]:
        conn = HTTPConnection(self.host, self.port, timeout=timeout or self.timeout)
        payload = None
        hdr = self._headers(headers)
        if body is not None:
            payload = json.dumps(body, ensure_ascii=False).encode("utf-8")
            hdr["Content-Type"] = "application/json; charset=utf-8"
        try:
            conn.request(method, path, body=payload, headers=hdr)
            resp = conn.getresponse()
            data = resp.read()
            return resp.status, {k.lower(): v for k, v in resp.getheaders()}, data
        finally:
            conn.close()

    def json(
        self,
        method: str,
        path: str,
        body: Any = None,
        headers: Optional[Dict[str, str]] = None,
        timeout: Optional[float] = None,
    ) -> Any:
        status, _, data = self.request(method, path, body=body, headers=headers, timeout=timeout)
        text = data.decode("utf-8", errors="replace") if data else ""
        try:
            parsed = json.loads(text) if text else {}
        except json.JSONDecodeError:
            parsed = {"raw": text}
        if status >= 400:
            err = parsed.get("error") if isinstance(parsed, dict) else None
            if isinstance(err, dict):
                msg = err.get("message") or err.get("code") or text
            else:
                msg = text or f"HTTP {status}"
            raise GatewayError(str(msg), status=status, payload=parsed)
        return parsed

    def unwrap(self, payload: Any) -> Any:
        if isinstance(payload, dict) and "data" in payload:
            return payload["data"]
        return payload

    def health(self) -> Any:
        return self.unwrap(self.json("GET", "/api/v1/health"))

    def status(self) -> Any:
        return self.unwrap(self.json("GET", "/api/v1/status"))

    def live_session(self) -> Any:
        return self.unwrap(self.json("GET", "/api/v1/sessions/live"))

    def list_sessions(self, cwd: Optional[str] = None) -> List[Dict[str, Any]]:
        path = "/api/v1/sessions"
        if cwd:
            path += f"?cwd={cwd}"
        data = self.unwrap(self.json("GET", path))
        if isinstance(data, dict):
            return list(data.get("sessions") or [])
        return []

    def list_across_projects(self) -> List[Dict[str, Any]]:
        data = self.json("GET", "/api/v1/sessions/across-projects")
        if isinstance(data, dict):
            return list(data.get("sessions") or [])
        return []

    def reply(self, session_id: str, text: str) -> Any:
        return self.unwrap(
            self.json("POST", f"/api/v1/sessions/{session_id}/reply", {"text": text})
        )

    def create_run(self, text: str, conversation_id: Optional[str] = None) -> str:
        run_id = str(uuid.uuid4())
        conv_id = conversation_id or run_id
        body = {
            "id": run_id,
            "type": "message",
            "text": text,
            "prompt": text,
            "payload": {"text": text},
            "source": {
                "platform": "generic",
                "sender": {"id": "workbuddy-ctl"},
                "conversation": {"id": conv_id, "type": "direct"},
            },
        }
        result = self.json("POST", "/api/v1/runs", body)
        data = self.unwrap(result)
        if isinstance(data, dict) and data.get("runId"):
            return str(data["runId"])
        return run_id

    def open_stream(
        self,
        method: str,
        path: str,
        headers: Optional[Dict[str, str]] = None,
        body: Any = None,
        timeout: Optional[float] = None,
    ) -> Tuple[HTTPConnection, HTTPResponse]:
        conn = HTTPConnection(self.host, self.port, timeout=timeout)
        hdr = self._headers(headers)
        payload = None
        if body is not None:
            payload = json.dumps(body, ensure_ascii=False).encode("utf-8")
            hdr["Content-Type"] = "application/json; charset=utf-8"
        conn.request(method, path, body=payload, headers=hdr)
        resp = conn.getresponse()
        if resp.status >= 400:
            raw = resp.read().decode("utf-8", errors="replace")
            conn.close()
            raise GatewayError(raw or f"HTTP {resp.status}", status=resp.status)
        return conn, resp


@dataclass
class AcpEvent:
    kind: str
    payload: Dict[str, Any]


class AcpClient:
    """ACP：先 connect，再挂 SSE，随后 POST JSON-RPC。"""

    def __init__(self, gateway: GatewayClient) -> None:
        self.gateway = gateway
        self.connection_id = ""
        self.session_token = ""
        self._sse_conn: Optional[HTTPConnection] = None
        self._lock = threading.Lock()
        self._cv = threading.Condition(self._lock)
        self._pending: Dict[Any, Dict[str, Any]] = {}
        self._events: List[AcpEvent] = []
        self._thread: Optional[threading.Thread] = None
        self._closed = False
        self._rpc_id = 0
        self.session_id = ""

    def _next_id(self) -> int:
        self._rpc_id += 1
        return self._rpc_id

    def _acp_headers(self, extra: Optional[Dict[str, str]] = None) -> Dict[str, str]:
        headers = {
            "acp-connection-id": self.connection_id,
            "Accept": "application/json, text/event-stream",
        }
        if self.session_token:
            headers["acp-session-token"] = self.session_token
        if extra:
            headers.update(extra)
        return headers

    def connect(self) -> None:
        data = self.gateway.json("POST", "/api/v1/acp/connect", {})
        self.connection_id = data.get("connectionId") or ""
        self.session_token = data.get("sessionToken") or ""
        if not self.connection_id:
            raise GatewayError("ACP connect 未返回 connectionId")
        conn, resp = self.gateway.open_stream(
            "GET",
            "/api/v1/acp",
            headers=self._acp_headers({"Accept": "text/event-stream"}),
            timeout=None,
        )
        self._sse_conn = conn
        self._thread = threading.Thread(
            target=self._read_sse, args=(resp,), name="acp-sse", daemon=True
        )
        self._thread.start()

    def close(self) -> None:
        self._closed = True
        try:
            if self.connection_id:
                self.gateway.request(
                    "DELETE",
                    "/api/v1/acp",
                    headers=self._acp_headers(),
                    timeout=3,
                )
        except Exception:
            pass
        if self._sse_conn is not None:
            try:
                self._sse_conn.close()
            except Exception:
                pass
            self._sse_conn = None

    def __enter__(self) -> "AcpClient":
        self.connect()
        return self

    def __exit__(self, exc_type, exc, tb) -> None:
        self.close()

    def _read_sse(self, resp: HTTPResponse) -> None:
        buf = ""
        try:
            while not self._closed:
                chunk = resp.read(1)
                if not chunk:
                    break
                buf += chunk.decode("utf-8", errors="replace")
                while "\n\n" in buf:
                    raw, buf = buf.split("\n\n", 1)
                    self._handle_sse_block(raw)
        except Exception:
            if not self._closed:
                with self._cv:
                    self._cv.notify_all()

    def _parse_sse_block(self, raw: str) -> Optional[Tuple[str, Dict[str, Any]]]:
        event_name = "message"
        data_lines: List[str] = []
        for line in raw.splitlines():
            if line.startswith(":"):
                continue
            if line.startswith("event:"):
                event_name = line[6:].strip()
            elif line.startswith("data:"):
                data_lines.append(line[5:].lstrip())
        if not data_lines:
            return None
        try:
            payload = json.loads("\n".join(data_lines))
        except json.JSONDecodeError:
            return None
        if not isinstance(payload, dict):
            return None
        return event_name, payload

    def _handle_sse_block(self, raw: str) -> None:
        parsed = self._parse_sse_block(raw)
        if not parsed:
            return
        event_name, payload = parsed
        with self._cv:
            if "id" in payload and ("result" in payload or "error" in payload):
                self._pending[payload["id"]] = payload
            else:
                self._events.append(AcpEvent(kind=event_name, payload=payload))
            self._cv.notify_all()

    def _iter_sse(self, resp: HTTPResponse) -> Iterator[Tuple[str, Dict[str, Any]]]:
        buf = ""
        while not self._closed:
            chunk = resp.read(1)
            if not chunk:
                break
            buf += chunk.decode("utf-8", errors="replace")
            while "\n\n" in buf:
                raw, buf = buf.split("\n\n", 1)
                parsed = self._parse_sse_block(raw)
                if parsed:
                    yield parsed

    def _post_rpc(
        self,
        method: str,
        params: Optional[Dict[str, Any]],
        timeout: Optional[float],
    ) -> Iterator[AcpEvent]:
        rpc_id = self._next_id()
        body = {"jsonrpc": "2.0", "id": rpc_id, "method": method, "params": params or {}}
        conn, resp = self.gateway.open_stream(
            "POST",
            "/api/v1/acp",
            headers=self._acp_headers(),
            body=body,
            timeout=timeout,
        )
        try:
            ctype = (resp.getheader("Content-Type") or "").lower()
            if "event-stream" not in ctype:
                raw = resp.read().decode("utf-8", errors="replace")
                payload = json.loads(raw) if raw else {}
                yield AcpEvent(kind="rpc-result", payload=payload)
                return
            for event_name, payload in self._iter_sse(resp):
                if payload.get("id") == rpc_id and ("result" in payload or "error" in payload):
                    yield AcpEvent(kind="rpc-result", payload=payload)
                    return
                yield AcpEvent(kind=event_name, payload=payload)
        finally:
            conn.close()

    def call(self, method: str, params: Optional[Dict[str, Any]] = None, timeout: float = 60.0) -> Any:
        reply: Optional[Dict[str, Any]] = None
        extras: List[AcpEvent] = []
        for event in self._post_rpc(method, params, timeout):
            if event.kind == "rpc-result":
                reply = event.payload
                break
            extras.append(event)
        if extras:
            with self._cv:
                self._events.extend(extras)
                self._cv.notify_all()
        if reply is None:
            raise GatewayError(f"ACP 调用没有返回结果: {method}")
        if reply.get("error"):
            err = reply["error"]
            if isinstance(err, dict):
                raise GatewayError(str(err.get("message") or err), payload=err)
            raise GatewayError(str(err), payload=err)
        return reply.get("result")

    def drain_events(self) -> List[AcpEvent]:
        with self._cv:
            events = self._events
            self._events = []
            return events

    def wait_events(self, timeout: float = 0.2) -> List[AcpEvent]:
        with self._cv:
            if not self._events:
                self._cv.wait(timeout=timeout)
            events = self._events
            self._events = []
            return events

    def initialize(self) -> Any:
        return self.call(
            "initialize",
            {
                "protocolVersion": DEFAULT_ACP_PROTOCOL,
                "clientInfo": {"name": "workbuddy-ctl", "version": "1.0.0"},
                "clientCapabilities": {},
            },
            timeout=20.0,
        )

    def session_new(self, cwd: str, session_id: Optional[str] = None) -> str:
        params: Dict[str, Any] = {"cwd": cwd, "mcpServers": []}
        if session_id:
            params["sessionId"] = session_id
        result = self.call("session/new", params, timeout=30.0)
        sid = (result or {}).get("sessionId") or session_id or ""
        self.session_id = str(sid)
        return self.session_id

    def session_load(self, session_id: str, cwd: Optional[str] = None) -> str:
        params: Dict[str, Any] = {"sessionId": session_id}
        if cwd:
            params["cwd"] = cwd
        result = self.call("session/load", params, timeout=60.0)
        sid = (result or {}).get("sessionId") or session_id
        self.session_id = str(sid)
        return self.session_id

    def session_prompt(self, text: str, session_id: Optional[str] = None) -> Iterator[AcpEvent]:
        sid = session_id or self.session_id
        if not sid:
            raise GatewayError("ACP session/prompt 缺少 sessionId")
        yield from self._post_rpc(
            "session/prompt",
            {
                "sessionId": sid,
                "prompt": [{"type": "text", "text": text}],
            },
            timeout=None,
        )


# ---------------------------------------------------------------------------
# 会话索引
# ---------------------------------------------------------------------------


@dataclass
class SessionInfo:
    session_id: str
    title: str = ""
    cwd: str = ""
    status: str = ""
    model: str = ""
    updated_at: float = 0.0
    source: str = ""
    live: bool = False
    busy: bool = False
    gateway_url: str = ""
    gateway_pid: int = 0
    jsonl_path: str = ""
    message_count: int = 0
    host: bool = False
    todos: List[Dict[str, Any]] = field(default_factory=list)

    @property
    def running(self) -> bool:
        if self.host:
            return False
        if self.live and self.busy:
            return True
        if self.live and self.status in {"running", "in_progress", "active"}:
            return True
        if self.jsonl_path:
            try:
                mtime = Path(self.jsonl_path).stat().st_mtime
                if time.time() - mtime <= JSONL_ACTIVE_SEC:
                    return True
            except OSError:
                pass
        return bool(self.live and not self.host)


def load_sqlite_sessions(root: Path) -> List[SessionInfo]:
    db = root / "workbuddy.db"
    if not db.is_file():
        return []
    try:
        conn = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    except sqlite3.Error:
        return []
    conn.row_factory = sqlite3.Row
    out: List[SessionInfo] = []
    try:
        rows = conn.execute(
            """
            SELECT id, title, custom_title, cwd, status, model, updated_at, last_activity_at
            FROM sessions
            WHERE deleted_at IS NULL
            ORDER BY COALESCE(last_activity_at, updated_at) DESC
            """
        )
        for row in rows:
            title = row["custom_title"] or row["title"] or ""
            updated = row["last_activity_at"] or row["updated_at"] or 0
            out.append(
                SessionInfo(
                    session_id=row["id"],
                    title=title,
                    cwd=row["cwd"] or "",
                    status=row["status"] or "",
                    model=row["model"] or "",
                    updated_at=ts_to_epoch(updated),
                    source="sqlite",
                    host=False,
                )
            )
    except sqlite3.Error:
        return []
    finally:
        conn.close()
    return out


def load_todos(root: Path, session_id: str) -> List[Dict[str, Any]]:
    task_dir = root / "tasks" / session_id
    if not task_dir.is_dir():
        return []
    items: List[Dict[str, Any]] = []
    for path in sorted(task_dir.glob("*.json"), key=lambda p: p.name):
        try:
            items.append(json.loads(path.read_text(encoding="utf-8")))
        except (OSError, json.JSONDecodeError):
            continue
    return items


def attach_jsonl(root: Path, info: SessionInfo) -> None:
    path = find_jsonl(root, info.session_id)
    if path:
        info.jsonl_path = str(path)
        if not info.updated_at:
            info.updated_at = path.stat().st_mtime


def merge_sessions(root: Path, gateways: List[LiveGateway]) -> List[SessionInfo]:
    by_id: Dict[str, SessionInfo] = {}
    for item in load_sqlite_sessions(root):
        by_id[item.session_id] = item
        attach_jsonl(root, item)
        item.todos = load_todos(root, item.session_id)

    for gw in gateways:
        if gw.host:
            continue
        info = by_id.get(gw.session_id)
        if info is None:
            info = SessionInfo(session_id=gw.session_id, cwd=gw.cwd, source="gateway")
            by_id[gw.session_id] = info
            attach_jsonl(root, info)
            info.todos = load_todos(root, gw.session_id)
        info.live = True
        info.gateway_url = gw.url
        info.gateway_pid = gw.pid
        info.cwd = info.cwd or gw.cwd
        info.updated_at = max(info.updated_at, gw.last_heartbeat)
        if gw.password:
            try:
                client = GatewayClient(gw.url, gw.password, timeout=5)
                live = client.live_session() or {}
                status = client.status() or {}
                info.busy = bool(status.get("busy"))
                live_id = live.get("sessionId")
                if live_id:
                    info.live = True
                    if live.get("writerOccupied"):
                        info.busy = True
            except Exception:
                pass

    # jsonl 有、库里没有的会话
    projects = root / "projects"
    if projects.is_dir():
        for path in projects.glob("**/*.jsonl"):
            if path.parent.name == "subagents":
                continue
            sid = path.stem
            if sid in by_id:
                continue
            by_id[sid] = SessionInfo(
                session_id=sid,
                jsonl_path=str(path),
                updated_at=path.stat().st_mtime,
                source="jsonl",
            )

    result = list(by_id.values())
    result.sort(key=lambda s: (not s.running, not s.live, -s.updated_at))
    return result


def pick_default_gateway(gateways: List[LiveGateway], session: Optional[SessionInfo] = None) -> LiveGateway:
    if session and session.gateway_pid:
        for gw in gateways:
            if gw.pid == session.gateway_pid:
                return gw
    conversation = [g for g in gateways if not g.host]
    if conversation:
        return conversation[0]
    if gateways:
        return gateways[0]
    raise GatewayError("没有发现正在运行的 WorkBuddy 网关。请先打开 WorkBuddy 客户端。")


# ---------------------------------------------------------------------------
# 进度展示
# ---------------------------------------------------------------------------


def truncate(text: str, limit: int = 240) -> str:
    text = re.sub(r"\s+", " ", text).strip()
    if len(text) <= limit:
        return text
    return text[: limit - 1] + "…"


def format_jsonl_event(obj: Dict[str, Any], verbose: bool = False) -> Optional[str]:
    kind = obj.get("type")
    if kind == "message":
        role = obj.get("role") or ""
        text = extract_user_query(content_to_text(obj.get("content")))
        if not text:
            return None
        label = "用户" if role == "user" else "助手"
        return f"[{label}] {truncate(text, 400 if verbose else 240)}"
    if kind == "function_call":
        name = obj.get("name") or "tool"
        display = obj.get("providerData", {}).get("argumentsDisplayText")
        args = display or obj.get("arguments") or ""
        if isinstance(args, (dict, list)):
            args = json.dumps(args, ensure_ascii=False)
        return f"[工具调用] {name} {truncate(str(args), 200)}"
    if kind == "function_call_result":
        output = content_to_text(obj.get("output") or obj.get("content") or obj.get("result"))
        if not output:
            output = json.dumps(obj, ensure_ascii=False)[:200]
        return f"[工具结果] {truncate(output, 200)}"
    if kind == "reasoning" and verbose:
        text = content_to_text(obj.get("rawContent") or obj.get("content"))
        if text:
            return f"[思考] {truncate(text, 200)}"
        return None
    if kind == "ai-title":
        return f"[标题] {obj.get('aiTitle') or ''}"
    if kind == "custom-title":
        return f"[标题] {obj.get('customTitle') or ''}"
    return None


def format_acp_event(event: AcpEvent, verbose: bool = False) -> Optional[str]:
    payload = event.payload
    if event.kind == "rpc-result":
        if payload.get("error"):
            err = payload["error"]
            msg = err.get("message") if isinstance(err, dict) else str(err)
            return f"[结束] 出错: {msg}"
        result = payload.get("result") or {}
        reason = result.get("stopReason") or "end_turn"
        return f"[结束] stopReason={reason}"
    method = payload.get("method")
    params = payload.get("params") or {}
    if method == "session/update":
        update = params.get("update") or params
        kind = update.get("sessionUpdate") or update.get("type") or ""
        if kind in {"agent_message_chunk", "agent_thought_chunk"}:
            text = content_to_text(update.get("content") or update.get("text"))
            if not text:
                return None
            prefix = "思考" if "thought" in kind else "助手"
            return f"[{prefix}] {text}" if verbose or "thought" not in kind else None
        if kind in {"tool_call", "tool_call_update"}:
            name = update.get("title") or update.get("toolName") or update.get("kind") or "tool"
            status = update.get("status") or ""
            return f"[工具] {name} {status}".strip()
        if kind == "available_commands_update":
            return None
        if verbose:
            return f"[更新] {truncate(json.dumps(update, ensure_ascii=False), 200)}"
        return None
    if method:
        return f"[ACP] {method}"
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
# 命令
# ---------------------------------------------------------------------------


def print_json(data: Any) -> None:
    print(json.dumps(data, ensure_ascii=False, indent=2))


def session_to_dict(info: SessionInfo) -> Dict[str, Any]:
    return {
        "session_id": info.session_id,
        "title": info.title,
        "cwd": info.cwd,
        "status": info.status,
        "model": info.model,
        "updated_at": fmt_time(info.updated_at),
        "live": info.live,
        "busy": info.busy,
        "running": info.running,
        "gateway_url": info.gateway_url,
        "gateway_pid": info.gateway_pid,
        "jsonl_path": info.jsonl_path,
        "todos": [
            {
                "id": t.get("id"),
                "subject": t.get("subject") or t.get("activeForm") or t.get("title"),
                "status": t.get("status"),
            }
            for t in info.todos
        ],
    }


def cmd_list(args: argparse.Namespace) -> int:
    root = workbuddy_dir()
    gateways = discover_gateways(root)
    sessions = merge_sessions(root, gateways)
    if not args.all:
        sessions = [s for s in sessions if s.running or s.live]
    if args.json:
        print_json(
            {
                "gateways": [
                    {
                        "pid": g.pid,
                        "url": g.url,
                        "session_id": g.session_id,
                        "cwd": g.cwd,
                        "host": g.host,
                        "alive": g.alive,
                    }
                    for g in gateways
                ],
                "sessions": [session_to_dict(s) for s in sessions],
            }
        )
        return EXIT_SUCCESS

    print(f"WorkBuddy 目录: {root}")
    print(f"在线网关: {len(gateways)}")
    for gw in gateways:
        kind = "宿主" if gw.host else "会话"
        print(
            f"  [{kind}] pid={gw.pid} {gw.url} session={gw.session_id} cwd={gw.cwd or '-'}"
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
        title = info.title or "(无标题)"
        print(
            f"{flag:<8} {fmt_time(info.updated_at):<20} {info.session_id:<38} {title}"
        )
        extra = []
        if info.cwd:
            extra.append(info.cwd)
        if info.model:
            extra.append(info.model)
        open_todos = [t for t in info.todos if t.get("status") not in {"completed", "cancelled", "canceled"}]
        if open_todos:
            extra.append(f"未完成子任务 {len(open_todos)}")
        if extra:
            print("         " + "  |  ".join(extra))
    return EXIT_SUCCESS


def find_session(root: Path, session_id: str, sessions: List[SessionInfo]) -> SessionInfo:
    exact = [s for s in sessions if s.session_id == session_id]
    if exact:
        return exact[0]
    prefix = [s for s in sessions if s.session_id.startswith(session_id)]
    if len(prefix) == 1:
        return prefix[0]
    if len(prefix) > 1:
        raise GatewayError(f"会话 ID 前缀不唯一: {session_id}")
    raise GatewayError(f"未找到会话: {session_id}")


def cmd_status(args: argparse.Namespace) -> int:
    root = workbuddy_dir()
    gateways = discover_gateways(root)
    if not gateways:
        print("没有发现正在运行的 WorkBuddy 网关。", file=sys.stderr)
        return EXIT_NOT_FOUND
    rows = []
    for gw in gateways:
        if not gw.password:
            rows.append({"pid": gw.pid, "url": gw.url, "error": "读不到网关密码"})
            continue
        client = GatewayClient(gw.url, gw.password, timeout=5)
        try:
            rows.append(
                {
                    "pid": gw.pid,
                    "url": gw.url,
                    "session_id": gw.session_id,
                    "host": gw.host,
                    "health": client.health(),
                    "status": client.status(),
                    "live": client.live_session(),
                }
            )
        except GatewayError as exc:
            rows.append({"pid": gw.pid, "url": gw.url, "error": str(exc)})
    if args.json:
        print_json(rows)
        return EXIT_SUCCESS
    for row in rows:
        print(f"pid={row['pid']} {row['url']}")
        if row.get("error"):
            print(f"  错误: {row['error']}")
            continue
        health = row.get("health") or {}
        status = row.get("status") or {}
        live = row.get("live") or {}
        print(f"  健康: {health.get('status')}  uptime={health.get('uptime')}")
        print(f"  忙碌: {status.get('busy')}  当前会话: {live.get('sessionId')}")
        print(f"  写入占用: {live.get('writerOccupied')}")
    return EXIT_SUCCESS


def watch_loop(
    jsonl_path: Optional[Path],
    acp: Optional[AcpClient] = None,
    verbose: bool = False,
    follow_prompt: bool = False,
    on_stop: Optional[Callable[[], bool]] = None,
) -> None:
    watcher = JsonlWatcher(jsonl_path) if jsonl_path else None
    if watcher and not follow_prompt:
        watcher.seek_end()
    stopped = False
    try:
        while not stopped:
            if watcher:
                for obj in watcher.poll():
                    line = format_jsonl_event(obj, verbose=verbose)
                    if line:
                        print(line, flush=True)
            if acp is not None:
                for event in acp.wait_events(timeout=0.2):
                    line = format_acp_event(event, verbose=verbose)
                    if line:
                        print(line, flush=True)
                    if event.kind == "rpc-result":
                        stopped = True
            else:
                time.sleep(0.25)
            if on_stop and on_stop():
                stopped = True
    except KeyboardInterrupt:
        print("\n已停止跟踪。")


def cmd_watch(args: argparse.Namespace) -> int:
    root = workbuddy_dir()
    gateways = discover_gateways(root)
    sessions = merge_sessions(root, gateways)
    info = find_session(root, args.session_id, sessions)
    path = Path(info.jsonl_path) if info.jsonl_path else find_jsonl(root, info.session_id)
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
    root = workbuddy_dir()
    sessions = merge_sessions(root, discover_gateways(root))
    info = find_session(root, args.session_id, sessions)
    path = Path(info.jsonl_path) if info.jsonl_path else find_jsonl(root, info.session_id)
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
    root = workbuddy_dir()
    gateways = discover_gateways(root)
    sessions = merge_sessions(root, gateways)
    info = find_session(root, args.session_id, sessions)
    gw = pick_default_gateway(gateways, info)
    if not gw.password:
        print("读不到网关密码，无法发送。", file=sys.stderr)
        return EXIT_FAILURE
    client = GatewayClient(gw.url, gw.password)
    jsonl = Path(info.jsonl_path) if info.jsonl_path else find_jsonl(root, info.session_id)
    print(f"目标会话: {info.session_id}")
    print(f"网关: {gw.url} (pid={gw.pid} {'宿主' if gw.host else '会话'})")
    print(f"目录: {info.cwd or gw.cwd or '-'}")

    delivered = False
    if info.live and not gw.host:
        try:
            client.reply(info.session_id, text)
            delivered = True
            print("已通过 live reply 追加到正在进行的会话。")
        except GatewayError as exc:
            print(f"live reply 不可用（{exc}），改走 ACP。")

    if delivered:
        if args.watch and jsonl:
            print("--- 进度 ---")
            watch_loop(jsonl, verbose=args.verbose)
        return EXIT_SUCCESS

    with AcpClient(client) as acp:
        acp.initialize()
        cwd = info.cwd or gw.cwd or os.getcwd()
        try:
            acp.session_load(info.session_id, cwd=cwd)
            print(f"已加载会话 {acp.session_id}")
        except GatewayError as exc:
            print(f"session/load 失败（{exc}），改为新建会话。")
            acp.session_new(cwd, session_id=info.session_id)
            print(f"已新建会话 {acp.session_id}")
        print("已发送 prompt，等待回复…")
        if args.watch:
            watcher = JsonlWatcher(jsonl) if jsonl else None
            if watcher:
                watcher.seek_end()
            print("--- 进度 ---")
            for event in acp.session_prompt(text):
                if watcher:
                    for obj in watcher.poll():
                        line = format_jsonl_event(obj, verbose=args.verbose)
                        if line:
                            print(line, flush=True)
                line = format_acp_event(event, verbose=args.verbose)
                if line:
                    print(line, flush=True)
        else:
            for event in acp.session_prompt(text):
                if event.kind == "rpc-result":
                    line = format_acp_event(event, verbose=True)
                    if line:
                        print(line)
    return EXIT_SUCCESS


def cmd_send(args: argparse.Namespace) -> int:
    text = resolve_text(args)
    if not text:
        print("请提供要发送的内容。", file=sys.stderr)
        return EXIT_INVALID_ARGS
    root = workbuddy_dir()
    gateways = discover_gateways(root)
    gw = pick_default_gateway(gateways)
    if not gw.password:
        print("读不到网关密码，无法发送。", file=sys.stderr)
        return EXIT_FAILURE
    cwd = args.cwd or (None if gw.host else gw.cwd) or os.getcwd()
    client = GatewayClient(gw.url, gw.password)
    print(f"网关: {gw.url} (pid={gw.pid})")
    print(f"工作目录: {cwd}")

    if args.via == "runs":
        run_id = client.create_run(text, conversation_id=args.session_id)
        print(f"已创建 run: {run_id}")
        if not args.watch:
            return EXIT_SUCCESS
        print("--- 进度 ---")
        conn, resp = client.open_stream(
            "GET",
            f"/api/v1/runs/{run_id}/stream",
            headers={"Accept": "text/event-stream"},
            timeout=None,
        )
        try:
            buf = ""
            while True:
                chunk = resp.read(1)
                if not chunk:
                    break
                buf += chunk.decode("utf-8", errors="replace")
                while "\n\n" in buf:
                    raw, buf = buf.split("\n\n", 1)
                    for line in raw.splitlines():
                        if line.startswith("data:"):
                            print(line[5:].strip(), flush=True)
        except KeyboardInterrupt:
            print("\n已停止跟踪。")
        finally:
            conn.close()
        return EXIT_SUCCESS

    with AcpClient(client) as acp:
        acp.initialize()
        if args.session_id:
            try:
                acp.session_load(args.session_id, cwd=cwd)
            except GatewayError:
                acp.session_new(cwd, session_id=args.session_id)
        else:
            acp.session_new(cwd)
        print(f"会话: {acp.session_id}")
        print("已发送 prompt，等待回复…")
        jsonl = find_jsonl(root, acp.session_id)
        watcher = JsonlWatcher(jsonl) if jsonl else None
        if watcher:
            watcher.seek_end()
        print("--- 进度 ---")
        for event in acp.session_prompt(text):
            if watcher:
                for obj in watcher.poll():
                    line = format_jsonl_event(obj, verbose=args.verbose)
                    if line:
                        print(line, flush=True)
            line = format_acp_event(event, verbose=args.verbose)
            if line:
                print(line, flush=True)
    return EXIT_SUCCESS


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="接管本机 WorkBuddy：列出任务、追加对话、实时跟踪进度。",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  %(prog)s list
  %(prog)s list --all
  %(prog)s status
  %(prog)s watch 7c8b3554-b3d5-4372-8761-51c1769b6523
  %(prog)s reply 7c8b3554 --watch -- 请补上单元测试
  %(prog)s send --cwd /home/ydl/WorkBuddy/x --watch -- 列出当前目录
        """,
    )
    parser.add_argument("-v", "--verbose", action="store_true", help="输出思考过程和更完整内容")
    sub = parser.add_subparsers(dest="command", required=True)

    p_list = sub.add_parser("list", help="列出正在进行（或全部）的任务")
    p_list.add_argument("--all", action="store_true", help="包含历史会话")
    p_list.add_argument("--json", action="store_true", help="以 JSON 输出")
    p_list.set_defaults(func=cmd_list)

    p_status = sub.add_parser("status", help="查看本机网关健康状态和当前会话")
    p_status.add_argument("--json", action="store_true", help="以 JSON 输出")
    p_status.set_defaults(func=cmd_status)

    p_watch = sub.add_parser("watch", help="实时跟踪某个会话的 jsonl 进度")
    p_watch.add_argument("session_id", help="会话 ID，支持前缀")
    p_watch.add_argument("--from-start", action="store_true", help="从文件开头开始打印")
    p_watch.add_argument("--interval", type=float, default=0.3, help="轮询间隔秒数")
    p_watch.add_argument("-v", "--verbose", action="store_true", help="输出思考过程")
    p_watch.set_defaults(func=cmd_watch)

    p_hist = sub.add_parser("history", help="打印会话最近的对话摘要")
    p_hist.add_argument("session_id", help="会话 ID，支持前缀")
    p_hist.add_argument("--tail", type=int, default=80, help="读取末尾多少行 jsonl")
    p_hist.add_argument("-v", "--verbose", action="store_true", help="输出思考过程")
    p_hist.set_defaults(func=cmd_history)

    p_reply = sub.add_parser("reply", help="向已有会话追加一条用户消息")
    p_reply.add_argument("session_id", help="会话 ID，支持前缀")
    p_reply.add_argument("text", nargs="*", help="要追加的内容，也可从标准输入读入")
    p_reply.add_argument("--watch", action="store_true", help="发送后持续打印进度")
    p_reply.add_argument("-v", "--verbose", action="store_true", help="输出思考过程")
    p_reply.set_defaults(func=cmd_reply)

    p_send = sub.add_parser("send", help="在网关上发起新对话（或加载指定会话）")
    p_send.add_argument("text", nargs="*", help="要发送的内容，也可从标准输入读入")
    p_send.add_argument("--cwd", help="工作目录，默认用当前目录或网关目录")
    p_send.add_argument("--session-id", help="指定已有会话 ID，相当于续聊")
    p_send.add_argument("--watch", action="store_true", default=True, help="发送后打印进度（默认开启）")
    p_send.add_argument("--no-watch", dest="watch", action="store_false", help="只发送，不跟踪进度")
    p_send.add_argument("--via", choices=["acp", "runs"], default="acp", help="发送通道")
    p_send.add_argument("-v", "--verbose", action="store_true", help="输出思考过程")
    p_send.set_defaults(func=cmd_send)
    return parser


def main(argv: Optional[List[str]] = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    try:
        return int(args.func(args))
    except GatewayError as exc:
        print(f"错误: {exc}", file=sys.stderr)
        return EXIT_FAILURE
    except KeyboardInterrupt:
        print("\n已中断。", file=sys.stderr)
        return 130


if __name__ == "__main__":
    sys.exit(main())
