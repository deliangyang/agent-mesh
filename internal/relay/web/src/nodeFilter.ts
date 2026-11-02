import type { NodeInfo } from "./types";

/** Distinct GOOS values present in the catalog (stable order). */
export function nodePlatforms(nodes: NodeInfo[]): string[] {
  const order = ["darwin", "linux", "windows"];
  const seen = new Set<string>();
  for (const n of nodes) {
    if (n.os) seen.add(n.os);
  }
  const out = order.filter((os) => seen.has(os));
  for (const os of seen) {
    if (!out.includes(os)) out.push(os);
  }
  return out;
}

export function filterNodesByOs(nodes: NodeInfo[], os: string): NodeInfo[] {
  if (!os) return nodes;
  return nodes.filter((n) => n.os === os);
}
