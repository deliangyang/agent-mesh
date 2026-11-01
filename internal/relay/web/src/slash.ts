import { ref, watch } from "vue";
import { fetchJSON } from "./http";
import { meshTarget } from "./target";
import { explainError, showToast } from "./toast";
import type { SlashItem } from "./types";

export const slashItems = ref<SlashItem[]>([]);

let seq = 0;

function redirectIfUnauthorized(resp: Response): void {
  if (resp.status === 401) {
    location.href = "/login";
    throw new Error("未登录");
  }
}

export async function loadSlashItems(node: string, engine: string, cwd: string): Promise<void> {
  if (!engine) {
    slashItems.value = [];
    return;
  }
  const token = ++seq;
  const q = new URLSearchParams();
  if (node) q.set("node", node);
  q.set("engine", engine);
  if (cwd) q.set("cwd", cwd);
  try {
    const resp = await fetchJSON("/v1/openai/commands?" + q.toString(), { credentials: "same-origin" });
    redirectIfUnauthorized(resp);
    if (!resp.ok) {
      if (token === seq) slashItems.value = [];
      return;
    }
    const body = (await resp.json()) as { items?: SlashItem[] };
    if (token !== seq) return;
    slashItems.value = Array.isArray(body.items) ? body.items : [];
  } catch (err) {
    if (token === seq) slashItems.value = [];
    const text = explainError(err);
    if (text.includes("网络")) showToast(text, "error");
  }
}

export function watchSlashCatalog(): void {
  watch(
    () => [meshTarget.node, meshTarget.engine, meshTarget.cwd] as const,
    ([node, engine, cwd]) => {
      void loadSlashItems(node, engine, cwd);
    },
    { immediate: true },
  );
  if (typeof window !== "undefined") {
    window.addEventListener("mesh-link-up", () => {
      void loadSlashItems(meshTarget.node, meshTarget.engine, meshTarget.cwd);
    });
  }
}
