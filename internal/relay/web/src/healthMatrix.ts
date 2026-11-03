import { nodeEngines } from "./engineCards.ts";
import { platformLabel } from "./platform.ts";
import type { EngineRow, NodeInfo } from "./types";

export type HealthCell = "ok" | "miss" | "none";

export type HealthMatrixRow = {
  nodeId: string;
  nodeName: string;
  platform: string;
  /** Normalized GOOS bucket (linux/darwin/windows/…/unknown). */
  osKey: string;
  cells: HealthCell[];
};

export type HealthMatrix = {
  /** Display titles for columns (same length as engineNames). */
  titles: string[];
  engineNames: string[];
  rows: HealthMatrixRow[];
};

export type HealthOsOption = {
  osKey: string;
  count: number;
};

const OS_ORDER = ["linux", "darwin", "windows", "freebsd", "openbsd", "netbsd", "unknown"];

const OS_LABEL: Record<string, string> = {
  darwin: "macOS",
  windows: "Windows",
  linux: "Linux",
  freebsd: "FreeBSD",
  openbsd: "OpenBSD",
  netbsd: "NetBSD",
  unknown: "Unknown",
};

function osKeyOf(n: Pick<NodeInfo, "os"> | null | undefined): string {
  const raw = (n?.os || "").trim().toLowerCase();
  return raw || "unknown";
}

/** Display label for an OS filter chip. */
export function healthOsLabel(osKey: string): string {
  return OS_LABEL[osKey] || osKey;
}

/** Normalize ?os= query into a known filter key, or "" for all. */
export function resolveHealthOsFilter(raw: string | undefined | null): string {
  const id = (raw || "").trim().toLowerCase();
  if (!id || id === "all" || id === "*") return "";
  return id;
}

/** Distinct OS keys present in the matrix, stable order. */
export function listHealthOsOptions(matrix: HealthMatrix): HealthOsOption[] {
  const counts = new Map<string, number>();
  for (const row of matrix.rows || []) {
    const key = row.osKey || "unknown";
    counts.set(key, (counts.get(key) || 0) + 1);
  }
  return [...counts.entries()]
    .map(([osKey, count]) => ({ osKey, count }))
    .sort((a, b) => {
      const ia = OS_ORDER.indexOf(a.osKey);
      const ib = OS_ORDER.indexOf(b.osKey);
      const ra = ia >= 0 ? ia : 500;
      const rb = ib >= 0 ? ib : 500;
      if (ra !== rb) return ra - rb;
      return a.osKey.localeCompare(b.osKey);
    });
}

/** Keep rows for one OS; empty osKey returns the full matrix. */
export function filterHealthMatrix(matrix: HealthMatrix, osKey: string): HealthMatrix {
  const key = resolveHealthOsFilter(osKey);
  if (!key) return matrix;
  return {
    titles: matrix.titles,
    engineNames: matrix.engineNames,
    rows: (matrix.rows || []).filter((r) => r.osKey === key),
  };
}

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
      osKey: osKeyOf(n),
      cells,
    };
  });

  return {
    titles: nameOrder.map((n) => titleBy[n] || n),
    engineNames: nameOrder,
    rows,
  };
}

/** Resolve a health-matrix click into concrete node/engine filters. */
export function resolveHealthPick(
  engines: EngineRow[],
  nodeId: string,
  engine?: string,
): { node: string; engine: string } {
  const available = nodeEngines(engines, nodeId);
  if (engine && available.some((e) => e.name === engine)) {
    return { node: nodeId, engine };
  }
  return { node: nodeId, engine: available[0]?.name || "" };
}
