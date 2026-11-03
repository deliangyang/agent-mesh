import { applyNodeCatalogPart, catalog, meshLink } from "../catalog";
import { pushLiveChunk } from "../meshAgent";
import { eventToChunk } from "./chunk.ts";
import { e2eClient, hasE2EClient } from "./runtime.ts";
import { isE2EMode } from "./mode.ts";
import { saveNodePeers } from "./peers.ts";

let watchAbort: AbortController | null = null;
let watchKey = "";
let resumeBound = false;
let started = false;

type Sock = { close: () => void; peerID: string };
const catalogSocks = new Map<string, Sock>();
const opening = new Map<string, Promise<void>>();
const reconnectTimers = new Map<string, number>();

function markLive(): void {
  meshLink.state = "live";
  meshLink.attempt = 0;
}

function markDown(): void {
  if (catalogSocks.size || opening.size) return;
  meshLink.state = "down";
  meshLink.attempt += 1;
}

function clearReconnect(nodeID: string): void {
  const t = reconnectTimers.get(nodeID);
  if (t) window.clearTimeout(t);
  reconnectTimers.delete(nodeID);
}

function scheduleReconnect(nodeID: string, attempt = 1): void {
  if (!isE2EMode() || !hasE2EClient() || !started) return;
  clearReconnect(nodeID);
  const delay = Math.min(1200 * Math.max(1, attempt), 8000);
  reconnectTimers.set(
    nodeID,
    window.setTimeout(() => {
      reconnectTimers.delete(nodeID);
      if (catalogSocks.has(nodeID) || opening.has(nodeID)) return;
      void openCatalogSock(nodeID, attempt + 1);
    }, delay),
  );
}

/** Fetch public /v1/nodes once, cache pubs, open one E2E catalog WS per node. */
export async function refreshE2ECatalog(): Promise<void> {
  if (!isE2EMode() || !hasE2EClient()) return;
  const c = e2eClient();
  try {
    const nodes = await c.nodes();
    saveNodePeers(nodes);
    catalog.nodes = nodes.map((n) => ({
      id: n.id,
      name: n.name || "",
      os: n.os || "",
      arch: n.arch || "",
    }));
    markLive();

    const want = new Set(nodes.map((n) => n.id));
    for (const [id, sock] of catalogSocks) {
      if (!want.has(id)) {
        clearReconnect(id);
        sock.close();
        catalogSocks.delete(id);
      }
    }
    for (const n of nodes) {
      if (catalogSocks.has(n.id) || opening.has(n.id)) continue;
      const p = openCatalogSock(n.id).finally(() => opening.delete(n.id));
      opening.set(n.id, p);
      void p;
    }
  } catch {
    markDown();
  }
}

async function openCatalogSock(nodeID: string, attempt = 1): Promise<void> {
  if (!isE2EMode() || !hasE2EClient() || !started) return;
  let held: Sock | null = null;
  try {
    const c = e2eClient();
    const sock = await c.openCatalogWS(
      nodeID,
      (msg) => {
        applyNodeCatalogPart(nodeID, msg);
        markLive();
      },
      () => {
        if (held && catalogSocks.get(nodeID) === held) {
          catalogSocks.delete(nodeID);
        }
        markDown();
        scheduleReconnect(nodeID, attempt);
      },
    );
    held = sock;
    clearReconnect(nodeID);
    const prev = catalogSocks.get(nodeID);
    if (prev && prev !== sock) prev.close();
    catalogSocks.set(nodeID, sock);
    markLive();
  } catch {
    catalogSocks.delete(nodeID);
    markDown();
    scheduleReconnect(nodeID, attempt);
  }
}

function onResume(): void {
  if (!isE2EMode() || !started) return;
  void refreshE2ECatalog();
}

/** First full directory + per-node WS; no interval polling. */
export function startE2ECatalogLoop(): void {
  if (!isE2EMode()) return;
  started = true;
  const c = e2eClient();
  c.onSessionDrop = (nodeID) => {
    clearReconnect(nodeID);
    const sock = catalogSocks.get(nodeID);
    if (sock) {
      sock.close();
      catalogSocks.delete(nodeID);
    }
    scheduleReconnect(nodeID, 1);
  };
  if (!resumeBound && typeof window !== "undefined") {
    resumeBound = true;
    window.addEventListener("online", onResume);
    document.addEventListener("visibilitychange", () => {
      if (document.visibilityState === "visible") onResume();
    });
    window.addEventListener("mesh-link-up", onResume);
  }
  meshLink.state = meshLink.state === "live" ? "live" : "connecting";
  void refreshE2ECatalog();
}

export function stopE2ECatalogLoop(): void {
  started = false;
  for (const id of [...reconnectTimers.keys()]) clearReconnect(id);
  for (const sock of catalogSocks.values()) sock.close();
  catalogSocks.clear();
  opening.clear();
}

export function stopE2EWatch(): void {
  watchAbort?.abort();
  watchAbort = null;
  watchKey = "";
}

export function startE2EWatch(node: string, engine: string, session: string): void {
  if (!isE2EMode() || !hasE2EClient() || !session || !engine) return;
  const key = `${node}|${engine}|${session}`;
  if (key === watchKey && watchAbort) return;
  stopE2EWatch();
  watchKey = key;
  const ac = new AbortController();
  watchAbort = ac;
  void (async () => {
    try {
      const c = e2eClient();
      for await (const ev of c.stream(node, "watch", { engine, session_id: session }, ac.signal)) {
        if (ac.signal.aborted) return;
        pushLiveChunk(eventToChunk(ev));
        if (ev.done) break;
      }
    } catch {
      /* aborted or node down */
    }
  })();
}
