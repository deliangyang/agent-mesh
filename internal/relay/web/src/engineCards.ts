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

export function engineEmptyHint(nodeId: string): string {
  if (!nodeId) return "Pick a node first";
  return "No engines on this node. Install an agent CLI on the machine, then refresh.";
}
