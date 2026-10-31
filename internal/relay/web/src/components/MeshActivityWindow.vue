<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from "vue";

const props = defineProps<{
  title?: string;
  badge?: string;
  variant?: "tools" | "think";
  revision?: string | number;
}>();

const bodyEl = ref<HTMLElement | null>(null);

async function pinBottom(): Promise<void> {
  await nextTick();
  const el = bodyEl.value;
  if (el) el.scrollTop = el.scrollHeight;
}

watch(() => props.revision, () => {
  void pinBottom();
});

onMounted(() => {
  void pinBottom();
});
</script>

<template>
  <div class="mesh-act" :data-variant="variant || 'tools'">
    <div v-if="title" class="mesh-act-head">
      <span>{{ title }}</span>
      <span v-if="badge" class="mesh-act-badge">{{ badge }}</span>
    </div>
    <div ref="bodyEl" class="mesh-act-body" :class="variant || 'tools'">
      <slot />
    </div>
  </div>
</template>
