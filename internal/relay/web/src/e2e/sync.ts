import { applyNodeCatalogPart, catalog } from "../catalog";
import { pushLiveChunk } from "../meshAgent";
import { eventToChunk } from "./chunk.ts";
import { e2eClient, hasE2EClient } from "./runtime.ts";
import { isE2EMode } from "./mode.ts";

let reconcileTimer = 0;
let watchAbort: AbortController | null = null;
let watchKey = "";

type Sock = { close: () => void; peerID: string };
const catalogSocks = new Map<string, Sock>();
const opening = new Map<string, Promise<void>>();

/** Keep public node directory in sync and open one E2E catalog WS per node. */
export async function refreshE2ECatalog(): Promise<void> {
  if (!isE2EMode() || !hasE2EClient()) return;
  const c = e2eClient();
  const nodes = await c.nodes();
  catalog.nodes = nodes.map((n) => ({
    id: n.id,
    name: n.name || "",
    os: n.os || "",
    arch: n.arch || "",
  }));

  const want = new Set(nodes.map((n) => n.id));
  for (const [id, sock] of catalogSocks) {
    if (!want.has(id)) {
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
}

async function openCatalogSock(nodeID: string): Promise<void> {
  if (!isE2EMode() || !hasE2EClient()) return;
  try {
    const c = e2eClient();
    const sock = await c.openCatalogWS(nodeID, (msg) => {
      applyNodeCatalogPart(nodeID, msg);
    });
    // Replaced while opening.
    const prev = catalogSocks.get(nodeID);
    if (prev) prev.close();
    catalogSocks.set(nodeID, sock);
  } catch {
    catalogSocks.delete(nodeID);
  }
}

export function startE2ECatalogLoop(): void {
  if (!isE2EMode()) return;
  window.clearInterval(reconcileTimer);
  void refreshE2ECatalog();
  // Reconcile which nodes to subscribe; catalog rows themselves are pushed over WS.
  reconcileTimer = window.setInterval(() => {
    void refreshE2ECatalog();
  }, 20000);
}

export function stopE2ECatalogLoop(): void {
  window.clearInterval(reconcileTimer);
  reconcileTimer = 0;
  for (const sock of catalogSocks.values()) sock.close();
  catalogSocks.clear();
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
