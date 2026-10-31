import { reactive } from "vue";
import { applyHistory } from "./meshAgent";
import type { CatalogState, DirRow, EngineRow, MeshEvent, ModelCard, NodeInfo, Session } from "./types";

export const catalog = reactive<CatalogState>({
  nodes: [],
  engines: [],
  models: [],
  dirs: [],
  sessions: [],
});

let ws: WebSocket | null = null;
let watched = "";

function sessionKey(s: Session | null): string {
  if (!s) return "";
  return (s.node_id || "") + "|" + (s.engine || "") + "|" + (s.id || "");
}

function sortSessions(rows: Session[]): Session[] {
  return (rows || [])
    .slice()
    .sort((a, b) => {
      const ua = a.updated_at || 0;
      const ub = b.updated_at || 0;
      if (ua !== ub) return ub - ua;
      const la = a.live || a.busy ? 1 : 0;
      const lb = b.live || b.busy ? 1 : 0;
      return lb - la;
    })
    .slice(0, 20);
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

function applyPart(msg: Record<string, unknown>): void {
  if (!msg || !msg.type) return;
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
      mergeRows(catalog.sessions, msg.sessions as Session[] | undefined, reset, removed, sessionKey),
    );
  }
  if (msg.type === "conversation") {
    const sid = String(msg.session || "");
    const engine = String(msg.engine || "");
    const node = String(msg.node_id || "");
    if (!watched) return;
    const [wNode, wEngine, wSess] = watched.split("|");
    if (sid && wSess && sid !== wSess) return;
    if (engine && wEngine && engine !== wEngine) return;
    if (node && wNode && node !== wNode) return;
    const evs = (msg.events || []) as MeshEvent[];
    if (!evs.length && !msg.reset) return;
    applyHistory(evs, Boolean(msg.reset));
  }
}

export function watchSession(s: Session | null): void {
  const key = sessionKey(s);
  if (key === watched && ws && ws.readyState === WebSocket.OPEN) return;
  watched = key;
  if (!s || !ws || ws.readyState !== WebSocket.OPEN) return;
  ws.send(
    JSON.stringify({
      type: "watch",
      node: s.node_id || "",
      engine: s.engine,
      session: s.id,
    }),
  );
}

export function refreshCatalog(): void {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: "refresh" }));
    return;
  }
  connectCatalog();
}

export function connectCatalog(): void {
  if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
    return;
  }
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  const sock = new WebSocket(proto + "//" + location.host + "/v1/openai/ws");
  sock.onmessage = (ev) => {
    if (!ev.data) return;
    try {
      const msg = JSON.parse(String(ev.data)) as Record<string, unknown>;
      if (msg.type === "chunk" || msg.object === "chat.completion.chunk") return;
      applyPart(msg);
    } catch {
      /* ignore */
    }
  };
  sock.onopen = () => {
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
  };
  sock.onclose = () => {
    ws = null;
    setTimeout(connectCatalog, 1500);
  };
  sock.onerror = () => {};
  ws = sock;
}
