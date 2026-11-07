import { reactive } from "vue";
import { applyCatalogBusy, applyHistory, meshLive, pushLiveChunk } from "./meshAgent";
import { applyFollowups } from "./followup";
import { fetchJSON } from "./http";
import { t } from "./locale";
import { applySessionIncrement, applySessionPage } from "./sessionPage";
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
  if (msg.type === "sessions" && (Array.isArray(msg.sessions) || removed.length)) {
    catalog.sessions = applySessionIncrement(catalog.sessions, msg.sessions as Session[] | undefined, removed);
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
  if (msg.type === "sessions") {
    const incoming = ((msg.sessions as Session[]) || []).map((s) => ({
      ...s,
      node_id: s.node_id || nid,
    }));
    const removed = Array.isArray(msg.removed) ? (msg.removed as string[]) : [];
    catalog.sessions = applySessionIncrement(catalog.sessions, incoming, removed);
    syncWatchedBusy();
    return;
  }
  if (!nid || !msg.reset) {
    applyPart(msg);
    return;
  }
  if (msg.type === "nodes") {
    applyPart({ ...msg, reset: false });
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
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    if (!s) {
      // Unsubscribe: one live session/engine per socket, nothing else streams.
      ws.send(JSON.stringify({ type: "watch", node: "", engine: "", session: "" }));
      return;
    }
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

const SESSION_PAGE = 40;
const readyNodes = new Set<string>();
const sessionMore = new Map<string, boolean>();
const loadedPages: { node: string; engine: string }[] = [];
let paging = false;

function pageMapKey(node: string, engine: string): string {
  return node + "|" + engine;
}

export function sessionPageHasMore(node: string, engine: string): boolean {
  return sessionMore.get(pageMapKey(node, engine)) === true;
}

function rememberPage(node: string, engine: string): void {
  if (!loadedPages.some((p) => p.node === node && p.engine === engine)) {
    loadedPages.push({ node, engine });
  }
}

function pageCursor(node: string, engine: string): { before: number; before_id: string } | null {
  let cursor: Session | null = null;
  for (const s of catalog.sessions) {
    if ((s.node_id || "") !== node || s.engine !== engine) continue;
    if (!cursor) {
      cursor = s;
      continue;
    }
    const su = s.updated_at || 0;
    const cu = cursor.updated_at || 0;
    if (su < cu || (su === cu && s.id < cursor.id)) cursor = s;
  }
  if (!cursor) return null;
  return { before: cursor.updated_at || 0, before_id: cursor.id };
}

function installEngines(nodeID: string, rows: EngineRow[]): void {
  const keep = catalog.engines.filter((e) => (e.node_id || "") !== nodeID);
  catalog.engines = [...keep, ...rows.map((e) => ({ ...e, node_id: e.node_id || nodeID }))];
}

function installModels(nodeID: string, rows: ModelCard[]): void {
  const prefix = nodeID ? nodeID + "/" : "";
  const keep = prefix ? catalog.models.filter((m) => !String(m.id || "").startsWith(prefix)) : [];
  catalog.models = [
    ...keep,
    ...rows.map((m) => {
      const id = String(m.id || "");
      return { ...m, id: prefix && !id.startsWith(prefix) ? prefix + id : id };
    }),
  ];
}

function normalizePage(body: unknown): { sessions: Session[]; more: boolean } {
  if (Array.isArray(body)) return { sessions: body as Session[], more: false };
  const page = (body || {}) as { sessions?: Session[]; more?: boolean };
  return { sessions: page.sessions || [], more: Boolean(page.more) };
}

function installPage(node: string, engine: string, body: unknown, append: boolean): void {
  const page = normalizePage(body);
  catalog.sessions = applySessionPage(catalog.sessions, page.sessions, append, node, engine);
  sessionMore.set(pageMapKey(node, engine), page.more);
  rememberPage(node, engine);
}

async function loadPlainMeta(nodeID: string): Promise<void> {
  const engResp = await fetchJSON("/v1/openai/engines", { credentials: "same-origin" });
  if (!engResp.ok) throw new Error("engines failed");
  installEngines(nodeID, ((await engResp.json()) as EngineRow[]) || []);
  const modelResp = await fetchJSON("/v1/models", { credentials: "same-origin" });
  if (!modelResp.ok) throw new Error("models failed");
  const modelBody = (await modelResp.json()) as { data?: ModelCard[] };
  installModels(nodeID, modelBody.data || []);
  readyNodes.add(nodeID);
}

async function loadE2EMeta(nodeID: string): Promise<void> {
  const { e2eClient } = await import("./e2e/runtime.ts");
  const c = e2eClient();
  const eng = await c.call(nodeID, "engines");
  if (!eng.ok) throw new Error(eng.error || "engines failed");
  installEngines(nodeID, (Array.isArray(eng.body) ? eng.body : []) as EngineRow[]);
  const models = await c.call(nodeID, "models");
  if (!models.ok) throw new Error(models.error || "models failed");
  const list = (Array.isArray(models.body) ? models.body : []) as { id?: string; engine?: string; thinking?: string[] }[];
  installModels(
    nodeID,
    list.map((m) => ({ id: String(m.id || ""), owned_by: m.engine, thinking: m.thinking })),
  );
  readyNodes.add(nodeID);
}

async function fetchPlainPage(node: string, engine: string, append: boolean): Promise<void> {
  const q = new URLSearchParams({ engine, limit: String(SESSION_PAGE), all: "1" });
  if (append) {
    const cursor = pageCursor(node, engine);
    if (!cursor) return;
    q.set("before", String(cursor.before));
    q.set("before_id", cursor.before_id);
  }
  const resp = await fetchJSON("/v1/openai/sessions?" + q.toString(), { credentials: "same-origin" });
  if (!resp.ok) throw new Error("sessions failed");
  installPage(node, engine, await resp.json(), append);
}

async function fetchE2EPage(node: string, engine: string, append: boolean): Promise<void> {
  const { e2eClient } = await import("./e2e/runtime.ts");
  const params: Record<string, unknown> = { engine, limit: SESSION_PAGE, all: true };
  if (append) {
    const cursor = pageCursor(node, engine);
    if (!cursor) return;
    params.before = cursor.before;
    params.before_id = cursor.before_id;
  }
  const resp = await e2eClient().call(node, "list", params);
  if (!resp.ok) throw new Error(resp.error || "list failed");
  installPage(node, engine, resp.body, append);
}

async function ensureNodeMeta(node: string, e2e: boolean): Promise<void> {
  if (!node || readyNodes.has(node)) return;
  if (e2e) await loadE2EMeta(node);
  else await loadPlainMeta(node);
}

export async function loadEngineSessions(node: string, engine: string): Promise<void> {
  if (!node || !engine) return;
  try {
    const { ensureMeshMode, isE2EMode } = await import("./e2e/mode.ts");
    await ensureMeshMode();
    const e2e = isE2EMode();
    await ensureNodeMeta(node, e2e);
    if (e2e) await fetchE2EPage(node, engine, false);
    else await fetchPlainPage(node, engine, false);
  } catch (err) {
    showToast(err instanceof Error ? err.message : String(err), "error");
  }
}

export async function loadMoreSessions(node: string, engine: string): Promise<void> {
  if (paging || !node || !engine || !sessionPageHasMore(node, engine)) return;
  paging = true;
  try {
    const { ensureMeshMode, isE2EMode } = await import("./e2e/mode.ts");
    await ensureMeshMode();
    if (isE2EMode()) await fetchE2EPage(node, engine, true);
    else await fetchPlainPage(node, engine, true);
  } catch (err) {
    showToast(err instanceof Error ? err.message : String(err), "error");
  } finally {
    paging = false;
  }
}

/** bootstrapScreen loads engines, models, and the first session page once. It does not poll. */
export async function bootstrapScreen(node: string, engine: string): Promise<{ node: string; engine: string }> {
  try {
    const { ensureMeshMode, isE2EMode } = await import("./e2e/mode.ts");
    await ensureMeshMode();
    if (isE2EMode()) return await bootstrapE2E(node, engine);
    return await bootstrapPlain(engine);
  } catch (err) {
    showToast(err instanceof Error ? err.message : String(err), "error");
    return { node, engine };
  }
}

async function bootstrapPlain(engine: string): Promise<{ node: string; engine: string }> {
  const infoResp = await fetchJSON("/v1/info", { credentials: "same-origin" });
  if (!infoResp.ok) throw new Error("info failed");
  const info = (await infoResp.json()) as { id?: string; name?: string; os?: string; arch?: string };
  const nodeID = String(info.id || "");
  if (!nodeID) return { node: "", engine };
  catalog.nodes = [{ id: nodeID, name: info.name || "", os: info.os || "", arch: info.arch || "" }];
  await loadPlainMeta(nodeID);
  const engineName = engine || catalog.engines.find((e) => e.node_id === nodeID)?.name || "";
  if (engineName) await fetchPlainPage(nodeID, engineName, false);
  return { node: nodeID, engine: engineName };
}

async function bootstrapE2E(node: string, engine: string): Promise<{ node: string; engine: string }> {
  const { refreshE2ECatalog } = await import("./e2e/sync.ts");
  await refreshE2ECatalog();
  const nodeID = node || catalog.nodes[0]?.id || "";
  if (!nodeID) return { node: "", engine: "" };
  await loadE2EMeta(nodeID);
  const engineName = engine || catalog.engines.find((e) => e.node_id === nodeID)?.name || "";
  if (engineName) await fetchE2EPage(nodeID, engineName, false);
  return { node: nodeID, engine: engineName };
}

export function refreshCatalog(): void {
  void import("./e2e/mode.ts").then(async ({ ensureMeshMode, isE2EMode }) => {
    await ensureMeshMode();
    if (isE2EMode()) {
      const { refreshE2ECatalog } = await import("./e2e/sync.ts");
      await refreshE2ECatalog();
    } else if (!ws || ws.readyState !== WebSocket.OPEN) {
      connectPlainCatalog();
    }
    const pages = loadedPages.slice();
    for (const page of pages) readyNodes.delete(page.node);
    for (const page of pages) await loadEngineSessions(page.node, page.engine);
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
    if (wasDown) window.dispatchEvent(new Event("mesh-link-up"));
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
    if (ws && ws.readyState === WebSocket.OPEN) return;
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
