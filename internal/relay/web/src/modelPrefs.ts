import type { ModelCard } from "./types";

export type ModelPref = {
  /** Model name without the node/engine prefix, so it applies on every node. */
  model?: string;
  /** Thinking level accepted by the chosen model; empty = off/unsupported. */
  thinking?: string;
};

export type ResolvedPref = { model: string; thinking: string; thinkingLevels: string[] };

const KEY = "mesh.modelPrefs";

export function loadModelPrefs(): Record<string, ModelPref> {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return {};
    const obj = JSON.parse(raw) as Record<string, ModelPref>;
    return obj && typeof obj === "object" ? obj : {};
  } catch {
    return {};
  }
}

export function saveModelPref(engine: string, pref: ModelPref): void {
  if (!engine) return;
  try {
    const all = loadModelPrefs();
    const next: ModelPref = { model: (pref.model || "").trim(), thinking: (pref.thinking || "").trim() };
    if (!next.model) delete next.model;
    if (!next.thinking) delete next.thinking;
    if (next.model || next.thinking) all[engine] = next;
    else delete all[engine];
    localStorage.setItem(KEY, JSON.stringify(all));
  } catch {
    /* ignore quota */
  }
}

/** modelName drops the node/engine prefix from a catalog model id. */
export function modelName(id: string, engine: string): string {
  const marker = "/" + engine + "/";
  const i = String(id || "").indexOf(marker);
  if (i >= 0) return String(id).slice(i + marker.length);
  const prefix = engine + "/";
  if (engine && String(id || "").startsWith(prefix)) return String(id).slice(prefix.length);
  return String(id || "");
}

export function modelsForEngine(models: ModelCard[], engine: string, node = ""): ModelCard[] {
  return (models || []).filter((m) => {
    if (m.owned_by && m.owned_by !== engine) return false;
    if (!m.owned_by && !String(m.id).includes("/" + engine + "/")) return false;
    if (node && !String(m.id).startsWith(node + "/")) return false;
    return true;
  });
}

/** resolveModelPref picks the stored model for one engine, falling back to the first available. */
export function resolveModelPref(
  engine: string,
  models: ModelCard[],
  node = "",
): ResolvedPref {
  const list = modelsForEngine(models, engine, node);
  const pref = loadModelPrefs()[engine] || {};
  const hit =
    (pref.model && list.find((m) => modelName(m.id, engine) === pref.model)) || list[0];
  const levels = (hit?.thinking || []).slice();
  const thinking = pref.thinking && levels.includes(pref.thinking) ? pref.thinking : "";
  return { model: hit?.id || "", thinking, thinkingLevels: levels };
}

export function thinkingForEngine(engine: string): string {
  return loadModelPrefs()[engine]?.thinking || "";
}
