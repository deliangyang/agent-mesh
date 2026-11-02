import { computed, ref } from "vue";
import { en } from "./locales/en.ts";
import { zhCN } from "./locales/zh-CN.ts";

export type MeshLocale = "en" | "zh-CN";
export type MeshLocalePref = "system" | MeshLocale;

const KEY = "mesh.locale";

const catalogs: Record<MeshLocale, Record<string, string>> = {
  en,
  "zh-CN": zhCN,
};

export function detectSystemLocale(lang?: string): MeshLocale {
  // Node / unit tests: no browser window → keep English so catalogs match existing fixtures.
  if (lang === undefined && typeof window === "undefined") return "en";
  const raw = (lang ?? ((typeof navigator !== "undefined" ? navigator.language : "") || "")).toLowerCase();
  if (raw.startsWith("zh")) return "zh-CN";
  return "en";
}

export function loadLocalePref(): MeshLocalePref {
  try {
    const raw = localStorage.getItem(KEY);
    if (raw === "en" || raw === "zh-CN" || raw === "system") return raw;
  } catch {
    /* ignore */
  }
  return "system";
}

export function resolveLocale(pref: MeshLocalePref, lang?: string): MeshLocale {
  if (pref === "en" || pref === "zh-CN") return pref;
  return detectSystemLocale(lang);
}

export const localePref = ref<MeshLocalePref>(loadLocalePref());
export const locale = ref<MeshLocale>(resolveLocale(localePref.value));

function syncDocument(next: MeshLocale): void {
  if (typeof document === "undefined") return;
  document.documentElement.lang = next === "zh-CN" ? "zh-CN" : "en";
  try {
    document.title = next === "zh-CN" ? "聊天 · Agent Mesh" : "Chat · Agent Mesh";
  } catch {
    /* ignore */
  }
}

export function applyLocalePref(pref: MeshLocalePref): void {
  localePref.value = pref;
  locale.value = resolveLocale(pref);
  try {
    localStorage.setItem(KEY, pref);
  } catch {
    /* ignore */
  }
  syncDocument(locale.value);
}

/** Apply stored / system preference at startup. */
export function applyLocale(): MeshLocale {
  applyLocalePref(loadLocalePref());
  return locale.value;
}

export function cycleLocale(): MeshLocalePref {
  const order: MeshLocalePref[] = ["system", "en", "zh-CN"];
  const i = order.indexOf(localePref.value);
  const next = order[(i + 1) % order.length];
  applyLocalePref(next);
  return next;
}

export function localeButtonLabel(pref: MeshLocalePref = localePref.value): string {
  if (pref === "zh-CN") return "中文";
  if (pref === "en") return "EN";
  return "Auto";
}

export function localeButtonTitle(pref: MeshLocalePref = localePref.value): string {
  void locale.value;
  if (pref === "system") return t("locale.followSystem");
  if (pref === "zh-CN") return t("locale.chinese");
  return t("locale.english");
}

export function t(key: string, params?: Record<string, string | number>): string {
  const loc = locale.value;
  const cat = catalogs[loc] || catalogs.en;
  let s = cat[key] ?? catalogs.en[key] ?? key;
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      s = s.split("{" + k + "}").join(String(v));
    }
  }
  return s;
}

/** Reactive helper for Vue templates / computed. */
export function useLocale() {
  return {
    locale: computed(() => locale.value),
    localePref: computed(() => localePref.value),
    t,
    cycleLocale,
    applyLocalePref,
    localeButtonLabel,
    localeButtonTitle,
  };
}

if (typeof window !== "undefined") {
  window.addEventListener("languagechange", () => {
    if (localePref.value !== "system") return;
    locale.value = detectSystemLocale();
    syncDocument(locale.value);
  });
}
