<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { catalog, connectCatalog, meshLink, refreshCatalog } from "../catalog";
import MeshSettings from "../components/MeshSettings.vue";
import { buildConnectionDiag } from "../connectionDiag";
import {
  cycleLocale,
  localeButtonLabel,
  localeButtonTitle,
  localePref,
  t,
} from "../locale";
import { clientPlatformLabel } from "../clientPlatform";
import { formatEngineInstall } from "../engineInstall";
import { buildPlatformGroups } from "../platformGroups";
import { showToast, toastError } from "../toast";
import {
  SETTINGS_NAV,
  adjacentSettingsSection,
  loadLastSettingsSection,
  resolveSettingsSection,
  saveLastSettingsSection,
  type SettingsSection,
} from "../settingsNav";
import {
  chordTokens,
  modKeyLabel,
  shortcutsByScope,
  type ShortcutScope,
} from "../settingsShortcuts";
import { loadTheme, toggleTheme, type MeshTheme } from "../theme";

const route = useRoute();
const router = useRouter();
const theme = ref<MeshTheme>(loadTheme());
const navEl = ref<HTMLElement | null>(null);
const modLabel = modKeyLabel();
const clientPlatform = clientPlatformLabel();

const SHORTCUT_SCOPES: { id: ShortcutScope; labelKey: string }[] = [
  { id: "chat", labelKey: "settings.scScopeChat" },
  { id: "settings", labelKey: "settings.scScopeSettings" },
  { id: "ask", labelKey: "settings.scScopeAsk" },
];

const section = computed(() => resolveSettingsSection(String(route.params.section || "")));
const diag = computed(() => buildConnectionDiag(meshLink, catalog));
const platformGroups = computed(() =>
  buildPlatformGroups(catalog.nodes, catalog.engines).map((g) => ({
    ...g,
    installs: g.missingEngines.map((e) => ({
      name: e.name,
      text: formatEngineInstall(e, g.osKey === "unknown" ? "" : g.osKey),
    })),
  })),
);
const sectionTitle = computed(() => {
  const item = SETTINGS_NAV.find((n) => n.id === section.value);
  return item ? t(item.labelKey) : t("settings.pageTitle");
});
const shortcutGroups = computed(() =>
  SHORTCUT_SCOPES.map((scope) => ({
    ...scope,
    items: shortcutsByScope(scope.id).map((entry) => ({
      ...entry,
      tokens: chordTokens(entry.chord, modLabel),
    })),
  })).filter((g) => g.items.length),
);

function scrollActiveNav(): void {
  void nextTick(() => {
    const btn = navEl.value?.querySelector<HTMLElement>(".mesh-settings-nav-item.on");
    btn?.scrollIntoView({ block: "nearest", inline: "nearest" });
  });
}

function linkStateLabel(state: string): string {
  if (state === "live") return t("settings.connLive");
  if (state === "connecting") return t("settings.connConnecting");
  return t("settings.connDown");
}

function reloadPage(): void {
  window.location.reload();
}

function goHome(): void {
  void router.push({ path: "/", query: route.query });
}

function settingsQuery(extra?: Record<string, string | undefined>): Record<string, string> {
  const q: Record<string, string> = {};
  for (const [k, v] of Object.entries(route.query)) {
    if (typeof v === "string" && v) q[k] = v;
  }
  if (extra) {
    for (const [k, v] of Object.entries(extra)) {
      if (v) q[k] = v;
      else delete q[k];
    }
  }
  return q;
}

function openSection(id: SettingsSection, extra?: Record<string, string | undefined>): void {
  saveLastSettingsSection(id);
  void router.replace({ path: `/settings/${id}`, query: settingsQuery(extra) });
}

function openHealthForOs(osKey: string): void {
  openSection("health", { os: osKey === "unknown" ? undefined : osKey });
}

async function copyPlatformInstalls(osLabel: string, lines: string[]): Promise<void> {
  const text = lines.map((s) => s.trim()).filter(Boolean).join("\n");
  if (!text) return;
  try {
    await navigator.clipboard.writeText(text);
    showToast(t("settings.platInstallCopied", { os: osLabel }), "info");
  } catch (err) {
    toastError(err);
  }
}

function flipTheme(): void {
  theme.value = toggleTheme();
}

function onHealthPick(p: { nodeId: string; engine?: string; focusMissing?: boolean }): void {
  const q: Record<string, string> = {};
  for (const [k, v] of Object.entries(route.query)) {
    if (typeof v === "string" && v) q[k] = v;
  }
  q.node = p.nodeId;
  if (p.engine) q.engine = p.engine;
  else delete q.engine;
  if (p.focusMissing) q.hint = "install";
  else delete q.hint;
  void router.push({ path: "/", query: q });
}

function onKey(ev: KeyboardEvent): void {
  const el = ev.target as HTMLElement | null;
  if (el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable)) return;
  if (ev.key === "Escape") {
    ev.preventDefault();
    goHome();
    return;
  }
  if (ev.key === "ArrowDown" || ev.key === "j") {
    ev.preventDefault();
    openSection(adjacentSettingsSection(section.value, 1));
    return;
  }
  if (ev.key === "ArrowUp" || ev.key === "k") {
    ev.preventDefault();
    openSection(adjacentSettingsSection(section.value, -1));
  }
}

watch(
  () => route.params.section,
  (raw) => {
    if (!raw) {
      openSection(loadLastSettingsSection());
      return;
    }
    saveLastSettingsSection(resolveSettingsSection(String(raw)));
    scrollActiveNav();
  },
  { immediate: true },
);

onMounted(() => {
  connectCatalog();
  window.addEventListener("keydown", onKey);
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKey);
});
</script>

<template>
  <div class="mesh-settings-page">
    <header class="mesh-settings-top">
      <button type="button" class="ghost mesh-settings-back" :aria-label="t('settings.backHome')" @click="goHome">
        <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
          <path
            d="M9.8 3.2 5.2 8l4.6 4.8"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
        <span>{{ t("settings.backHome") }}</span>
      </button>
      <h1 class="mesh-settings-page-title">{{ t("settings.pageTitle") }}</h1>
      <p class="mesh-settings-page-section" aria-live="polite">{{ sectionTitle }}</p>
    </header>

    <div class="mesh-settings-layout">
      <nav ref="navEl" class="mesh-settings-nav" :aria-label="t('settings.pageTitle')">
        <button
          v-for="item in SETTINGS_NAV"
          :key="item.id"
          type="button"
          class="mesh-settings-nav-item"
          :class="{ on: section === item.id }"
          :aria-current="section === item.id ? 'page' : undefined"
          @click="openSection(item.id)"
        >
          <span class="mesh-settings-nav-label">{{ t(item.labelKey) }}</span>
          <span class="mesh-settings-nav-hint">{{ t(item.hintKey) }}</span>
        </button>
      </nav>

      <main class="mesh-settings-main">
        <section v-if="section === 'appearance'" class="mesh-settings-panel" :aria-label="t('settings.navAppearance')">
          <header class="mesh-settings-panel-head">
            <h2>{{ t("settings.navAppearance") }}</h2>
            <p class="mesh-settings-sub">{{ t("settings.navAppearanceHint") }}</p>
          </header>
          <div class="mesh-settings-actions-stack">
            <button
              type="button"
              class="mesh-settings-action"
              :title="theme === 'dark' ? t('nav.themeLight') : t('nav.themeDark')"
              @click="flipTheme"
            >
              <svg v-if="theme === 'dark'" viewBox="0 0 16 16" fill="none" aria-hidden="true">
                <circle cx="8" cy="8" r="3.1" stroke="currentColor" stroke-width="1.4" />
                <path
                  d="M8 1.6v1.5M8 12.9v1.5M1.6 8h1.5M12.9 8h1.5M3.3 3.3l1.1 1.1M11.6 11.6l1.1 1.1M3.3 12.7l1.1-1.1M11.6 4.4l1.1-1.1"
                  stroke="currentColor"
                  stroke-width="1.4"
                  stroke-linecap="round"
                />
              </svg>
              <svg v-else viewBox="0 0 16 16" fill="none" aria-hidden="true">
                <path
                  d="M13.2 10.2A5.4 5.4 0 0 1 5.8 2.8 6.1 6.1 0 1 0 13.2 10.2Z"
                  stroke="currentColor"
                  stroke-width="1.4"
                  stroke-linejoin="round"
                />
              </svg>
              <span>{{ theme === "dark" ? t("nav.themeLight") : t("nav.themeDark") }}</span>
            </button>
            <button
              type="button"
              class="mesh-settings-action"
              :title="localeButtonTitle(localePref)"
              @click="cycleLocale"
            >
              <span class="mesh-settings-action-mark">{{ localeButtonLabel(localePref) }}</span>
              <span>{{ localeButtonTitle(localePref) }}</span>
            </button>
            <button type="button" class="mesh-settings-action" @click="openSection('shortcuts')">
              {{ t("settings.hotkeySeeAll") }}
            </button>
          </div>
        </section>

        <section v-else-if="section === 'shortcuts'" class="mesh-settings-panel" :aria-label="t('settings.navShortcuts')">
          <header class="mesh-settings-panel-head">
            <h2>{{ t("settings.navShortcuts") }}</h2>
            <p class="mesh-settings-sub">{{ t("settings.navShortcutsHint") }}</p>
          </header>
          <div class="mesh-sc-list">
            <section v-for="g in shortcutGroups" :key="g.id" class="mesh-sc-group" :aria-label="t(g.labelKey)">
              <h3 class="mesh-sc-scope">{{ t(g.labelKey) }}</h3>
              <ul class="mesh-sc-rows">
                <li v-for="row in g.items" :key="row.id" class="mesh-sc-row">
                  <span class="mesh-sc-label">{{ t(row.labelKey) }}</span>
                  <span class="mesh-sc-keys" aria-hidden="true">
                    <template v-for="(tok, i) in row.tokens" :key="row.id + tok + i">
                      <span v-if="i > 0" class="mesh-sc-sep">{{ row.chord.kind === 'keys' && row.tokens.length > 1 ? '/' : '+' }}</span>
                      <kbd>{{ tok }}</kbd>
                    </template>
                  </span>
                </li>
              </ul>
            </section>
          </div>
        </section>

        <section v-else-if="section === 'connection'" class="mesh-settings-panel" :aria-label="t('settings.navConnection')">
          <header class="mesh-settings-panel-head">
            <h2>{{ t("settings.navConnection") }}</h2>
            <p class="mesh-settings-sub">{{ t("settings.navConnectionHint") }}</p>
          </header>
          <dl class="mesh-conn-grid">
            <div>
              <dt>{{ t("settings.connLink") }}</dt>
              <dd :class="{ ok: diag.linkState === 'live', bad: diag.linkState === 'down' }">
                {{ linkStateLabel(diag.linkState) }}
              </dd>
            </div>
            <div>
              <dt>{{ t("settings.connAttempts") }}</dt>
              <dd>{{ diag.attempt }}</dd>
            </div>
            <div>
              <dt>{{ t("settings.connNodes") }}</dt>
              <dd>{{ diag.nodeCount }}</dd>
            </div>
            <div>
              <dt>{{ t("settings.connEngines") }}</dt>
              <dd>{{ diag.engineCount }}</dd>
            </div>
            <div>
              <dt>{{ t("settings.connSessions") }}</dt>
              <dd>{{ diag.sessionCount }}</dd>
            </div>
            <div>
              <dt>{{ t("settings.connClientOs") }}</dt>
              <dd>{{ clientPlatform }}</dd>
            </div>
          </dl>
          <div v-if="diag.nodes.length" class="mesh-conn-nodes" role="list" :aria-label="t('settings.connNodeList')">
            <button
              v-for="n in diag.nodes"
              :key="n.id"
              type="button"
              class="mesh-conn-node"
              role="listitem"
              :title="n.engineCount ? t('settings.connOpenNode') : t('settings.connOpenInstall')"
              @click="onHealthPick({ nodeId: n.id, focusMissing: n.engineCount === 0 })"
            >
              <div class="mesh-conn-node-name">
                {{ n.name }}
                <span v-if="!n.engineCount" class="mesh-conn-node-miss">{{ t("engine.missing") }}</span>
              </div>
              <div class="mesh-conn-node-meta">
                <span v-if="n.platform">{{ n.platform }}</span>
                <span>{{ t("settings.connNodeEngines", { count: n.engineCount }) }}</span>
              </div>
            </button>
          </div>
          <div class="mesh-settings-actions-stack">
            <button type="button" class="mesh-settings-action" @click="refreshCatalog">
              {{ t("settings.connRefresh") }}
            </button>
            <button type="button" class="mesh-settings-action" @click="reloadPage">
              {{ t("link.reload") }}
            </button>
          </div>
        </section>

        <section v-else-if="section === 'platforms'" class="mesh-settings-panel" :aria-label="t('settings.navPlatforms')">
          <header class="mesh-settings-panel-head">
            <h2>{{ t("settings.navPlatforms") }}</h2>
            <p class="mesh-settings-sub">{{ t("settings.navPlatformsHint") }}</p>
          </header>
          <div v-if="!platformGroups.length" class="mesh-settings-status">{{ t("settings.platEmpty") }}</div>
          <div v-else class="mesh-plat-list" role="list">
            <article v-for="g in platformGroups" :key="g.osKey" class="mesh-plat-group" role="listitem">
              <header class="mesh-plat-head">
                <h3 class="mesh-plat-os">{{ g.label }}</h3>
                <p class="mesh-plat-summary">
                  <span>{{ t("settings.platNodes", { count: g.nodeCount }) }}</span>
                  <span>{{ t("settings.platOk", { count: g.ok }) }}</span>
                  <span v-if="g.miss" class="mesh-plat-miss">{{ t("settings.platMiss", { count: g.miss }) }}</span>
                </p>
              </header>
              <div class="mesh-plat-nodes">
                <button
                  v-for="n in g.nodes"
                  :key="n.id"
                  type="button"
                  class="mesh-plat-node"
                  :title="n.miss ? t('settings.platOpenInstall') : t('settings.platOpenNode')"
                  @click="onHealthPick({ nodeId: n.id, focusMissing: n.miss > 0 })"
                >
                  <span class="mesh-plat-node-name">{{ n.name }}</span>
                  <span class="mesh-plat-node-meta">
                    <span v-if="n.platform">{{ n.platform }}</span>
                    <span>{{ t("settings.platOk", { count: n.ok }) }}</span>
                    <span v-if="n.miss" class="mesh-plat-miss">{{ t("settings.platMiss", { count: n.miss }) }}</span>
                  </span>
                </button>
              </div>
              <div v-if="g.installs.length" class="mesh-plat-installs" :aria-label="t('settings.platInstallTitle', { os: g.label })">
                <div class="mesh-plat-install-bar">
                  <h4 class="mesh-plat-install-title">{{ t("settings.platInstallTitle", { os: g.label }) }}</h4>
                  <button
                    type="button"
                    class="ghost mesh-plat-install-copy"
                    @click="copyPlatformInstalls(g.label, g.installs.map((row) => row.text))"
                  >
                    {{ t("settings.platInstallCopy") }}
                  </button>
                </div>
                <ul class="mesh-plat-install-list">
                  <li v-for="row in g.installs" :key="g.osKey + row.name">{{ row.text }}</li>
                </ul>
              </div>
            </article>
          </div>
          <div class="mesh-settings-actions-stack">
            <button type="button" class="mesh-settings-action" @click="openSection('health', { os: undefined })">
              {{ t("settings.platOpenHealth") }}
            </button>
            <button
              v-for="g in platformGroups.filter((x) => x.nodeCount)"
              :key="'health-' + g.osKey"
              type="button"
              class="mesh-settings-action"
              @click="openHealthForOs(g.osKey)"
            >
              {{ t("settings.platOpenHealthOs", { os: g.label }) }}
            </button>
          </div>
        </section>

        <MeshSettings v-else-if="section === 'relay'" section="relay" />
        <MeshSettings v-else-if="section === 'health'" section="health" @pick="onHealthPick" />

        <section v-else-if="section === 'account'" class="mesh-settings-panel" :aria-label="t('settings.navAccount')">
          <header class="mesh-settings-panel-head">
            <h2>{{ t("settings.navAccount") }}</h2>
            <p class="mesh-settings-sub">{{ t("settings.navAccountHint") }}</p>
          </header>
          <form
            method="post"
            action="/logout"
            class="mesh-settings-actions-stack"
            @submit="
              () => {
                void import('../e2e/runtime.ts').then((m) => m.lockE2E());
              }
            "
          >
            <button type="submit" class="mesh-settings-action is-danger">{{ t("nav.signOut") }}</button>
          </form>
        </section>
      </main>
    </div>
  </div>
</template>
