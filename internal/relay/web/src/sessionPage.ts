import type { Session } from "./types";

export function sortSessions(rows: Session[]): Session[] {
  return (rows || []).slice().sort((a, b) => {
    const ua = a.updated_at || 0;
    const ub = b.updated_at || 0;
    if (ua !== ub) return ub - ua;
    const la = a.live || a.busy ? 1 : 0;
    const lb = b.live || b.busy ? 1 : 0;
    return lb - la;
  });
}

export function mergeSession(prev: Session | undefined, row: Session): Session {
  if (!prev) return row;
  const git = Object.prototype.hasOwnProperty.call(row, "git") ? Boolean(row.git) : prev.git;
  return {
    ...prev,
    ...row,
    title: row.title || prev.title,
    cwd: row.cwd || prev.cwd,
    updated_at: Math.max(row.updated_at || 0, prev.updated_at || 0) || undefined,
    git,
  };
}

export function mergeSessionRows(
  cur: Session[],
  incoming: Session[] | undefined,
  reset: boolean,
  removed: string[],
): Session[] {
  const rows = incoming || [];
  const prev = new Map<string, Session>();
  for (const row of cur) {
    const k = sessionKey(row);
    if (k) prev.set(k, row);
  }
  if (reset) {
    return rows.map((row) => mergeSession(prev.get(sessionKey(row)), row));
  }
  const map = new Map(prev);
  for (const row of rows) {
    const k = sessionKey(row);
    if (k) map.set(k, mergeSession(map.get(k), row));
  }
  for (const id of removed) map.delete(id);
  return Array.from(map.values());
}

/** applySessionIncrement merges one websocket delta and keeps pages already loaded. */
export function applySessionIncrement(cur: Session[], incoming: Session[] | undefined, removed: string[]): Session[] {
  return sortSessions(mergeSessionRows(cur, incoming, false, removed));
}

/** applySessionPage installs an HTTP page. append keeps older rows; a fresh page replaces that engine. */
export function applySessionPage(
  cur: Session[],
  incoming: Session[],
  append: boolean,
  node: string,
  engine: string,
): Session[] {
  const rows = (incoming || []).map((s) => ({
    ...s,
    node_id: s.node_id || node,
    engine: s.engine || engine,
  }));
  if (!append) {
    const keep = cur.filter((s) => (s.node_id || "") !== node || (s.engine || "") !== engine);
    const prev = cur.filter((s) => (s.node_id || "") === node && (s.engine || "") === engine);
    return sortSessions([...keep, ...mergeSessionRows(prev, rows, true, [])]);
  }
  return sortSessions(mergeSessionRows(cur, rows, false, []));
}

function sessionKey(s: Session | null): string {
  if (!s) return "";
  return (s.node_id || "") + "|" + (s.engine || "") + "|" + (s.id || "");
}
