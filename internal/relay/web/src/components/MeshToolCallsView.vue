<script setup lang="ts">
import { computed, watch } from "vue";
import type { AssistantMessage, Message, ToolCall, ToolMessage } from "@ag-ui/core";
import { useCopilotKit } from "@copilotkit/vue/v2";
import { noteAskFromTool } from "../ask";
import { meshLive } from "../meshAgent";
import MeshActivityWindow from "./MeshActivityWindow.vue";
import MeshTodoList from "./MeshTodoList.vue";
import MeshToolWindow from "./MeshToolWindow.vue";
import { isTodoTool, todosFromMessages } from "../todos.ts";
import { TOOL_TAIL, formatToolResult, summarizeToolGroup } from "../toolSummary";

const props = defineProps<{
  message: AssistantMessage;
  messages?: Message[];
}>();

const { executingToolCallIds } = useCopilotKit();

const rows = computed(() => {
  const running = executingToolCallIds.value;
  const msgs = props.messages || [];
  const out: { id: string; name: string; args: unknown; status: string; result?: string }[] = [];
  for (const tc of (props.message.toolCalls || []) as ToolCall[]) {
    if (!tc?.id) continue;
    const toolMsg = msgs.find(
      (row) => row.role === "tool" && (row as ToolMessage).toolCallId === tc.id,
    ) as ToolMessage | undefined;
    let status = "inProgress";
    if (toolMsg || !meshLive.streaming) status = "complete";
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
    out.push({ id: tc.id, name: tc.function.name, args, status, result });
  }
  return out;
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

const todoRows = computed(() => rows.value.filter((row) => isTodoTool(row.name, row.args)));
const toolRows = computed(() => rows.value.filter((row) => !isTodoTool(row.name, row.args)));
const todos = computed(() => {
  const msgs = props.messages || [];
  const cur = props.message;
  if (cur?.id && msgs.some((row) => row.id === cur.id)) {
    return todosFromMessages(msgs, cur.id);
  }
  return todosFromMessages(cur ? msgs.concat(cur) : msgs, cur?.id);
});
const todoPending = computed(() =>
  todoRows.value.some((row) => row.status === "inProgress" || row.status === "executing"),
);

const title = computed(() => summarizeToolGroup(toolRows.value));
const hidden = computed(() => Math.max(0, toolRows.value.length - TOOL_TAIL));
const resultLine = computed(() => {
  if (toolRows.value.length !== 1) return "";
  return formatToolResult(toolRows.value[0].name, toolRows.value[0].result);
});
const visibleRows = computed(() => {
  if (toolRows.value.length === 1 && resultLine.value) return [];
  if (toolRows.value.length === 1) return [];
  return toolRows.value.slice(-TOOL_TAIL);
});
const revision = computed(
  () =>
    title.value +
    "|" +
    hidden.value +
    "|" +
    resultLine.value +
    "|" +
    toolRows.value.map((r) => r.id + r.status + r.name).join("|") +
    "|" +
    todos.value.map((t) => t.id + t.status).join("|"),
);
</script>

<template>
  <MeshActivityWindow v-if="toolRows.length" variant="tools" :title="title" :hidden="hidden" :revision="revision">
    <MeshToolWindow
      v-for="row in visibleRows"
      :key="row.id"
      :name="row.name"
      :tool-call-id="row.id"
      :args="row.args"
      :status="row.status"
      :result="row.result"
    />
    <div v-if="resultLine" class="mesh-act-row mesh-act-result">{{ resultLine }}</div>
  </MeshActivityWindow>
  <MeshTodoList v-if="todoRows.length" :items="todos" :pending="todoPending" />
</template>
