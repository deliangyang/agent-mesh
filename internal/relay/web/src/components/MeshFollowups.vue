<script setup lang="ts">
import { dropFollowup, meshFollow, pullLastFollowup } from "../followup";
import { t } from "../locale";

const props = defineProps<{
  running: boolean;
  canStop: boolean;
}>();

const emit = defineEmits<{
  pull: [text: string];
  stop: [];
}>();

async function edit(id: string): Promise<void> {
  const item = meshFollow.items.find((it) => it.id === id);
  if (!item) return;
  emit("pull", item.text);
  await dropFollowup(id);
}

async function editLast(): Promise<void> {
  const text = await pullLastFollowup();
  if (text) emit("pull", text);
}
</script>

<template>
  <div v-if="meshFollow.items.length" class="mesh-follow" role="region" :aria-label="t('followups.label')">
    <div class="mesh-follow-head">
      <span class="mesh-follow-kicker">{{ t("followups.label") }}</span>
      <button
        v-if="canStop && running"
        type="button"
        class="mesh-follow-stop"
        @click="emit('stop')"
      >
        {{ t("followups.stop") }}
      </button>
    </div>
    <button
      v-for="item in meshFollow.items"
      :key="item.id"
      type="button"
      class="mesh-follow-item"
      :title="t('followups.editTitle')"
      @click="edit(item.id)"
    >
      <span class="mesh-follow-dot" aria-hidden="true"></span>
      <span class="mesh-follow-text">{{ item.text }}</span>
    </button>
    <div class="mesh-follow-foot">
      <button type="button" class="mesh-follow-hint" @click="editLast">{{ t("followups.edit") }}</button>
      <p v-if="meshFollow.error" class="mesh-follow-err">{{ meshFollow.error }}</p>
    </div>
  </div>
</template>
