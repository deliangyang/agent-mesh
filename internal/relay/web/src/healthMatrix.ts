import { platformLabel } from "./platform.ts";
import type { EngineRow, NodeInfo } from "./types";

export type HealthCell = "ok" | "miss" | "none";

export type HealthMatrixRow = {
  nodeId: string;
  nodeName: string;
  platform: string;
  cells: HealthCell[];
};

export type HealthMatrix = {
  /** Display titles for columns (same length as engineNames). */
  titles: string[];
  engineNames: string[];
  rows: HealthMatrixRow[];
};

/** Build a nodes × engines availability matrix from the live catalog. */
export function buildHealthMatrix(nodes: NodeInfo[], engines: EngineRow[]): HealthMatrix {
  const nameOrder: string[] = [];
  const titleBy: Record<string, string> = {};
  for (const e of engines) {
    if (!e.name) continue;
    if (!titleBy[e.name]) {
      nameOrder.push(e.name);
      titleBy[e.name] = e.title || e.name;
    }
  }
  nameOrder.sort((a, b) => a.localeCompare(b));

  const byNode = new Map<string, Map<string, boolean>>();
  for (const e of engines) {
    if (!e.name || !e.node_id) continue;
    let m = byNode.get(e.node_id);
    if (!m) {
      m = new Map();
      byNode.set(e.node_id, m);
    }
    m.set(e.name, !!e.available);
  }

  const rows: HealthMatrixRow[] = nodes.map((n) => {
    const avail = byNode.get(n.id);
    const cells: HealthCell[] = nameOrder.map((name) => {
      if (!avail || !avail.has(name)) return "none";
      return avail.get(name) ? "ok" : "miss";
    });
    return {
      nodeId: n.id,
      nodeName: n.name || n.id,
      platform: platformLabel(n),
      cells,
    };
  });

  return {
    titles: nameOrder.map((n) => titleBy[n] || n),
    engineNames: nameOrder,
    rows,
  };
}
