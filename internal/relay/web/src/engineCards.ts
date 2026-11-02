import { t } from "./locale.ts";
import type { EngineRow } from "./types";

export function nodeEngines(rows: EngineRow[], nodeId: string): EngineRow[] {
  if (!nodeId) return [];
  const seen: Record<string, boolean> = {};
  const out: EngineRow[] = [];
  for (const e of rows) {
    if (!e.available) continue;
    if (e.node_id && e.node_id !== nodeId) continue;
    if (seen[e.name]) continue;
    seen[e.name] = true;
    out.push(e);
  }
  return out;
}

/** Registered engines that are not installed / not probeable on this node. */
export function nodeMissingEngines(rows: EngineRow[], nodeId: string): EngineRow[] {
  if (!nodeId) return [];
  const seen: Record<string, boolean> = {};
  const out: EngineRow[] = [];
  for (const e of rows) {
    if (e.available) continue;
    if (e.node_id && e.node_id !== nodeId) continue;
    if (seen[e.name]) continue;
    seen[e.name] = true;
    out.push(e);
  }
  return out;
}

export function engineEmptyHint(nodeId: string): string {
  if (!nodeId) return t("engine.pickNode");
  return t("engine.empty");
}
