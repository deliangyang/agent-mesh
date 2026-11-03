<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { createVault, unlockVault, vaultStatus } from "../e2e/vault.ts";
import { afterVaultReady } from "../e2e/runtime.ts";
import { ensureMeshMode, isE2EMode } from "../e2e/mode.ts";
import { fetchJSON } from "../http";
import { loadLastPick, pickToQuery } from "../lastPick";
import { t } from "../locale";
import { toastError } from "../toast";

const password = ref("");
const keyPass = ref("");
const keyPass2 = ref("");
const err = ref("");
const busy = ref(false);
const step = ref<"gate" | "vault">("gate");
const vaultPresent = ref(false);
const router = useRouter();

onMounted(async () => {
  await ensureMeshMode();
  // Cookie may already be valid (Chat sent us here for vault unlock only).
  try {
    const resp = await fetch("/v1/openai/mesh", { credentials: "same-origin" });
    if (resp.status === 401) return;
    if (!isE2EMode()) {
      await goChat();
      return;
    }
    const st = await vaultStatus();
    vaultPresent.value = st.present;
    if (st.unlocked) {
      await goChat();
      return;
    }
    step.value = "vault";
  } catch {
    /* stay on password gate */
  }
});

async function goChat(): Promise<void> {
  const pick = loadLastPick();
  await router.replace(pick ? { path: "/", query: pickToQuery(pick) } : "/");
}

async function submitGate(): Promise<void> {
  err.value = "";
  busy.value = true;
  try {
    const resp = await fetchJSON("/login", {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ password: password.value }),
    });
    if (!resp.ok) {
      err.value = t("login.wrongPassword");
      return;
    }
    await ensureMeshMode();
    if (!isE2EMode()) {
      await goChat();
      return;
    }
    const st = await vaultStatus();
    vaultPresent.value = st.present;
    if (st.unlocked) {
      await goChat();
      return;
    }
    step.value = "vault";
  } catch (e) {
    err.value = t("login.networkError");
    toastError(e);
  } finally {
    busy.value = false;
  }
}

async function submitVault(): Promise<void> {
  err.value = "";
  if (!keyPass.value) {
    err.value = t("login.keyRequired");
    return;
  }
  if (!vaultPresent.value && keyPass.value !== keyPass2.value) {
    err.value = t("login.keyMismatch");
    return;
  }
  busy.value = true;
  try {
    const id = vaultPresent.value ? await unlockVault(keyPass.value) : await createVault(keyPass.value);
    await afterVaultReady(id);
    await goChat();
  } catch (e) {
    err.value = vaultPresent.value ? t("login.keyWrong") : t("login.keyCreateFailed");
    toastError(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="mesh-login">
    <form v-if="step === 'gate'" @submit.prevent="submitGate">
      <div class="mesh-brand">
        <svg class="mesh-mark" viewBox="0 0 32 32" fill="none" aria-hidden="true">
          <path
            d="M16 8 L8.4 21.4 H23.6 Z"
            stroke="currentColor"
            stroke-width="1.6"
            stroke-linejoin="round"
          />
          <circle cx="16" cy="8" r="2.7" fill="currentColor" />
          <circle cx="8.4" cy="21.4" r="2.7" fill="currentColor" />
          <circle cx="23.6" cy="21.4" r="2.7" fill="currentColor" />
        </svg>
        <span class="mesh-brand-word">Mesh</span>
      </div>
      <h1>{{ t("login.title") }}</h1>
      <p class="hint">{{ t("login.hint") }}</p>
      <input v-model="password" type="password" autofocus required :placeholder="t('login.password')" />
      <p v-if="err" class="mesh-err">{{ err }}</p>
      <button type="submit" :disabled="busy">{{ t("login.enter") }}</button>
    </form>

    <form v-else @submit.prevent="submitVault">
      <div class="mesh-brand">
        <span class="mesh-brand-word">Mesh</span>
      </div>
      <h1>{{ vaultPresent ? t("login.keyUnlockTitle") : t("login.keyCreateTitle") }}</h1>
      <p class="hint">{{ vaultPresent ? t("login.keyUnlockHint") : t("login.keyCreateHint") }}</p>
      <input
        v-model="keyPass"
        type="password"
        autofocus
        required
        :placeholder="t('login.keyPassphrase')"
        autocomplete="new-password"
      />
      <input
        v-if="!vaultPresent"
        v-model="keyPass2"
        type="password"
        required
        :placeholder="t('login.keyPassphraseAgain')"
        autocomplete="new-password"
      />
      <p v-if="err" class="mesh-err">{{ err }}</p>
      <button type="submit" :disabled="busy">
        {{ vaultPresent ? t("login.keyUnlock") : t("login.keyCreate") }}
      </button>
    </form>
  </div>
</template>
