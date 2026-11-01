<script setup lang="ts">
import { computed } from "vue";
import { formatToolLine } from "../toolSummary";

const props = defineProps<{
  name: string;
  toolCallId?: string;
  parameters?: unknown;
  args?: unknown;
  status: "inProgress" | "executing" | "complete" | string;
  result?: unknown;
}>();

const line = computed(() => formatToolLine(props.name, props.parameters ?? props.args));
</script>

<template>
  <div
    class="mesh-act-tool"
    data-testid="copilot-tool-render"
    :data-tool-name="name"
    :data-tool-call-id="toolCallId || ''"
    :data-status="status"
  >
    <div class="mesh-act-row" :data-status="status">{{ line }}</div>
  </div>
</template>
