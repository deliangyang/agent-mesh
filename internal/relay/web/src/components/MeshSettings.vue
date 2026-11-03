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
import { buildMeshConfigBundle, meshConfigFilename, parseMeshConfigBundle } from "../meshConfigIO";
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
    section?: "relay" | "health" | "nodes";
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
const importEl = ref<HTMLInputElement | null>(null);
const e2e = ref(false);
const keyHint = ref("");
const keyPass = ref("");
const keyNew = ref("");
const keyBusy = ref(false);
const keyMsg = ref("");
const nodeRows = ref<
  {
    id: string;
    name: string;
    os: string;
    arch: string;
    engines: string;
    sign: string;
    x25519: string;
    lastSeen: string;
  }[]
>([]);
const nodesBusy = ref(false);

const fullMatrix = computed(() => buildHealthMatrix(catalog.nodes, catalog.engines));
const osFilter = computed(() => resolveHealthOsFilter(String(route.query.os || "")));
const matrix = computed(() => filterHealthMatrix(fullMatrix.value, osFilter.value));
const osOptions = computed(() => listHealthOsOptions(fullMatrix.value));
const showRelay = computed(() => props.section === "relay");
const showHealth = computed(() => props.section === "health");
const showNodes = computed(() => props.section === "nodes");

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

function encodePub(v: unknown): string {
  if (v == null) return "";
  if (typeof v === "string") return v;
  if (Array.isArray(v)) {
    const bytes = new Uint8Array(v);
    let s = "";
    for (const b of bytes) s += String.fromCharCode(b);
    return btoa(s);
  }
  return "";
}

function fmtSeen(unix?: number): string {
  if (!unix) return "—";
  try {
    return new Date(unix * 1000).toLocaleString();
  } catch {
    return String(unix);
  }
}

async function loadNodes(): Promise<void> {
  nodesBusy.value = true;
  keyMsg.value = "";
  try {
    const { ensureMeshMode, isE2EMode } = await import("../e2e/mode.ts");
    await ensureMeshMode();
    e2e.value = isE2EMode();
    if (e2e.value) {
      const { vaultStatus } = await import("../e2e/vault.ts");
      const st = await vaultStatus();
      keyHint.value = st.idHint || "";
    }
    const resp = await fetchJSON("/v1/nodes", { credentials: "same-origin" }, 8000);
    if (!resp.ok) throw new Error(`nodes http ${resp.status}`);
    const rows = (await resp.json()) as Array<{
      id: string;
      name?: string;
      os?: string;
      arch?: string;
      engines?: string[];
      sign_pub?: unknown;
      x25519_pub?: unknown;
      last_seen?: number;
    }>;
    const { saveNodePeers } = await import("../e2e/peers.ts");
    saveNodePeers(rows);
    nodeRows.value = rows.map((n) => ({
      id: n.id,
      name: n.name || n.id,
      os: n.os || "",
      arch: n.arch || "",
      engines: (n.engines || []).join(", "),
      sign: encodePub(n.sign_pub),
      x25519: encodePub(n.x25519_pub),
      lastSeen: fmtSeen(n.last_seen),
    }));
  } catch (err) {
    toastError(err);
  } finally {
    nodesBusy.value = false;
  }
}

async function copyPub(raw: string): Promise<void> {
  if (!raw) return;
  try {
    await navigator.clipboard.writeText(raw);
    showToast(t("settings.nodesCopied"), "info");
  } catch (err) {
    toastError(err);
  }
}

async function load(): Promise<void> {
  loading.value = true;
  local.value = false;
  e2e.value = false;
  keyMsg.value = "";
  try {
    const { ensureMeshMode, isE2EMode } = await import("../e2e/mode.ts");
    await ensureMeshMode();
    e2e.value = isE2EMode();
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

async function exportKey(): Promise<void> {
  try {
    const { exportUnlockedKeyJSON } = await import("../e2e/vault.ts");
    const raw = exportUnlockedKeyJSON();
    await navigator.clipboard.writeText(raw);
    keyMsg.value = t("settings.keyExported");
  } catch (err) {
    toastError(err);
  }
}

async function importKey(): Promise<void> {
  if (!keyPass.value) {
    keyMsg.value = t("login.keyRequired");
    return;
  }
  keyBusy.value = true;
  try {
    const raw = await navigator.clipboard.readText();
    const { importVaultKeyJSON } = await import("../e2e/vault.ts");
    const { afterVaultReady } = await import("../e2e/runtime.ts");
    const id = await importVaultKeyJSON(raw, keyPass.value);
    await afterVaultReady(id);
    keyHint.value = id.id;
    keyMsg.value = t("settings.keyImported");
  } catch (err) {
    toastError(err);
    keyMsg.value = t("settings.keyImportFailed");
  } finally {
    keyBusy.value = false;
  }
}

async function changeKeyPass(): Promise<void> {
  if (!keyPass.value || !keyNew.value) {
    keyMsg.value = t("login.keyRequired");
    return;
  }
  keyBusy.value = true;
  try {
    const { changeVaultPassphrase } = await import("../e2e/vault.ts");
    await changeVaultPassphrase(keyPass.value, keyNew.value);
    keyPass.value = "";
    keyNew.value = "";
    keyMsg.value = t("settings.keyPassChanged");
  } catch (err) {
    toastError(err);
    keyMsg.value = t("settings.keyPassFailed");
  } finally {
    keyBusy.value = false;
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

function exportConfig(): void {
  const bundle = buildMeshConfigBundle(form);
  const blob = new Blob([JSON.stringify(bundle, null, 2) + "\n"], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = meshConfigFilename();
  a.click();
  URL.revokeObjectURL(url);
  showToast(t("settings.exportDone"), "info");
}

function pickImport(): void {
  importEl.value?.click();
}

async function onImportFile(ev: Event): Promise<void> {
  const input = ev.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file) return;
  try {
    const raw = await file.text();
    const bundle = parseMeshConfigBundle(raw);
    if (!bundle) {
      showToast(t("settings.importInvalid"), "error");
      return;
    }
    const { ensureMeshMode, isE2EMode } = await import("../e2e/mode.ts");
    await ensureMeshMode();
    e2e.value = isE2EMode();
    form.relay = bundle.relay;
    form.password = bundle.password;
    form.advertise = bundle.advertise;
    if (bundle.vault_passphrase) keyPass.value = bundle.vault_passphrase;
    if (bundle.client_key && bundle.vault_passphrase && e2e.value) {
      try {
        const { importVaultKeyJSON } = await import("../e2e/vault.ts");
        const { afterVaultReady } = await import("../e2e/runtime.ts");
        const id = await importVaultKeyJSON(bundle.client_key, bundle.vault_passphrase);
        await afterVaultReady(id);
        keyHint.value = id.id;
        keyMsg.value = t("settings.keyImported");
      } catch (err) {
        toastError(err);
        keyMsg.value = t("settings.keyImportFailed");
      }
    }
    showToast(t("settings.importLoaded"), "info");
  } catch (err) {
    toastError(err);
  }
}

watch(
  () => props.section,
  (sec) => {
    if (sec === "relay") void load();
    if (sec === "nodes") void loadNodes();
  },
);

onMounted(() => {
  if (showRelay.value) void load();
  if (showNodes.value) void loadNodes();
});
</script>

<template>
  <div
    class="mesh-settings-panel"
    :aria-label="showNodes ? t('settings.nodesTitle') : showRelay ? t('settings.title') : t('settings.health')"
  >
    <template v-if="showNodes">
      <header class="mesh-settings-panel-head">
        <h2>{{ t("settings.nodesTitle") }}</h2>
        <p class="mesh-settings-sub">{{ t("settings.nodesSub") }}</p>
      </header>
      <section v-if="e2e" class="mesh-settings-form mesh-settings-pad" :aria-label="t('settings.keyTitle')">
        <h3>{{ t("settings.keyTitle") }}</h3>
        <p class="mesh-settings-sub">{{ t("settings.keySub") }}</p>
        <p v-if="keyHint" class="mesh-settings-status">{{ t("settings.keyId") }}: {{ keyHint }}</p>
        <label>
          <span>{{ t("login.keyPassphrase") }}</span>
          <input v-model="keyPass" type="password" autocomplete="off" />
        </label>
        <label>
          <span>{{ t("settings.keyNewPass") }}</span>
          <input v-model="keyNew" type="password" autocomplete="off" />
        </label>
        <p v-if="keyMsg" class="mesh-settings-status">{{ keyMsg }}</p>
        <div class="mesh-settings-actions">
          <button type="button" class="ghost" :disabled="keyBusy" @click="exportKey">{{ t("settings.keyExport") }}</button>
          <button type="button" class="ghost" :disabled="keyBusy" @click="importKey">{{ t("settings.keyImport") }}</button>
          <button type="button" class="mesh-settings-save" :disabled="keyBusy" @click="changeKeyPass">
            {{ t("settings.keyChangePass") }}
          </button>
        </div>
      </section>
      <div class="mesh-settings-actions-stack mesh-settings-pad">
        <button type="button" class="mesh-settings-action" :disabled="nodesBusy" @click="loadNodes">
          {{ nodesBusy ? t("settings.loading") : t("settings.nodesRefresh") }}
        </button>
      </div>
      <div v-if="!nodeRows.length && !nodesBusy" class="mesh-settings-status mesh-settings-pad">
        {{ t("settings.nodesEmpty") }}
      </div>
      <div v-else class="mesh-nodes-list" role="list" :aria-label="t('settings.nodesTitle')">
        <article v-for="n in nodeRows" :key="n.id" class="mesh-nodes-card" role="listitem">
          <header class="mesh-nodes-card-head">
            <h3>{{ n.name }}</h3>
            <p class="mesh-nodes-meta">
              <span>{{ n.id }}</span>
              <span v-if="n.os || n.arch">{{ [n.os, n.arch].filter(Boolean).join(" / ") }}</span>
            </p>
          </header>
          <dl class="mesh-nodes-dl">
            <div>
              <dt>{{ t("settings.nodesEngines") }}</dt>
              <dd>{{ n.engines || "—" }}</dd>
            </div>
            <div>
              <dt>{{ t("settings.nodesLastSeen") }}</dt>
              <dd>{{ n.lastSeen }}</dd>
            </div>
            <div>
              <dt>{{ t("settings.nodesSignPub") }}</dt>
              <dd>
                <button type="button" class="ghost mesh-nodes-pub" :disabled="!n.sign" @click="copyPub(n.sign)">
                  {{ n.sign || "—" }}
                </button>
              </dd>
            </div>
            <div>
              <dt>{{ t("settings.nodesX25519") }}</dt>
              <dd>
                <button type="button" class="ghost mesh-nodes-pub" :disabled="!n.x25519" @click="copyPub(n.x25519)">
                  {{ n.x25519 || "—" }}
                </button>
              </dd>
            </div>
          </dl>
          <p class="mesh-settings-hotkey-hint">{{ t("settings.nodesCached") }}</p>
        </article>
      </div>
    </template>

    <template v-else-if="showRelay">
      <header class="mesh-settings-panel-head">
        <h2>{{ t("settings.title") }}</h2>
        <p class="mesh-settings-sub">{{ t("settings.sub") }}</p>
      </header>
      <div v-if="loading" class="mesh-settings-status mesh-settings-pad">{{ t("settings.loading") }}</div>
      <template v-else>
        <form v-if="local" class="mesh-settings-form" @submit.prevent="save">
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
          <div class="mesh-settings-actions mesh-settings-io">
            <button type="button" class="ghost" :disabled="testing || saving" @click="exportConfig">
              {{ t("settings.exportConfig") }}
            </button>
            <button type="button" class="ghost" :disabled="testing || saving" @click="pickImport">
              {{ t("settings.importConfig") }}
            </button>
            <input
              ref="importEl"
              type="file"
              accept="application/json,.json"
              class="mesh-settings-file"
              @change="onImportFile"
            />
          </div>
          <p class="mesh-settings-hotkey-hint">{{ t("settings.importHint") }}</p>
        </form>
        <div v-else class="mesh-settings-status mesh-settings-pad">{{ t("settings.relayRemoteOnly") }}</div>
      </template>
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
