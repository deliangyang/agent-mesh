import { t } from "./locale.ts";
import type { EngineCaps, EngineRow } from "./types";

export type { EngineCaps };

/** Caps for the selected engine on a node; defaults to all-on when catalog omits caps. */
export function engineCaps(rows: EngineRow[], nodeId: string, engine: string): EngineCaps {
  if (!engine) return { ask: true, tools: true, diff: true, followups: true };
  for (const e of rows) {
    if (e.name !== engine) continue;
    if (e.node_id && nodeId && e.node_id !== nodeId) continue;
    if (e.caps) return e.caps;
    break;
  }
  return { ask: true, tools: true, diff: true, followups: true };
}

export function capsAllowDiff(caps: EngineCaps): boolean {
  return caps.diff !== false;
}

export function capsAllowFollowups(caps: EngineCaps): boolean {
  return caps.followups !== false;
}

/** Ask is opt-in: only engines that advertise caps.ask. */
export function capsAllowAsk(caps: EngineCaps): boolean {
  return caps.ask === true;
}

/**
 * Mount MeshAskCard when the engine supports ask, or when a live ask prompt
 * already arrived (keep streaming asks visible even if caps omitted ask).
 */
export function showAskChrome(caps: EngineCaps, hasPrompt: boolean): boolean {
  return hasPrompt || capsAllowAsk(caps);
}

/** Compact labels for engine picker / health surfaces. */
export function capsBadges(caps: EngineCaps | undefined): string[] {
  const c = caps || { ask: true, tools: true, diff: true, followups: true };
  const out: string[] = [];
  if (c.tools !== false) out.push("tools");
  if (c.ask) out.push("ask");
  if (c.diff !== false) out.push("diff");
  if (c.followups !== false) out.push("followups");
  return out;
}

/** Localized short label for a caps badge id. */
export function capsBadgeLabel(badge: string): string {
  const key = `caps.${badge}`;
  const label = t(key);
  return label === key ? badge : label;
}

/** Localized tooltip explaining what a capability means. */
export function capsBadgeTitle(badge: string): string {
  const key = `caps.${badge}Hint`;
  const label = t(key);
  return label === key ? capsBadgeLabel(badge) : label;
}
