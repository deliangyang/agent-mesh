<script setup lang="ts">
import { computed } from "vue";
import { StreamMarkdown } from "streamdown-vue";
import MeshActivityWindow from "./MeshActivityWindow.vue";

const props = defineProps<{
  content: unknown;
  streaming?: boolean;
}>();

const text = computed(() => (typeof props.content === "string" ? props.content : ""));
const show = computed(() => props.streaming || !!text.value.trim());
</script>

<template>
  <MeshActivityWindow
    v-if="show"
    variant="think"
    :revision="text.length + (streaming ? '-live' : '')"
  >
    <div class="mesh-act-think" :data-streaming="streaming ? 'true' : undefined">
      <StreamMarkdown
        v-if="text"
        class="copilot-chat-assistant-markdown"
        :content="text"
        :parse-incomplete-markdown="!!streaming"
        :code-block-show-line-numbers="false"
        :code-block-hide-copy="true"
        :code-block-hide-download="true"
        :shiki-theme="{ light: 'github-light', dark: 'github-dark' }"
      />
      <span v-else class="mesh-act-think-wait">…</span>
    </div>
  </MeshActivityWindow>
</template>
