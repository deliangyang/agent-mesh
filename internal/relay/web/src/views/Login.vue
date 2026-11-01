<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";
import { loadLastPick, pickToQuery } from "../lastPick";
import { toastError } from "../toast";

const password = ref("");
const err = ref("");
const busy = ref(false);
const router = useRouter();

async function submit(): Promise<void> {
  err.value = "";
  busy.value = true;
  try {
    const resp = await fetch("/login", {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ password: password.value }),
    });
    if (!resp.ok) {
      err.value = "密码不对，再试一次";
      return;
    }
    const pick = loadLastPick();
    await router.replace(pick ? { path: "/", query: pickToQuery(pick) } : "/");
  } catch (e) {
    err.value = "网络连接失败，请检查网络后重试";
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
      <h1>进入对话</h1>
      <p class="hint">输入约定密码</p>
      <input v-model="password" type="password" autofocus required placeholder="密码" />
      <p v-if="err" class="mesh-err">{{ err }}</p>
      <button type="submit" :disabled="busy">进入</button>
    </form>
  </div>
</template>
