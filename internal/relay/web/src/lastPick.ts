const KEY = "mesh.last-pick";

export type LastPick = {
  node?: string;
  engine?: string;
  model?: string;
  dir?: string;
  session?: string;
  all?: boolean;
};

export function saveLastPick(pick: LastPick): void {
  try {
    localStorage.setItem(KEY, JSON.stringify(pick));
  } catch {
    /* ignore quota */
  }
}

export function loadLastPick(): LastPick | null {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return null;
    const obj = JSON.parse(raw) as LastPick;
    if (!obj || typeof obj !== "object") return null;
    return obj;
  } catch {
    return null;
  }
}

export function pickToQuery(pick: LastPick): Record<string, string> {
  const q: Record<string, string> = {};
  if (pick.node) q.node = pick.node;
  if (pick.engine) q.engine = pick.engine;
  if (pick.model) q.model = pick.model;
  if (pick.dir) q.dir = pick.dir;
  if (pick.session) q.session = pick.session;
  if (pick.all) q.all = "1";
  return q;
}
