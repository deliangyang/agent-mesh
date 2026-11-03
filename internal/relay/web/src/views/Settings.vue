<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
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
import {
  SETTINGS_NAV,
  loadLastSettingsSection,
  resolveSettingsSection,
  saveLastSettingsSection,
  type SettingsSection,
} from "../settingsNav";
import { loadTheme, toggleTheme, type MeshTheme } from "../theme";

const route = useRoute();
const router = useRouter();
const theme = ref<MeshTheme>(loadTheme());

const section = computed(() => resolveSettingsSection(String(route.params.section || "")));
const diag = computed(() => buildConnectionDiag(meshLink, catalog));

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

function openSection(id: SettingsSection): void {
  saveLastSettingsSection(id);
  void router.replace({ path: `/settings/${id}`, query: route.query });
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
  if (ev.key !== "Escape") return;
  const t = ev.target as HTMLElement | null;
  if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable)) return;
  ev.preventDefault();
  goHome();
}

watch(
  () => route.params.section,
  (raw) => {
    if (!raw) {
      openSection(loadLastSettingsSection());
      return;
    }
    saveLastSettingsSection(resolveSettingsSection(String(raw)));
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
    </header>

    <div class="mesh-settings-layout">
      <nav class="mesh-settings-nav" :aria-label="t('settings.pageTitle')">
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
            <p class="mesh-settings-hotkey-hint">{{ t("settings.hotkeyOpen") }}</p>
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
          </dl>
          <div v-if="diag.nodes.length" class="mesh-conn-nodes" :aria-label="t('settings.connNodeList')">
            <div v-for="n in diag.nodes" :key="n.id" class="mesh-conn-node">
              <div class="mesh-conn-node-name">{{ n.name }}</div>
              <div class="mesh-conn-node-meta">
                <span v-if="n.platform">{{ n.platform }}</span>
                <span>{{ t("settings.connNodeEngines", { count: n.engineCount }) }}</span>
              </div>
            </div>
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

        <MeshSettings v-else-if="section === 'relay'" section="relay" />
        <MeshSettings v-else-if="section === 'health'" section="health" @pick="onHealthPick" />

        <section v-else-if="section === 'account'" class="mesh-settings-panel" :aria-label="t('settings.navAccount')">
          <header class="mesh-settings-panel-head">
            <h2>{{ t("settings.navAccount") }}</h2>
            <p class="mesh-settings-sub">{{ t("settings.navAccountHint") }}</p>
          </header>
          <form method="post" action="/logout" class="mesh-settings-actions-stack">
            <button type="submit" class="mesh-settings-action is-danger">{{ t("nav.signOut") }}</button>
          </form>
        </section>
      </main>
    </div>
  </div>
</template>
