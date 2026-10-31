<script setup lang="ts">
import { CopilotChatAttachmentRenderer } from "@copilotkit/vue/v2";
import { computed } from "vue";
import { mediaParts } from "../attach";

const props = defineProps<{
  content: unknown;
}>();

const parts = computed(() => mediaParts(props.content));
</script>

<template>
  <div v-if="parts.length" class="mesh-user-files">
    <CopilotChatAttachmentRenderer
      v-for="(part, i) in parts"
      :key="part.filename || part.type + '-' + i"
      :type="part.type"
      :source="part.source"
      :filename="part.filename"
      class-name="mesh-user-file"
    />
  </div>
</template>
