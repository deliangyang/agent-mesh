<script setup lang="ts">
import { computed, watch } from "vue";
import type { AssistantMessage, Message, ToolMessage } from "@ag-ui/core";
import { useCopilotKit } from "@copilotkit/vue/v2";
import { noteAskFromTool } from "../ask";
import MeshActivityWindow from "./MeshActivityWindow.vue";
import MeshToolWindow from "./MeshToolWindow.vue";
import { summarizeToolGroup } from "../toolSummary";

const props = defineProps<{
  message: AssistantMessage;
  messages?: Message[];
}>();

const { executingToolCallIds } = useCopilotKit();

const rows = computed(() => {
  const running = executingToolCallIds.value;
  return (props.message.toolCalls || []).map((tc) => {
    const toolMsg = (props.messages || []).find(
      (m) => m.role === "tool" && (m as ToolMessage).toolCallId === tc.id,
    ) as ToolMessage | undefined;
    let status = "inProgress";
    if (toolMsg) status = "complete";
    else if (running.has(tc.id)) status = "executing";
    let args: unknown = tc.function.arguments;
    if (typeof args === "string") {
      try {
        args = JSON.parse(args);
      } catch {
        /* 流式参数尚未成 JSON */
      }
    }
    const result = toolMsg && typeof toolMsg.content === "string" ? toolMsg.content : undefined;
    return { id: tc.id, name: tc.function.name, args, status, result };
  });
});

watch(
  rows,
  (list) => {
    for (const row of list) {
      if (row.status !== "complete") noteAskFromTool(row.name, row.args, row.id);
    }
  },
  { immediate: true },
);

const title = computed(() => (rows.value.length > 1 ? summarizeToolGroup(rows.value) : ""));

const revision = computed(() => title.value + "|" + rows.value.map((r) => r.id + r.status + r.name).join("|"));
</script>

<template>
  <MeshActivityWindow v-if="rows.length" variant="tools" :title="title" :revision="revision">
    <MeshToolWindow
      v-for="row in rows"
      :key="row.id"
      :name="row.name"
      :tool-call-id="row.id"
      :args="row.args"
      :status="row.status"
      :result="row.result"
    />
  </MeshActivityWindow>
</template>
