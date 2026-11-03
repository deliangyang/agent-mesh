<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { catalog } from "../catalog";
import {
  buildHealthMatrix,
  filterHealthMatrix,
  healthOsLabel,
  listHealthOsOptions,
  resolveHealthOsFilter,
} from "../healthMatrix";
import { fetchJSON } from "../http";
import { t } from "../locale";
import { showToast, toastError } from "../toast";

export type MeshSettingsState = {
  relays: string[];
  password: string;
  advertise: string;
  linked: boolean;
  link_error?: string;
  local?: boolean;
};

const props = withDefaults(
  defineProps<{
    /** Which panel to render; parent Settings page switches modules. */
    section?: "relay" | "health";
  }>(),
  { section: "relay" },
);

const emit = defineEmits<{
  pick: [payload: { nodeId: string; engine?: string; focusMissing?: boolean }];
}>();

const route = useRoute();
const router = useRouter();

const form = reactive({
  relay: "",
  password: "",
  advertise: "",
});
const linked = ref(false);
const linkError = ref("");
const loading = ref(false);
const testing = ref(false);
const saving = ref(false);
const local = ref(false);

const fullMatrix = computed(() => buildHealthMatrix(catalog.nodes, catalog.engines));
const osFilter = computed(() => resolveHealthOsFilter(String(route.query.os || "")));
const matrix = computed(() => filterHealthMatrix(fullMatrix.value, osFilter.value));
const osOptions = computed(() => listHealthOsOptions(fullMatrix.value));
const showRelay = computed(() => props.section === "relay");
const showHealth = computed(() => props.section === "health");

function setOsFilter(osKey: string): void {
  const q: Record<string, string> = {};
  for (const [k, v] of Object.entries(route.query)) {
    if (typeof v === "string" && v) q[k] = v;
  }
  const next = resolveHealthOsFilter(osKey);
  if (next) q.os = next;
  else delete q.os;
  void router.replace({ path: route.path, query: q });
}

function cellLabel(cell: string): string {
  if (cell === "ok") return t("settings.healthOk");
  if (cell === "miss") return t("settings.healthMiss");
  return t("settings.healthNone");
}

function pickNodeRow(nodeId: string): void {
  emit("pick", { nodeId });
}

function pickCell(nodeId: string, engine: string, cell: string): void {
  if (cell === "none") {
    pickNodeRow(nodeId);
    return;
  }
  if (cell === "miss") {
    emit("pick", { nodeId, focusMissing: true });
    return;
  }
  emit("pick", { nodeId, engine });
}

async function load(): Promise<void> {
  loading.value = true;
  local.value = false;
  try {
    const resp = await fetchJSON("/v1/openai/mesh", { credentials: "same-origin" }, 8000);
    if (!resp.ok) return;
    const st = (await resp.json()) as MeshSettingsState;
    if (!st.local) return;
    local.value = true;
    form.relay = (st.relays || []).join(", ");
    form.password = st.password || "";
    form.advertise = st.advertise || "";
    linked.value = !!st.linked;
    linkError.value = st.link_error || "";
  } catch {
    /* relay / non-node: health matrix only */
  } finally {
    loading.value = false;
  }
}

function body() {
  return {
    relay: form.relay.trim(),
    password: form.password,
    advertise: form.advertise.trim(),
  };
}

async function testLink(): Promise<void> {
  testing.value = true;
  try {
    const resp = await fetchJSON(
      "/v1/openai/mesh/test",
      {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body()),
      },
      20000,
    );
    const data = await resp.json().catch(() => ({}));
    const mesh = (data.mesh || {}) as MeshSettingsState;
    linked.value = !!mesh.linked;
    linkError.value = mesh.link_error || data.error || "";
    if (!resp.ok || data.ok === false) {
      showToast(linkError.value || t("settings.unreachable"), "error");
      return;
    }
    showToast(t("settings.reachable"), "info");
  } catch (err) {
    toastError(err);
  } finally {
    testing.value = false;
  }
}

async function save(): Promise<void> {
  saving.value = true;
  try {
    const resp = await fetchJSON(
      "/v1/openai/mesh",
      {
        method: "PUT",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body()),
      },
      20000,
    );
    const data = await resp.json().catch(() => ({}));
    const mesh = (data.mesh || {}) as MeshSettingsState;
    if (mesh.relays) form.relay = mesh.relays.join(", ");
    if (typeof mesh.password === "string") form.password = mesh.password;
    if (typeof mesh.advertise === "string") form.advertise = mesh.advertise;
    linked.value = !!mesh.linked;
    linkError.value = mesh.link_error || data.error || "";
    if (!resp.ok || data.ok === false) {
      showToast(linkError.value || t("settings.saveFailed"), "error");
      return;
    }
    showToast(linked.value ? t("settings.savedLinked") : t("settings.saved"), "info");
  } catch (err) {
    toastError(err);
  } finally {
    saving.value = false;
  }
}

watch(
  () => props.section,
  (sec) => {
    if (sec === "relay") void load();
  },
);

onMounted(() => {
  if (showRelay.value) void load();
});
</script>

<template>
  <div class="mesh-settings-panel" :aria-label="showRelay ? t('settings.title') : t('settings.health')">
    <template v-if="showRelay">
      <header class="mesh-settings-panel-head">
        <h2>{{ t("settings.title") }}</h2>
        <p class="mesh-settings-sub">{{ t("settings.sub") }}</p>
      </header>
      <div v-if="loading" class="mesh-settings-status mesh-settings-pad">{{ t("settings.loading") }}</div>
      <form v-else-if="local" class="mesh-settings-form" @submit.prevent="save">
        <label>
          <span>{{ t("settings.relayUrl") }}</span>
          <input
            v-model="form.relay"
            type="url"
            placeholder="https://mesh.example.com"
            autocomplete="off"
            spellcheck="false"
          />
        </label>
        <label>
          <span>{{ t("settings.relayPassword") }}</span>
          <input v-model="form.password" type="password" :placeholder="t('settings.passwordPlaceholder')" autocomplete="off" />
        </label>
        <label>
          <span>{{ t("settings.advertise") }}</span>
          <input
            v-model="form.advertise"
            type="url"
            placeholder="http://LAN-IP:7700 (optional)"
            autocomplete="off"
            spellcheck="false"
          />
        </label>

        <div class="mesh-settings-status" :class="{ ok: linked, bad: !linked && !!linkError }">
          <span v-if="linked">{{ t("settings.linked") }}</span>
          <span v-else-if="linkError">{{ linkError }}</span>
          <span v-else>{{ t("settings.direct") }}</span>
        </div>

        <div class="mesh-settings-actions">
          <button type="button" class="ghost" :disabled="testing || saving" @click="testLink">
            {{ testing ? t("settings.testing") : t("settings.test") }}
          </button>
          <button type="submit" class="mesh-settings-save" :disabled="saving || testing">
            {{ saving ? t("settings.saving") : t("settings.save") }}
          </button>
        </div>
      </form>
      <div v-else class="mesh-settings-status mesh-settings-pad">{{ t("settings.relayRemoteOnly") }}</div>
    </template>

    <section v-else-if="showHealth" class="mesh-health" :aria-label="t('settings.health')">
      <header class="mesh-settings-panel-head">
        <h2>{{ t("settings.health") }}</h2>
        <p class="mesh-settings-sub">{{ t("settings.healthSub") }}</p>
      </header>
      <p class="mesh-health-hint">{{ t("settings.healthHint") }}</p>
      <div v-if="osOptions.length > 1" class="mesh-health-os" role="toolbar" :aria-label="t('settings.healthOsFilter')">
        <button
          type="button"
          class="mesh-health-os-chip"
          :class="{ on: !osFilter }"
          @click="setOsFilter('')"
        >
          {{ t("settings.healthOsAll") }}
        </button>
        <button
          v-for="opt in osOptions"
          :key="opt.osKey"
          type="button"
          class="mesh-health-os-chip"
          :class="{ on: osFilter === opt.osKey }"
          @click="setOsFilter(opt.osKey)"
        >
          {{ healthOsLabel(opt.osKey) }}
          <span class="mesh-health-os-count">{{ opt.count }}</span>
        </button>
      </div>
      <div v-if="!matrix.rows.length" class="mesh-settings-status">
        {{ osFilter ? t("settings.healthOsEmpty", { os: healthOsLabel(osFilter) }) : t("settings.healthEmpty") }}
      </div>
      <div v-else class="mesh-health-scroll">
        <table class="mesh-health-table">
          <thead>
            <tr>
              <th scope="col">{{ t("settings.healthNode") }}</th>
              <th v-for="(title, i) in matrix.titles" :key="matrix.engineNames[i]" scope="col" :title="matrix.engineNames[i]">
                {{ title }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in matrix.rows" :key="row.nodeId">
              <th scope="row">
                <button type="button" class="mesh-health-node" @click="pickNodeRow(row.nodeId)">
                  <span class="mesh-health-name">{{ row.nodeName }}</span>
                  <span v-if="row.platform" class="mesh-health-plat">{{ row.platform }}</span>
                </button>
              </th>
              <td v-for="(cell, i) in row.cells" :key="row.nodeId + matrix.engineNames[i]">
                <button
                  type="button"
                  class="mesh-health-cell"
                  :class="cell"
                  :title="cellLabel(cell)"
                  :aria-label="cellLabel(cell)"
                  @click="pickCell(row.nodeId, matrix.engineNames[i], cell)"
                >
                  {{ cell === "ok" ? "●" : cell === "miss" ? "○" : "–" }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </div>
</template>
