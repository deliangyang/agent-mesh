import type { CatalogState } from "./types";

export type MeshLinkSnapshot = {
  state: "connecting" | "live" | "down";
  attempt: number;
};

export type ConnectionDiag = {
  linkState: MeshLinkSnapshot["state"];
  attempt: number;
  nodeCount: number;
  engineCount: number;
  sessionCount: number;
};

/** Snapshot catalog + live link for the Settings connection panel. */
export function buildConnectionDiag(link: MeshLinkSnapshot, cat: Pick<CatalogState, "nodes" | "engines" | "sessions">): ConnectionDiag {
  return {
    linkState: link.state,
    attempt: Math.max(0, link.attempt | 0),
    nodeCount: (cat.nodes || []).length,
    engineCount: (cat.engines || []).length,
    sessionCount: (cat.sessions || []).length,
  };
}
