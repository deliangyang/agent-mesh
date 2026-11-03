/** Settings page left-nav modules. */

export type SettingsSection =
  | "appearance"
  | "shortcuts"
  | "connection"
  | "platforms"
  | "nodes"
  | "relay"
  | "health"
  | "account";

export type SettingsNavItem = {
  id: SettingsSection;
  labelKey: string;
  hintKey: string;
};

const LAST_KEY = "mesh.settings.section";

export const SETTINGS_NAV: SettingsNavItem[] = [
  { id: "appearance", labelKey: "settings.navAppearance", hintKey: "settings.navAppearanceHint" },
  { id: "shortcuts", labelKey: "settings.navShortcuts", hintKey: "settings.navShortcutsHint" },
  { id: "connection", labelKey: "settings.navConnection", hintKey: "settings.navConnectionHint" },
  { id: "platforms", labelKey: "settings.navPlatforms", hintKey: "settings.navPlatformsHint" },
  { id: "nodes", labelKey: "settings.navNodes", hintKey: "settings.navNodesHint" },
  { id: "relay", labelKey: "settings.navRelay", hintKey: "settings.navRelayHint" },
  { id: "health", labelKey: "settings.navHealth", hintKey: "settings.navHealthHint" },
  { id: "account", labelKey: "settings.navAccount", hintKey: "settings.navAccountHint" },
];

export function resolveSettingsSection(raw: string | undefined | null): SettingsSection {
  const id = (raw || "").trim().toLowerCase();
  if (
    id === "appearance" ||
    id === "shortcuts" ||
    id === "connection" ||
    id === "platforms" ||
    id === "nodes" ||
    id === "relay" ||
    id === "health" ||
    id === "account"
  ) {
    return id;
  }
  return "appearance";
}

export function loadLastSettingsSection(): SettingsSection {
  try {
    return resolveSettingsSection(localStorage.getItem(LAST_KEY));
  } catch {
    return "appearance";
  }
}

export function saveLastSettingsSection(section: SettingsSection): void {
  try {
    localStorage.setItem(LAST_KEY, section);
  } catch {
    /* ignore quota */
  }
}

/** Move one step in the settings nav; wraps at the ends. */
export function adjacentSettingsSection(current: SettingsSection, delta: number): SettingsSection {
  const ids = SETTINGS_NAV.map((n) => n.id);
  const i = Math.max(0, ids.indexOf(current));
  const next = (i + delta + ids.length * 8) % ids.length;
  return ids[next];
}
