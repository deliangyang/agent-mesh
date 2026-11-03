import { catalog } from "../catalog";
import { applyFollowups } from "../followup";
import { pushLiveChunk } from "../meshAgent";
import type { DirRow, EngineRow, ModelCard, Session } from "../types";
import { eventToChunk } from "./chunk.ts";
import { e2eClient, hasE2EClient } from "./runtime.ts";
import { isE2EMode } from "./mode.ts";

let syncTimer = 0;
let watchAbort: AbortController | null = null;
let watchKey = "";

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

  const engines: EngineRow[] = [];
  const models: ModelCard[] = [];
  const sessions: Session[] = [];
  const dirs: DirRow[] = [];
  const followParts: unknown[] = [];

  for (const n of nodes) {
    try {
      const engResp = await c.call(n.id, "engines");
      const engRows = (Array.isArray(engResp.body) ? engResp.body : []) as EngineRow[];
      for (const e of engRows) {
        engines.push({ ...e, node_id: n.id, node_name: n.name || "" });
      }
    } catch {
      /* node offline */
    }
    try {
      const listResp = await c.call(n.id, "list", { all: true });
      const rows = (Array.isArray(listResp.body) ? listResp.body : []) as Session[];
      for (const s of rows) {
        const row: Session = {
          ...s,
          node_id: s.node_id || n.id,
          node_name: s.node_name || n.name || "",
        };
        sessions.push(row);
        if (row.cwd) {
          dirs.push({
            path: row.cwd,
            engine: row.engine,
            node_id: n.id,
            node_name: n.name || "",
          });
        }
      }
    } catch {
      /* ignore */
    }
    try {
      const modResp = await c.call(n.id, "models", {});
      const rows = (Array.isArray(modResp.body) ? modResp.body : []) as {
        id: string;
        engine?: string;
        name?: string;
        title?: string;
        default?: boolean;
      }[];
      for (const m of rows) {
        const id = m.id.includes("/") ? m.id : `${n.id}/${m.id}`;
        models.push({
          id,
          owned_by: m.engine || "mesh",
        });
      }
    } catch {
      /* ignore */
    }
    try {
      const fu = await c.call(n.id, "followups", { action: "list" });
      if (fu.body) followParts.push(fu.body);
    } catch {
      /* ignore */
    }
  }

  catalog.engines = engines;
  catalog.models = models;
  catalog.sessions = sessions;
  // Dedupe dirs by node|engine|path
  const dirMap = new Map<string, DirRow>();
  for (const d of dirs) {
    const k = `${d.node_id || ""}\x1f${d.engine || ""}\x1f${d.path}`;
    dirMap.set(k, d);
  }
  catalog.dirs = Array.from(dirMap.values());

  for (const body of followParts) {
    applyFollowups({ type: "followups", followups: normalizeFollowups(body), reset: false });
  }
}

function normalizeFollowups(body: unknown): unknown[] {
  if (Array.isArray(body)) return body;
  if (body && typeof body === "object" && Array.isArray((body as { queues?: unknown }).queues)) {
    return (body as { queues: unknown[] }).queues;
  }
  if (body && typeof body === "object" && Array.isArray((body as { items?: unknown }).items)) {
    return [body];
  }
  return [];
}

export function startE2ECatalogLoop(): void {
  if (!isE2EMode()) return;
  window.clearInterval(syncTimer);
  void refreshE2ECatalog();
  syncTimer = window.setInterval(() => {
    void refreshE2ECatalog();
  }, 8000);
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
