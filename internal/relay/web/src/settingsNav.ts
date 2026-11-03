/** Settings page left-nav modules. */

export type SettingsSection = "appearance" | "relay" | "health" | "account";

export type SettingsNavItem = {
  id: SettingsSection;
  labelKey: string;
  hintKey: string;
};

const LAST_KEY = "mesh.settings.section";

export const SETTINGS_NAV: SettingsNavItem[] = [
  { id: "appearance", labelKey: "settings.navAppearance", hintKey: "settings.navAppearanceHint" },
  { id: "relay", labelKey: "settings.navRelay", hintKey: "settings.navRelayHint" },
  { id: "health", labelKey: "settings.navHealth", hintKey: "settings.navHealthHint" },
  { id: "account", labelKey: "settings.navAccount", hintKey: "settings.navAccountHint" },
];

export function resolveSettingsSection(raw: string | undefined | null): SettingsSection {
  const id = (raw || "").trim().toLowerCase();
  if (id === "appearance" || id === "relay" || id === "health" || id === "account") return id;
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
