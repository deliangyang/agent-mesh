<script setup lang="ts">
import { computed } from "vue";
import { thinkPreview } from "../toolSummary";
import MeshActivityWindow from "./MeshActivityWindow.vue";

const props = defineProps<{
  content: unknown;
  streaming?: boolean;
}>();

const preview = computed(() => thinkPreview(props.content));
const show = computed(() => props.streaming || !!preview.value);
</script>

<template>
  <MeshActivityWindow
    v-if="show"
    variant="think"
    :revision="preview"
  >
    <div class="mesh-act-think">{{ preview || "…" }}</div>
  </MeshActivityWindow>
</template>
