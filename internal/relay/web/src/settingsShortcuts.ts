/** Catalog of Mesh UI keyboard shortcuts shown in Settings. */

export type ShortcutScope = "chat" | "settings" | "ask";

export type ShortcutChord =
  | { kind: "mod"; key: string }
  | { kind: "keys"; keys: string[] };

export type ShortcutEntry = {
  id: string;
  scope: ShortcutScope;
  chord: ShortcutChord;
  labelKey: string;
};

export const SETTINGS_SHORTCUTS: ShortcutEntry[] = [
  {
    id: "open-settings",
    scope: "chat",
    chord: { kind: "mod", key: "," },
    labelKey: "settings.scOpenSettings",
  },
  {
    id: "leave-settings",
    scope: "settings",
    chord: { kind: "keys", keys: ["Esc"] },
    labelKey: "settings.scLeaveSettings",
  },
  {
    id: "settings-next",
    scope: "settings",
    chord: { kind: "keys", keys: ["↓", "j"] },
    labelKey: "settings.scSettingsNext",
  },
  {
    id: "settings-prev",
    scope: "settings",
    chord: { kind: "keys", keys: ["↑", "k"] },
    labelKey: "settings.scSettingsPrev",
  },
  {
    id: "dismiss-ask",
    scope: "ask",
    chord: { kind: "keys", keys: ["Esc"] },
    labelKey: "settings.scDismissAsk",
  },
];

/** True when the runtime looks like Apple (⌘ modifier). */
export function prefersMetaMod(ua = "", platform = ""): boolean {
  const hay = `${ua} ${platform}`.toLowerCase();
  return /mac|iphone|ipad|ipod|macintosh/.test(hay);
}

export function modKeyLabel(ua?: string, platform?: string): string {
  const u = ua ?? (typeof navigator !== "undefined" ? navigator.userAgent : "");
  const p = platform ?? (typeof navigator !== "undefined" ? navigator.platform : "");
  return prefersMetaMod(u, p) ? "⌘" : "Ctrl";
}

/** Flatten a chord into display tokens for <kbd> chips. */
export function chordTokens(chord: ShortcutChord, modLabel: string): string[] {
  if (chord.kind === "mod") return [modLabel, chord.key];
  return [...chord.keys];
}

export function shortcutsByScope(scope: ShortcutScope): ShortcutEntry[] {
  return SETTINGS_SHORTCUTS.filter((s) => s.scope === scope);
}
