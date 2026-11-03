import { platformLabel } from "./platform.ts";
import type { CatalogState, NodeInfo } from "./types";

export type MeshLinkSnapshot = {
  state: "connecting" | "live" | "down";
  attempt: number;
};

export type ConnectionNodeRow = {
  id: string;
  name: string;
  platform: string;
  engineCount: number;
};

export type ConnectionDiag = {
  linkState: MeshLinkSnapshot["state"];
  attempt: number;
  nodeCount: number;
  engineCount: number;
  sessionCount: number;
  nodes: ConnectionNodeRow[];
};

function nodeEngineCount(engines: CatalogState["engines"], nodeId: string): number {
  let n = 0;
  const seen: Record<string, boolean> = {};
  for (const e of engines || []) {
    if (!e.available) continue;
    if (e.node_id && e.node_id !== nodeId) continue;
    if (seen[e.name]) continue;
    seen[e.name] = true;
    n += 1;
  }
  return n;
}

export function connectionNodeRows(nodes: NodeInfo[], engines: CatalogState["engines"]): ConnectionNodeRow[] {
  return (nodes || []).map((n) => ({
    id: n.id,
    name: n.name || n.id,
    platform: platformLabel(n),
    engineCount: nodeEngineCount(engines, n.id),
  }));
}

/** Snapshot catalog + live link for the Settings connection panel. */
export function buildConnectionDiag(link: MeshLinkSnapshot, cat: Pick<CatalogState, "nodes" | "engines" | "sessions">): ConnectionDiag {
  const nodes = connectionNodeRows(cat.nodes || [], cat.engines || []);
  return {
    linkState: link.state,
    attempt: Math.max(0, link.attempt | 0),
    nodeCount: nodes.length,
    engineCount: (cat.engines || []).length,
    sessionCount: (cat.sessions || []).length,
    nodes,
  };
}
