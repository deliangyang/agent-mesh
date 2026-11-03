<script setup lang="ts">
import { computed } from "vue";
import { mediaParts, type MediaPart } from "../attach";

const props = defineProps<{
  content: unknown;
}>();

const parts = computed(() => mediaParts(props.content));

function srcOf(part: MediaPart): string {
  if (part.source.type === "url") return part.source.value;
  const mime = part.source.mimeType || "application/octet-stream";
  return `data:${mime};base64,${part.source.value}`;
}
</script>

<template>
  <div v-if="parts.length" class="mesh-user-files">
    <template v-for="(part, i) in parts" :key="part.filename || part.type + '-' + i">
      <img
        v-if="part.type === 'image'"
        class="mesh-user-file"
        :src="srcOf(part)"
        :alt="part.filename || 'image'"
        loading="lazy"
        data-testid="copilot-chat-attachment-renderer-image"
      />
      <a
        v-else
        class="mesh-user-file mesh-user-doc"
        :href="srcOf(part)"
        :download="part.filename || undefined"
        target="_blank"
        rel="noopener"
        data-testid="copilot-chat-attachment-renderer-document"
      >
        {{ part.filename || part.type }}
      </a>
    </template>
  </div>
</template>
