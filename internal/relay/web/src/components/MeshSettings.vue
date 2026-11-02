<script setup lang="ts">
import { onMounted, reactive, ref, watch } from "vue";
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

const open = defineModel<boolean>("open", { default: false });

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

async function load(): Promise<void> {
  loading.value = true;
  try {
    const resp = await fetchJSON("/v1/openai/mesh", { credentials: "same-origin" }, 8000);
    if (!resp.ok) throw new Error(t("settings.unavailable"));
    const st = (await resp.json()) as MeshSettingsState;
    form.relay = (st.relays || []).join(", ");
    form.password = st.password || "";
    form.advertise = st.advertise || "";
    linked.value = !!st.linked;
    linkError.value = st.link_error || "";
  } catch (err) {
    toastError(err);
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
    open.value = false;
  } catch (err) {
    toastError(err);
  } finally {
    saving.value = false;
  }
}

watch(open, (v) => {
  if (v) void load();
});

onMounted(() => {
  if (open.value) void load();
});
</script>

<template>
  <div v-if="open" class="mesh-settings-mask" @click.self="open = false">
    <section class="mesh-settings" role="dialog" aria-modal="true" :aria-label="t('settings.title')">
      <header class="mesh-settings-head">
        <div>
          <h2>{{ t("settings.title") }}</h2>
          <p class="mesh-settings-sub">{{ t("settings.sub") }}</p>
        </div>
        <button type="button" class="ghost" :aria-label="t('settings.close')" @click="open = false">{{ t("settings.close") }}</button>
      </header>

      <div v-if="loading" class="mesh-settings-status">{{ t("settings.loading") }}</div>
      <form v-else class="mesh-settings-form" @submit.prevent="save">
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
    </section>
  </div>
</template>
