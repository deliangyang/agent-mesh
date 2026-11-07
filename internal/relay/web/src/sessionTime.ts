/** Compact last-update label for the session list (unix seconds). */
export function formatSessionUpdated(
  unixSec: number,
  opts: { nowMs?: number; locale?: string } = {},
): string {
  if (!unixSec) return "";
  const nowMs = opts.nowMs ?? Date.now();
  const loc = opts.locale || "en";
  const d = new Date(unixSec * 1000);
  const now = new Date(nowMs);
  const todayStart = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const weekStart = todayStart - 6 * 86400000;
  const time = d.toLocaleTimeString(loc, { hour: "2-digit", minute: "2-digit", hour12: false });
  if (unixSec * 1000 >= weekStart) return time;
  if (d.getFullYear() === now.getFullYear()) {
    return d.toLocaleDateString(loc, { month: "numeric", day: "numeric" });
  }
  return d.toLocaleDateString(loc, { year: "numeric", month: "numeric", day: "numeric" });
}
