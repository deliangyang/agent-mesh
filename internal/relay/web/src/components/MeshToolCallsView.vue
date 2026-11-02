<script setup lang="ts">
import { computed, watch } from "vue";
import type { AssistantMessage, Message, ToolCall, ToolMessage } from "@ag-ui/core";
import { useCopilotKit } from "@copilotkit/vue/v2";
import { noteAskFromTool } from "../ask";
import { meshLive } from "../meshAgent";
import MeshActivityWindow from "./MeshActivityWindow.vue";
import MeshToolWindow from "./MeshToolWindow.vue";
import { TOOL_TAIL, formatToolResult, summarizeToolGroup } from "../toolSummary";

const props = defineProps<{
  message: AssistantMessage;
  messages?: Message[];
}>();

const { executingToolCallIds } = useCopilotKit();

function hasToolCalls(m: Message): m is AssistantMessage {
  return m.role === "assistant" && Array.isArray((m as AssistantMessage).toolCalls) && !!(m as AssistantMessage).toolCalls?.length;
}

function turnSlice(messages: Message[], id: string): Message[] {
  const idx = messages.findIndex((m) => m.id === id);
  if (idx < 0) return messages;
  let start = 0;
  for (let i = idx; i >= 0; i--) {
    if (messages[i].role === "user") {
      start = i + 1;
      break;
    }
  }
  let end = messages.length;
  for (let i = idx + 1; i < messages.length; i++) {
    if (messages[i].role === "user") {
      end = i;
      break;
    }
  }
  return messages.slice(start, end);
}

const isPrimary = computed(() => {
  const slice = turnSlice(props.messages || [], props.message.id);
  const first = slice.find(hasToolCalls);
  return first?.id === props.message.id;
});

const rows = computed(() => {
  if (!isPrimary.value) return [];
  const running = executingToolCallIds.value;
  const slice = turnSlice(props.messages || [], props.message.id);
  const msgs = props.messages || [];
  const seen: Record<string, boolean> = {};
  const out: { id: string; name: string; args: unknown; status: string; result?: string }[] = [];
  for (const m of slice) {
    if (!hasToolCalls(m)) continue;
    for (const tc of (m.toolCalls || []) as ToolCall[]) {
      if (!tc?.id || seen[tc.id]) continue;
      seen[tc.id] = true;
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

const title = computed(() => summarizeToolGroup(rows.value));
const hidden = computed(() => Math.max(0, rows.value.length - TOOL_TAIL));
const resultLine = computed(() => {
  if (rows.value.length !== 1) return "";
  return formatToolResult(rows.value[0].name, rows.value[0].result);
});
const visibleRows = computed(() => {
  if (rows.value.length === 1 && resultLine.value) return [];
  if (rows.value.length === 1) return [];
  return rows.value.slice(-TOOL_TAIL);
});
const revision = computed(
  () => title.value + "|" + hidden.value + "|" + resultLine.value + "|" + rows.value.map((r) => r.id + r.status + r.name).join("|"),
);
</script>

<template>
  <MeshActivityWindow v-if="rows.length" variant="tools" :title="title" :hidden="hidden" :revision="revision">
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
</template>
