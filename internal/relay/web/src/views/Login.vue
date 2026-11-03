<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";
import { fetchJSON } from "../http";
import { loadLastPick, pickToQuery } from "../lastPick";
import { t } from "../locale";
import { toastError } from "../toast";

const password = ref("");
const err = ref("");
const busy = ref(false);
const router = useRouter();

async function submit(): Promise<void> {
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
    const pick = loadLastPick();
    await router.replace(pick ? { path: "/", query: pickToQuery(pick) } : "/");
  } catch (e) {
    err.value = t("login.networkError");
    toastError(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="mesh-login">
    <form @submit.prevent="submit">
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
  </div>
</template>
