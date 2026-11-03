/** Plaintext node directory cache for E2E handshakes (pubs come from /v1/nodes). */

import type { NodeRow } from "./client.ts";

const STORE_KEY = "mesh.e2e.nodePeers";

let memory: NodeRow[] = [];

function readStore(): NodeRow[] {
  try {
    const raw = localStorage.getItem(STORE_KEY);
    if (!raw) return [];
    const rows = JSON.parse(raw) as NodeRow[];
    return Array.isArray(rows) ? rows : [];
  } catch {
    return [];
  }
}

function writeStore(rows: NodeRow[]): void {
  try {
    localStorage.setItem(STORE_KEY, JSON.stringify(rows));
  } catch {
    /* quota */
  }
}

/** Replace the cached public node directory (info + pubs; plaintext by design). */
export function saveNodePeers(rows: NodeRow[]): void {
  memory = (rows || []).map((n) => ({ ...n }));
  writeStore(memory);
}

export function listNodePeers(): NodeRow[] {
  if (memory.length) return memory.slice();
  memory = readStore();
  return memory.slice();
}

export function peerByID(id: string): NodeRow | undefined {
  const rows = listNodePeers();
  return rows.find((n) => n.id === id);
}

export function clearNodePeers(): void {
  memory = [];
  try {
    localStorage.removeItem(STORE_KEY);
  } catch {
    /* ignore */
  }
}
