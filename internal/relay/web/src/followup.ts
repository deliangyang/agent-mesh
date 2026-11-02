import { reactive, watch } from "vue";
import { fetchJSON } from "./http";
import { meshTarget } from "./target";
import { toastError } from "./toast";
import type { FollowItem, FollowQueue } from "./types";

type FollowFile = FollowQueue;

export const meshFollow = reactive({
  items: [] as FollowItem[],
  error: "",
  sending: false,
});

let seq = 0;
const queues = new Map<string, FollowItem[]>();

function redirectIfUnauthorized(resp: Response): void {
  if (resp.status === 401) {
    location.href = "/login";
    throw new Error("not signed in");
  }
}

function asItems(body: FollowFile | null): FollowItem[] {
  if (!body || !Array.isArray(body.items)) return [];
  return body.items.filter((it) => it && it.id && it.text);
}

function queueKey(node?: string, engine?: string, session?: string): string {
  return (node || "") + "|" + (engine || "") + "|" + (session || "");
}

function currentKey(): string {
  if (!meshTarget.engine || !meshTarget.session) return "";
  return queueKey(meshTarget.node, meshTarget.engine, meshTarget.session);
}

function localsOf(): FollowItem[] {
  return meshFollow.items.filter((it) => it.id.startsWith("local-"));
}

function paint(): void {
  const locals = localsOf();
  const key = currentKey();
  if (!key) {
    meshFollow.items = locals;
    return;
  }
  meshFollow.items = (queues.get(key) || []).concat(locals);
}

function remember(body: FollowFile | null): FollowItem[] {
  const items = asItems(body);
  const engine = body?.engine || meshTarget.engine;
  const session = body?.session || meshTarget.session;
  if (engine && session) {
    queues.set(queueKey(body?.node_id || meshTarget.node, engine, session), items);
  }
  return items;
}

function query(): URLSearchParams {
  const q = new URLSearchParams();
  if (meshTarget.node) q.set("node", meshTarget.node);
  if (meshTarget.engine) q.set("engine", meshTarget.engine);
  if (meshTarget.session) q.set("session", meshTarget.session);
  if (meshTarget.cwd) q.set("cwd", meshTarget.cwd);
  return q;
}

function payload(extra: Record<string, string> = {}): Record<string, string> {
  return {
    node: meshTarget.node,
    engine: meshTarget.engine,
    session: meshTarget.session,
    cwd: meshTarget.cwd,
    ...extra,
  };
}

function localId(): string {
  return "local-" + Math.random().toString(36).slice(2, 10);
}

export function applyFollowups(msg: Record<string, unknown>): void {
  const reset = Boolean(msg.reset);
  const removed = Array.isArray(msg.removed) ? (msg.removed as string[]) : [];
  const rows = Array.isArray(msg.followups) ? (msg.followups as FollowQueue[]) : [];
  if (reset) queues.clear();
  for (const row of rows) {
    const k = queueKey(row.node_id, row.engine, row.session);
    if (k !== "||") queues.set(k, asItems(row));
  }
  for (const id of removed) queues.delete(id);
  paint();
}

export async function loadFollowups(): Promise<void> {
  if (!meshTarget.engine || !meshTarget.session) {
    paint();
    return;
  }
  const token = ++seq;
  try {
    const resp = await fetchJSON("/v1/openai/followups?" + query().toString(), {
      credentials: "same-origin",
    });
    redirectIfUnauthorized(resp);
    if (!resp.ok) throw new Error((await resp.text()) || "http " + resp.status);
    const body = (await resp.json()) as FollowFile;
    if (token !== seq) return;
    remember(body);
    paint();
    meshFollow.error = "";
  } catch (err) {
    if (token !== seq) return;
    meshFollow.error = err instanceof Error ? err.message : String(err);
    toastError(err);
  }
}

async function applyFile(resp: Response): Promise<void> {
  redirectIfUnauthorized(resp);
  if (!resp.ok) throw new Error((await resp.text()) || "http " + resp.status);
  const body = (await resp.json()) as FollowFile;
  remember(body);
  paint();
  meshFollow.error = "";
}

async function flushLocals(): Promise<void> {
  if (!meshTarget.engine || !meshTarget.session) return;
  const locals = localsOf();
  if (!locals.length) return;
  meshFollow.items = meshFollow.items.filter((it) => !it.id.startsWith("local-"));
  for (const it of locals) {
    try {
      const resp = await fetchJSON("/v1/openai/followups", {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload({ text: it.text })),
      });
      await applyFile(resp);
    } catch (err) {
      meshFollow.items = meshFollow.items.concat([it]);
      meshFollow.error = err instanceof Error ? err.message : String(err);
      toastError(err);
      return;
    }
  }
}

export async function pushFollowup(text: string): Promise<void> {
  const value = text.trim();
  if (!value) return;
  meshFollow.sending = true;
  meshFollow.error = "";
  try {
    if (!meshTarget.session) {
      meshFollow.items = meshFollow.items.concat([
        { id: localId(), text: value, created: Math.floor(Date.now() / 1000) },
      ]);
      return;
    }
    const resp = await fetchJSON("/v1/openai/followups", {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload({ text: value })),
    });
    await applyFile(resp);
  } catch (err) {
    meshFollow.error = err instanceof Error ? err.message : String(err);
    meshFollow.items = meshFollow.items.concat([
      { id: localId(), text: value, created: Math.floor(Date.now() / 1000) },
    ]);
    toastError(err);
  } finally {
    meshFollow.sending = false;
  }
}

export async function dropFollowup(id: string): Promise<void> {
  if (!id) return;
  if (id.startsWith("local-") || !meshTarget.session) {
    meshFollow.items = meshFollow.items.filter((it) => it.id !== id);
    return;
  }
  try {
    const q = query();
    q.set("id", id);
    const resp = await fetchJSON("/v1/openai/followups?" + q.toString(), {
      method: "DELETE",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload({ id })),
    });
    await applyFile(resp);
  } catch (err) {
    meshFollow.error = err instanceof Error ? err.message : String(err);
    toastError(err);
  }
}

export async function pullLastFollowup(): Promise<string> {
  const items = meshFollow.items;
  const last = items.length ? items[items.length - 1] : null;
  if (!last) return "";
  await dropFollowup(last.id);
  return last.text;
}

export function shouldQueueFollowup(isRunning: boolean): boolean {
  // queue a follow-up only while the current turn is running. Blocking while idle because the queue is non-empty drops both the bubble and the reply.
  return isRunning;
}

export function watchFollowups(): void {
  watch(
    () => [meshTarget.node, meshTarget.engine, meshTarget.session] as const,
    async () => {
      await flushLocals();
      paint();
      await loadFollowups();
    },
    { immediate: true },
  );
  if (typeof window === "undefined") return;
  window.addEventListener("mesh-link-up", () => {
    void loadFollowups();
  });
}
