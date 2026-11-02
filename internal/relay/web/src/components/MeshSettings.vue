<script setup lang="ts">
import { onMounted, reactive, ref, watch } from "vue";
import { fetchJSON } from "../http";
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
    if (!resp.ok) throw new Error("settings unavailable");
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
      showToast(linkError.value || "Relay unreachable", "error");
      return;
    }
    showToast("Relay reachable", "info");
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
      showToast(linkError.value || "Could not save relay settings", "error");
      return;
    }
    showToast(linked.value ? "Relay saved and linked" : "Relay settings saved", "info");
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
    <section class="mesh-settings" role="dialog" aria-modal="true" aria-label="Node settings">
      <header class="mesh-settings-head">
        <div>
          <h2>Node settings</h2>
          <p class="mesh-settings-sub">Connect this machine to a relay</p>
        </div>
        <button type="button" class="ghost" aria-label="Close settings" @click="open = false">Close</button>
      </header>

      <div v-if="loading" class="mesh-settings-status">Loading…</div>
      <form v-else class="mesh-settings-form" @submit.prevent="save">
        <label>
          <span>Relay URL</span>
          <input
            v-model="form.relay"
            type="url"
            placeholder="https://mesh.example.com"
            autocomplete="off"
            spellcheck="false"
          />
        </label>
        <label>
          <span>Relay password</span>
          <input v-model="form.password" type="password" placeholder="Optional shared password" autocomplete="off" />
        </label>
        <label>
          <span>Advertise URL</span>
          <input
            v-model="form.advertise"
            type="url"
            placeholder="http://LAN-IP:7700 (optional)"
            autocomplete="off"
            spellcheck="false"
          />
        </label>

        <div class="mesh-settings-status" :class="{ ok: linked, bad: !linked && !!linkError }">
          <span v-if="linked">Linked to relay</span>
          <span v-else-if="linkError">{{ linkError }}</span>
          <span v-else>Direct mode (no relay)</span>
        </div>

        <div class="mesh-settings-actions">
          <button type="button" class="ghost" :disabled="testing || saving" @click="testLink">
            {{ testing ? "Testing…" : "Test connection" }}
          </button>
          <button type="submit" class="mesh-settings-save" :disabled="saving || testing">
            {{ saving ? "Saving…" : "Save" }}
          </button>
        </div>
      </form>
    </section>
  </div>
</template>
