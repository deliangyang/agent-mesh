import { reactive } from "vue";
import { applyCatalogBusy, applyHistory, meshLive, pushLiveChunk } from "./meshAgent";
import { applyFollowups } from "./followup";
import { t } from "./locale";
import { showToast } from "./toast";
import type { CatalogState, DirRow, EngineRow, MeshEvent, ModelCard, NodeInfo, Session } from "./types";

export const catalog = reactive<CatalogState>({
  nodes: [],
  engines: [],
  models: [],
  dirs: [],
  sessions: [],
});

export const meshLink = reactive({
  state: "connecting" as "connecting" | "live" | "down",
  attempt: 0,
});

let ws: WebSocket | null = null;
let watched = "";
let reconnectTimer = 0;

function sessionKey(s: Session | null): string {
  if (!s) return "";
  return (s.node_id || "") + "|" + (s.engine || "") + "|" + (s.id || "");
}

const SESSION_CAP_PER_ENGINE = 80;

function sortSessions(rows: Session[]): Session[] {
  const sorted = (rows || []).slice().sort((a, b) => {
    const ua = a.updated_at || 0;
    const ub = b.updated_at || 0;
    if (ua !== ub) return ub - ua;
    const la = a.live || a.busy ? 1 : 0;
    const lb = b.live || b.busy ? 1 : 0;
    return lb - la;
  });
  const n: Record<string, number> = {};
  const out: Session[] = [];
  for (const s of sorted) {
    if (s.live || s.busy) {
      out.push(s);
      continue;
    }
    const eng = s.engine || "";
    if ((n[eng] || 0) >= SESSION_CAP_PER_ENGINE) continue;
    n[eng] = (n[eng] || 0) + 1;
    out.push(s);
  }
  return out;
}

function mergeSession(prev: Session | undefined, row: Session): Session {
  if (!prev) return row;
  return {
    ...prev,
    ...row,
    title: row.title || prev.title,
    cwd: row.cwd || prev.cwd,
    updated_at: Math.max(row.updated_at || 0, prev.updated_at || 0) || undefined,
  };
}

function mergeSessionRows(cur: Session[], incoming: Session[] | undefined, reset: boolean, removed: string[]): Session[] {
  const rows = incoming || [];
  const prev = new Map<string, Session>();
  for (const row of cur) {
    const k = sessionKey(row);
    if (k) prev.set(k, row);
  }
  if (reset) {
    return rows.map((row) => mergeSession(prev.get(sessionKey(row)), row));
  }
  const map = new Map(prev);
  for (const row of rows) {
    const k = sessionKey(row);
    if (k) map.set(k, mergeSession(map.get(k), row));
  }
  for (const id of removed) map.delete(id);
  return Array.from(map.values());
}

function mergeRows<T>(cur: T[], incoming: T[] | undefined, reset: boolean, removed: string[], keyOf: (row: T) => string): T[] {
  const rows = incoming || [];
  if (reset) return rows.slice();
  const map = new Map<string, T>();
  for (const row of cur) {
    const k = keyOf(row);
    if (k) map.set(k, row);
  }
  for (const row of rows) {
    const k = keyOf(row);
    if (k) map.set(k, row);
  }
  for (const id of removed) map.delete(id);
  return Array.from(map.values());
}

function watchedSession(): Session | undefined {
  const parts = watched.split("|");
  const sid = parts.length >= 3 ? parts.slice(2).join("|") : "";
  const want = sid || meshLive.session;
  if (!want) return undefined;
  return catalog.sessions.find((s) => s.id === want);
}

function syncWatchedBusy(): void {
  const hit = watchedSession();
  applyCatalogBusy(Boolean(hit?.busy), hit?.id || meshLive.session || "");
}

function watching(node: string, engine: string, session: string): boolean {
  if (session && meshLive.session && session === meshLive.session) return true;
  if (!watched) return false;
  const [wNode, wEngine, ...rest] = watched.split("|");
  const wSess = rest.join("|");
  if (session && wSess && session !== wSess) return false;
  if (engine && wEngine && engine !== wEngine) return false;
  if (node && wNode && node !== wNode) return false;
  return true;
}

function applyChunk(msg: Record<string, unknown>): void {
  const chunk = ((msg.chunk as Record<string, unknown> | undefined) || msg) as Record<string, unknown>;
  const mesh = (chunk.agent_mesh || chunk.mesh || {}) as {
    session?: string;
    engine?: string;
    node_id?: string;
  };
  if (!watching(String(mesh.node_id || ""), String(mesh.engine || ""), String(mesh.session || ""))) {
    return;
  }
  pushLiveChunk(chunk);
}

function applyPart(msg: Record<string, unknown>): void {
  if (!msg || !msg.type) return;
  if (msg.type === "chunk" || msg.object === "chat.completion.chunk") {
    applyChunk(msg);
    return;
  }
  const reset = Boolean(msg.reset);
  const removed = Array.isArray(msg.removed) ? (msg.removed as string[]) : [];
  if (msg.type === "nodes" && (reset || Array.isArray(msg.nodes) || removed.length)) {
    catalog.nodes = mergeRows(catalog.nodes, msg.nodes as NodeInfo[] | undefined, reset, removed, (n) => n.id);
  }
  if (msg.type === "engines" && (reset || Array.isArray(msg.engines) || removed.length)) {
    catalog.engines = mergeRows(
      catalog.engines,
      msg.engines as EngineRow[] | undefined,
      reset,
      removed,
      (e) => (e.node_id || "") + "\x1f" + e.name,
    );
  }
  if (msg.type === "models" && (reset || Array.isArray(msg.models) || removed.length)) {
    catalog.models = mergeRows(catalog.models, msg.models as ModelCard[] | undefined, reset, removed, (m) => m.id);
  }
  if (msg.type === "dirs" && (reset || Array.isArray(msg.dirs) || removed.length)) {
    catalog.dirs = mergeRows(
      catalog.dirs,
      msg.dirs as DirRow[] | undefined,
      reset,
      removed,
      (d) => (d.node_id || "") + "\x1f" + (d.engine || "") + "\x1f" + d.path,
    );
  }
  if (msg.type === "sessions" && (reset || Array.isArray(msg.sessions) || removed.length)) {
    catalog.sessions = sortSessions(
      mergeSessionRows(catalog.sessions, msg.sessions as Session[] | undefined, reset, removed),
    );
    syncWatchedBusy();
  }
  if (msg.type === "followups" && (reset || Array.isArray(msg.followups) || removed.length)) {
    applyFollowups(msg);
  }
  if (msg.type === "conversation") {
    const sid = String(msg.session || "");
    const engine = String(msg.engine || "");
    const node = String(msg.node_id || "");
    if (!watching(node, engine, sid)) return;
    const evs = (msg.events || []) as MeshEvent[];
    if (!evs.length && !msg.reset) return;
    applyHistory(evs, Boolean(msg.reset));
  }
}

/** Apply one node's E2E catalog part without wiping other nodes' rows on reset. */
export function applyNodeCatalogPart(nodeID: string, msg: Record<string, unknown>): void {
  if (!msg || !msg.type) return;
  const nid = nodeID || String(msg.node_id || "");
  if (!nid || !msg.reset) {
    applyPart(msg);
    return;
  }
  if (msg.type === "nodes") {
    applyPart({ ...msg, reset: false });
    return;
  }
  if (msg.type === "sessions") {
    const keep = catalog.sessions.filter((s) => (s.node_id || "") !== nid);
    const incoming = ((msg.sessions as Session[]) || []).map((s) => ({
      ...s,
      node_id: s.node_id || nid,
    }));
    catalog.sessions = sortSessions([...keep, ...incoming]);
    syncWatchedBusy();
    return;
  }
  if (msg.type === "engines") {
    const keep = catalog.engines.filter((e) => (e.node_id || "") !== nid);
    catalog.engines = [...keep, ...((msg.engines as EngineRow[]) || [])];
    return;
  }
  if (msg.type === "models") {
    const prefix = nid + "/";
    const keep = catalog.models.filter((m) => !String(m.id || "").startsWith(prefix));
    catalog.models = [...keep, ...((msg.models as ModelCard[]) || [])];
    return;
  }
  if (msg.type === "dirs") {
    const keep = catalog.dirs.filter((d) => (d.node_id || "") !== nid);
    catalog.dirs = [...keep, ...((msg.dirs as DirRow[]) || [])];
    return;
  }
  if (msg.type === "followups") {
    applyFollowups({ ...msg, node_id: nid });
    return;
  }
  applyPart(msg);
}

export function watchSession(s: Session | null): void {
  const key = sessionKey(s);
  applyCatalogBusy(Boolean(s?.busy), s?.id || "");
  const same = key === watched;
  watched = key;
  void import("./e2e/mode.ts").then(async ({ isE2EMode }) => {
    if (isE2EMode()) {
      const { startE2EWatch, stopE2EWatch } = await import("./e2e/sync.ts");
      if (!s) {
        stopE2EWatch();
        return;
      }
      startE2EWatch(s.node_id || "", s.engine, s.id);
      return;
    }
    if (same && ws && ws.readyState === WebSocket.OPEN) return;
    if (!s || !ws || ws.readyState !== WebSocket.OPEN) return;
    ws.send(
      JSON.stringify({
        type: "watch",
        node: s.node_id || "",
        engine: s.engine,
        session: s.id,
      }),
    );
  });
}

export function refreshCatalog(): void {
  void import("./e2e/mode.ts").then(async ({ ensureMeshMode, isE2EMode }) => {
    await ensureMeshMode();
    if (isE2EMode()) {
      const { refreshE2ECatalog } = await import("./e2e/sync.ts");
      await refreshE2ECatalog();
      return;
    }
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: "refresh" }));
      return;
    }
    connectCatalog();
  });
}

function resendWatch(sock: WebSocket): void {
  const parts = watched.split("|");
  if (parts.length >= 3 && parts[2]) {
    sock.send(
      JSON.stringify({
        type: "watch",
        node: parts[0],
        engine: parts[1],
        session: parts.slice(2).join("|"),
      }),
    );
  }
}

export function connectCatalog(): void {
  void import("./e2e/mode.ts").then(async ({ ensureMeshMode, isE2EMode }) => {
    await ensureMeshMode();
    if (isE2EMode()) {
      // E2E: never attach plaintext /v1/openai/ws — PublicSnapshot would wipe sessions.
      if (ws) {
        const old = ws;
        ws = null;
        try {
          old.close();
        } catch {
          /* ignore */
        }
      }
      window.clearTimeout(reconnectTimer);
      meshLink.state = meshLink.state === "live" ? "live" : "connecting";
      const { startE2ECatalogLoop } = await import("./e2e/sync.ts");
      startE2ECatalogLoop();
      return;
    }
    connectPlainCatalog();
  });
}

function connectPlainCatalog(): void {
  if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
    return;
  }
  meshLink.state = meshLink.state === "live" ? "live" : "connecting";
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  const sock = new WebSocket(proto + "//" + location.host + "/v1/openai/ws");
  sock.onmessage = (ev) => {
    if (!ev.data) return;
    try {
      const msg = JSON.parse(String(ev.data)) as Record<string, unknown>;
      applyPart(msg);
    } catch {
      /* ignore */
    }
  };
  sock.onopen = () => {
    const wasDown = meshLink.state !== "live";
    meshLink.state = "live";
    meshLink.attempt = 0;
    resendWatch(sock);
    if (wasDown) {
      sock.send(JSON.stringify({ type: "refresh" }));
      window.dispatchEvent(new Event("mesh-link-up"));
    }
  };
  sock.onclose = () => {
    const wasLive = meshLink.state === "live";
    if (ws === sock) ws = null;
    void import("./e2e/mode.ts").then(({ isE2EMode }) => {
      if (isE2EMode()) return;
      meshLink.state = "down";
      meshLink.attempt += 1;
      if (wasLive) showToast(t("link.nodeDown"), "error");
      const delay = Math.min(1200 * Math.max(1, meshLink.attempt), 8000);
      window.clearTimeout(reconnectTimer);
      reconnectTimer = window.setTimeout(connectCatalog, delay);
    });
  };
  sock.onerror = () => {};
  ws = sock;
}

function onBrowserResume(): void {
  void import("./e2e/mode.ts").then(async ({ ensureMeshMode, isE2EMode }) => {
    await ensureMeshMode();
    if (isE2EMode()) {
      const { refreshE2ECatalog } = await import("./e2e/sync.ts");
      await refreshE2ECatalog();
      window.dispatchEvent(new Event("mesh-link-up"));
      return;
    }
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: "refresh" }));
      resendWatch(ws);
      window.dispatchEvent(new Event("mesh-link-up"));
      return;
    }
    connectPlainCatalog();
  });
}

if (typeof window !== "undefined") {
  window.addEventListener("online", onBrowserResume);
  window.addEventListener("offline", () => {
    showToast(t("toast.networkDropped"), "error");
  });
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") onBrowserResume();
  });
}
