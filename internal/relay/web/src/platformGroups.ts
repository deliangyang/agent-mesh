import { platformLabel } from "./platform.ts";
import type { EngineRow, NodeInfo } from "./types";

const OS_ORDER = ["linux", "darwin", "windows", "freebsd", "openbsd", "netbsd"];

const OS_TITLE: Record<string, string> = {
  darwin: "macOS",
  windows: "Windows",
  linux: "Linux",
  freebsd: "FreeBSD",
  openbsd: "OpenBSD",
  netbsd: "NetBSD",
  unknown: "Unknown",
};

export type PlatformNodeRow = {
  id: string;
  name: string;
  arch: string;
  platform: string;
  ok: number;
  miss: number;
};

export type PlatformMissingEngine = {
  name: string;
  title: string;
};

export type PlatformGroup = {
  osKey: string;
  label: string;
  nodeCount: number;
  ok: number;
  miss: number;
  nodes: PlatformNodeRow[];
  /** Distinct unavailable engines across nodes in this OS bucket. */
  missingEngines: PlatformMissingEngine[];
};

function osKeyOf(n: Pick<NodeInfo, "os"> | null | undefined): string {
  const raw = (n?.os || "").trim().toLowerCase();
  return raw || "unknown";
}

function osLabel(key: string): string {
  return OS_TITLE[key] || key;
}

function engineTallies(engines: EngineRow[], nodeId: string): { ok: number; miss: number } {
  let ok = 0;
  let miss = 0;
  const seen: Record<string, boolean> = {};
  for (const e of engines || []) {
    if (!e.name || !e.node_id || e.node_id !== nodeId) continue;
    if (seen[e.name]) continue;
    seen[e.name] = true;
    if (e.available) ok += 1;
    else miss += 1;
  }
  return { ok, miss };
}

function sortOsKeys(a: string, b: string): number {
  const ia = OS_ORDER.indexOf(a);
  const ib = OS_ORDER.indexOf(b);
  const ra = ia >= 0 ? ia : a === "unknown" ? 1000 : 500 + a.localeCompare(b);
  const rb = ib >= 0 ? ib : b === "unknown" ? 1000 : 500 + b.localeCompare(a);
  if (ra !== rb) return ra - rb;
  return a.localeCompare(b);
}

/** Distinct engines reported unavailable on nodes matching osKey. */
export function missingEnginesOnOs(
  nodes: NodeInfo[],
  engines: EngineRow[],
  osKey: string,
): PlatformMissingEngine[] {
  const nodeIds = new Set(
    (nodes || []).filter((n) => osKeyOf(n) === osKey).map((n) => n.id),
  );
  if (!nodeIds.size) return [];
  const byName = new Map<string, PlatformMissingEngine>();
  for (const e of engines || []) {
    if (!e.name || !e.node_id || !nodeIds.has(e.node_id)) continue;
    if (e.available) continue;
    if (byName.has(e.name)) continue;
    byName.set(e.name, { name: e.name, title: e.title || e.name });
  }
  return [...byName.values()].sort((a, b) => a.name.localeCompare(b.name));
}

/** Group catalog nodes by OS for multi-platform coverage in Settings. */
export function buildPlatformGroups(nodes: NodeInfo[], engines: EngineRow[]): PlatformGroup[] {
  const buckets = new Map<string, PlatformNodeRow[]>();
  for (const n of nodes || []) {
    const key = osKeyOf(n);
    const tallies = engineTallies(engines, n.id);
    const row: PlatformNodeRow = {
      id: n.id,
      name: n.name || n.id,
      arch: (n.arch || "").trim(),
      platform: platformLabel(n),
      ok: tallies.ok,
      miss: tallies.miss,
    };
    const list = buckets.get(key);
    if (list) list.push(row);
    else buckets.set(key, [row]);
  }

  const keys = [...buckets.keys()].sort(sortOsKeys);
  return keys.map((key) => {
    const list = buckets.get(key) || [];
    list.sort((a, b) => a.name.localeCompare(b.name));
    let ok = 0;
    let miss = 0;
    for (const row of list) {
      ok += row.ok;
      miss += row.miss;
    }
    return {
      osKey: key,
      label: osLabel(key),
      nodeCount: list.length,
      ok,
      miss,
      nodes: list,
      missingEngines: missingEnginesOnOs(nodes, engines, key),
    };
  });
}
