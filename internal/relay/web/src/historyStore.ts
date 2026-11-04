import type { MeshEvent } from "./types";

/** Bounded localStorage mirror of the per-thread transcript cache. */

const PREFIX = "mesh.history.";
const INDEX_KEY = "mesh.history.index";
const MAX_THREADS = 12;
const MAX_EVENTS = 300;
const MAX_BYTES = 3 * 1024 * 1024;

type IndexRow = { id: string; bytes: number; at: number };

function readIndex(): IndexRow[] {
  try {
    const raw = localStorage.getItem(INDEX_KEY);
    const rows = raw ? (JSON.parse(raw) as IndexRow[]) : [];
    return Array.isArray(rows) ? rows.filter((r) => r && typeof r.id === "string") : [];
  } catch {
    return [];
  }
}

function writeIndex(rows: IndexRow[]): void {
  try {
    localStorage.setItem(INDEX_KEY, JSON.stringify(rows));
  } catch {
    /* ignore quota */
  }
}

function key(threadId: string): string {
  return PREFIX + threadId;
}

function drop(threadId: string): void {
  try {
    localStorage.removeItem(key(threadId));
  } catch {
    /* ignore */
  }
}

function touchIndex(threadId: string, bytes: number): void {
  const now = Date.now();
  const rows = readIndex().filter((r) => r.id !== threadId);
  rows.unshift({ id: threadId, bytes, at: now });
  let total = rows.reduce((n, r) => n + r.bytes, 0);
  while (rows.length > MAX_THREADS || (total > MAX_BYTES && rows.length > 1)) {
    const old = rows.pop();
    if (!old) break;
    total -= old.bytes;
    drop(old.id);
  }
  writeIndex(rows);
}

/** loadStoredHistory returns the last painted transcript for a thread, if any. */
export function loadStoredHistory(threadId: string): MeshEvent[] | null {
  if (!threadId) return null;
  try {
    const raw = localStorage.getItem(key(threadId));
    if (!raw) return null;
    const rows = JSON.parse(raw) as MeshEvent[];
    if (!Array.isArray(rows)) return null;
    touchIndex(threadId, raw.length);
    return rows;
  } catch {
    return null;
  }
}

/** saveStoredHistory mirrors a bounded tail of the transcript for instant repaint. */
export function saveStoredHistory(threadId: string, events: MeshEvent[]): void {
  if (!threadId || !events || !events.length) return;
  const rows = events.slice(-MAX_EVENTS);
  try {
    const raw = JSON.stringify(rows);
    if (raw.length > MAX_BYTES / 2) {
      drop(threadId);
      return;
    }
    localStorage.setItem(key(threadId), raw);
    touchIndex(threadId, raw.length);
  } catch {
    // Quota: drop the oldest half and retry once.
    const rowsIndex = readIndex();
    for (const row of rowsIndex.slice(Math.floor(rowsIndex.length / 2))) drop(row.id);
    writeIndex(rowsIndex.slice(0, Math.floor(rowsIndex.length / 2)));
    try {
      localStorage.setItem(key(threadId), JSON.stringify(rows));
      touchIndex(threadId, JSON.stringify(rows).length);
    } catch {
      /* ignore */
    }
  }
}

export function forgetStoredHistory(threadId: string): void {
  if (!threadId) return;
  drop(threadId);
  writeIndex(readIndex().filter((r) => r.id !== threadId));
}
