<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, watch } from "vue";
import { dismissAsk, meshAsk, submitAsk } from "../ask";
import { t } from "../locale";

const picks = reactive<Record<string, string[]>>({});

watch(
  () => meshAsk.prompt,
  (prompt) => {
    for (const key of Object.keys(picks)) delete picks[key];
    if (!prompt) return;
    for (const q of prompt.questions) {
      picks[q.id || q.prompt] = [];
    }
  },
  { immediate: true },
);

const ready = computed(() => {
  const prompt = meshAsk.prompt;
  if (!prompt) return false;
  return prompt.questions.every((q) => (picks[q.id || q.prompt] || []).length > 0);
});

function keyOf(q: { id?: string; prompt: string }): string {
  return q.id || q.prompt;
}

function chosen(qkey: string, id: string): boolean {
  return (picks[qkey] || []).includes(id);
}

function toggle(qkey: string, id: string, multi: boolean): void {
  const cur = picks[qkey] ? picks[qkey].slice() : [];
  if (!multi) {
    picks[qkey] = [id];
    return;
  }
  const i = cur.indexOf(id);
  if (i >= 0) cur.splice(i, 1);
  else cur.push(id);
  picks[qkey] = cur;
}

async function confirm(): Promise<void> {
  if (!meshAsk.prompt || !ready.value || meshAsk.answering) return;
  await submitAsk(meshAsk.prompt, { ...picks });
}

function onKey(ev: KeyboardEvent): void {
  if (ev.key !== "Escape" || !meshAsk.prompt) return;
  ev.preventDefault();
  dismissAsk();
}

onMounted(() => {
  window.addEventListener("keydown", onKey);
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKey);
});
</script>

<template>
  <div v-if="meshAsk.prompt" class="mesh-ask" role="group" :aria-label="meshAsk.prompt.title || t('ask.chooseOption')">
    <div class="mesh-ask-head">
      <div class="mesh-ask-kicker">{{ meshAsk.prompt.kind === "permission" ? t("ask.waitConfirm") : t("ask.waitChoice") }}</div>
      <button type="button" class="mesh-ask-close" :aria-label="t('ask.dismiss')" :title="t('ask.close')" @click="dismissAsk">
        {{ t("ask.close") }}
      </button>
    </div>
    <div v-if="meshAsk.prompt.title" class="mesh-ask-title">{{ meshAsk.prompt.title }}</div>
    <div v-for="q in meshAsk.prompt.questions" :key="keyOf(q)" class="mesh-ask-q">
      <p>{{ q.prompt }}</p>
      <button
        v-for="opt in q.options"
        :key="opt.id"
        type="button"
        class="mesh-ask-opt"
        :class="{ on: chosen(keyOf(q), opt.id) }"
        :aria-pressed="chosen(keyOf(q), opt.id)"
        @click="toggle(keyOf(q), opt.id, !!q.multi)"
      >
        <span class="mesh-ask-mark" aria-hidden="true"></span>
        <span class="mesh-ask-opt-body">
          <span class="mesh-ask-opt-label">{{ opt.label }}</span>
          <span v-if="opt.hint" class="mesh-ask-opt-hint">{{ opt.hint }}</span>
        </span>
      </button>
    </div>
    <div class="mesh-ask-bar">
      <p v-if="meshAsk.error" class="mesh-ask-err">{{ meshAsk.error }}</p>
      <button type="button" class="mesh-ask-go" :disabled="!ready || meshAsk.answering" @click="confirm">
        {{ meshAsk.answering ? t("ask.submitting") : t("ask.chooseThis") }}
      </button>
    </div>
  </div>
</template>
