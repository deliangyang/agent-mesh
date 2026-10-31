import { reactive, watch } from "vue";
import { meshTarget } from "./target";
import type { FollowItem } from "./types";

type FollowFile = {
  engine?: string;
  session?: string;
  cwd?: string;
  items?: FollowItem[];
};

export const meshFollow = reactive({
  items: [] as FollowItem[],
  error: "",
  sending: false,
});

let seq = 0;
let timer: ReturnType<typeof setInterval> | null = null;

function redirectIfUnauthorized(resp: Response): void {
  if (resp.status === 401) {
    location.href = "/login";
    throw new Error("未登录");
  }
}

function asItems(body: FollowFile | null): FollowItem[] {
  if (!body || !Array.isArray(body.items)) return [];
  return body.items.filter((it) => it && it.id && it.text);
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

export async function loadFollowups(): Promise<void> {
  if (!meshTarget.engine || !meshTarget.session) {
    meshFollow.items = meshFollow.items.filter((it) => it.id.startsWith("local-"));
    return;
  }
  const token = ++seq;
  try {
    const resp = await fetch("/v1/openai/followups?" + query().toString(), {
      credentials: "same-origin",
    });
    redirectIfUnauthorized(resp);
    if (!resp.ok) throw new Error((await resp.text()) || "http " + resp.status);
    const body = (await resp.json()) as FollowFile;
    if (token !== seq) return;
    const remote = asItems(body);
    const locals = meshFollow.items.filter((it) => it.id.startsWith("local-"));
    meshFollow.items = remote.concat(locals);
    meshFollow.error = "";
  } catch (err) {
    if (token !== seq) return;
    meshFollow.error = err instanceof Error ? err.message : String(err);
  }
}

async function applyFile(resp: Response): Promise<void> {
  redirectIfUnauthorized(resp);
  if (!resp.ok) throw new Error((await resp.text()) || "http " + resp.status);
  const body = (await resp.json()) as FollowFile;
  meshFollow.items = asItems(body);
  meshFollow.error = "";
}

async function flushLocals(): Promise<void> {
  if (!meshTarget.engine || !meshTarget.session) return;
  const locals = meshFollow.items.filter((it) => it.id.startsWith("local-"));
  if (!locals.length) return;
  meshFollow.items = meshFollow.items.filter((it) => !it.id.startsWith("local-"));
  for (const it of locals) {
    try {
      const resp = await fetch("/v1/openai/followups", {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload({ text: it.text })),
      });
      await applyFile(resp);
    } catch (err) {
      meshFollow.items = meshFollow.items.concat([it]);
      meshFollow.error = err instanceof Error ? err.message : String(err);
      return;
    }
  }
  await loadFollowups();
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
    const resp = await fetch("/v1/openai/followups", {
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
    const resp = await fetch("/v1/openai/followups?" + q.toString(), {
      method: "DELETE",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload({ id })),
    });
    await applyFile(resp);
  } catch (err) {
    meshFollow.error = err instanceof Error ? err.message : String(err);
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
  // 只有当前轮还在跑时才进追加队列。空闲时若因队列非空也拦截，输入气泡和回复都会丢。
  return isRunning;
}

export function watchFollowups(): void {
  watch(
    () => [meshTarget.node, meshTarget.engine, meshTarget.session] as const,
    async () => {
      await flushLocals();
      await loadFollowups();
    },
    { immediate: true },
  );
  if (timer) clearInterval(timer);
  timer = setInterval(() => {
    if (meshTarget.session) void loadFollowups();
  }, 2000);
}
