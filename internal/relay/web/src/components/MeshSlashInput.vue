<script setup lang="ts">
import { CopilotChatInput } from "@copilotkit/vue/v2";
import { computed, nextTick, ref, watch } from "vue";
import type { SlashItem } from "../types";

type InputMode = "input" | "transcribe" | "processing";

const props = defineProps<{
  modelValue: string;
  isRunning: boolean;
  inputMode: InputMode;
  items: SlashItem[];
  canStop: boolean;
  canAddFile: boolean;
  canTranscribe: boolean;
  fileCount?: number;
  onUpdateModelValue: (value: string) => void;
  onSubmitMessage: (value: string) => void;
  onStop: () => void;
  onAddFile: () => void;
  onStartTranscribe: () => void;
  onCancelTranscribe: () => void;
  onFinishTranscribe: () => void;
  onFinishTranscribeWithAudio: (audioBlob: Blob) => void | Promise<void>;
  onPullFollowup?: () => boolean | Promise<boolean>;
}>();

const highlight = ref(0);
const dismissed = ref(false);
const locked = ref("");
const hasFiles = computed(() => (props.fileCount || 0) > 0);

const query = computed(() => {
  const text = props.modelValue || "";
  const first = text.split(/\r?\n/, 1)[0] || "";
  if (!first.startsWith("/")) return null;
  return first.slice(1);
});

const filtered = computed(() => {
  if (query.value === null) return [] as SlashItem[];
  const q = query.value.trim().toLowerCase();
  if (!q) return props.items;
  const starts: SlashItem[] = [];
  const contains: SlashItem[] = [];
  for (const item of props.items) {
    const name = item.name.toLowerCase();
    const desc = (item.description || "").toLowerCase();
    const kind = (item.kind || "").toLowerCase();
    if (name.startsWith(q) || ("/" + name).startsWith("/" + q)) {
      starts.push(item);
      continue;
    }
    if (name.includes(q) || desc.includes(q) || kind.includes(q)) {
      contains.push(item);
    }
  }
  return starts.concat(contains);
});

const open = computed(
  () =>
    query.value !== null &&
    !dismissed.value &&
    !locked.value &&
    props.items.length > 0 &&
    filtered.value.length > 0,
);

watch(query, (next) => {
  if (next === null) {
    dismissed.value = false;
    locked.value = "";
    highlight.value = 0;
    return;
  }
  if (highlight.value >= filtered.value.length) highlight.value = 0;
});

watch(
  () => filtered.value.length,
  (n) => {
    if (highlight.value >= n) highlight.value = 0;
  },
);

function kindLabel(kind: string): string {
  if (kind === "skill") return "skill";
  return "command";
}

function fillText(item: SlashItem): string {
  return "/" + item.name + " ";
}

function pick(item: SlashItem): void {
  highlight.value = 0;
  locked.value = item.name;
  dismissed.value = true;
  props.onUpdateModelValue(fillText(item));
}

function onKey(ev: KeyboardEvent): void {
  if (
    ev.key === "Enter" &&
    !ev.shiftKey &&
    !open.value &&
    !(props.modelValue || "").trim() &&
    hasFiles.value
  ) {
    ev.preventDefault();
    ev.stopPropagation();
    submit("请查看附件。");
    return;
  }
  if (
    ev.key === "ArrowUp" &&
    !open.value &&
    !(props.modelValue || "").trim() &&
    props.onPullFollowup
  ) {
    ev.preventDefault();
    ev.stopPropagation();
    void Promise.resolve(props.onPullFollowup()).then((ok) => {
      if (!ok) return;
    });
    return;
  }
  if (!open.value) return;
  if (ev.key === "ArrowDown") {
    ev.preventDefault();
    ev.stopPropagation();
    highlight.value = (highlight.value + 1) % filtered.value.length;
    return;
  }
  if (ev.key === "ArrowUp") {
    ev.preventDefault();
    ev.stopPropagation();
    highlight.value = (highlight.value - 1 + filtered.value.length) % filtered.value.length;
    return;
  }
  if (ev.key === "Enter" && !ev.shiftKey) {
    const item = filtered.value[highlight.value];
    if (!item) return;
    ev.preventDefault();
    ev.stopPropagation();
    pick(item);
    return;
  }
  if (ev.key === "Escape") {
    ev.preventDefault();
    ev.stopPropagation();
    dismissed.value = true;
  }
}

function onInput(value: string): void {
  if (locked.value && !value.startsWith("/" + locked.value)) {
    locked.value = "";
  }
  if (!locked.value) dismissed.value = false;
  props.onUpdateModelValue(value);
}

let lastSubmitAt = 0;
function submit(value: string): void {
  const now = Date.now();
  if (now - lastSubmitAt < 80) return;
  lastSubmitAt = now;
  props.onSubmitMessage(value);
}

function onSend(orig: () => void): void {
  if (!(props.modelValue || "").trim() && hasFiles.value) {
    props.onUpdateModelValue("请查看附件。");
    void nextTick(() => orig());
    return;
  }
  orig();
}
</script>

<template>
  <div class="mesh-slash-host" @keydown.capture="onKey">
    <CopilotChatInput
      :model-value="modelValue"
      :is-running="false"
      :mode="inputMode"
      :tools-menu="[]"
      :on-submit-message="submit"
      @submit-message="submit"
      :on-stop="undefined"
      :on-add-file="canAddFile ? onAddFile : undefined"
      :on-start-transcribe="canTranscribe ? onStartTranscribe : undefined"
      :on-cancel-transcribe="canTranscribe ? onCancelTranscribe : undefined"
      :on-finish-transcribe="canTranscribe ? onFinishTranscribe : undefined"
      :on-finish-transcribe-with-audio="canTranscribe ? onFinishTranscribeWithAudio : undefined"
      positioning="static"
      :show-disclaimer="true"
      :bottom-anchored="true"
      @update:model-value="onInput"
    >
      <template #add-menu-button="{ disabled, labels }">
        <button
          type="button"
          class="mesh-add-btn"
          data-testid="copilot-chat-input-add"
          :disabled="disabled"
          :aria-label="labels.chatInputToolbarAddButtonLabel"
          :title="labels.chatInputToolbarAddButtonLabel"
          @click.stop="onAddFile"
        >
          <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <path d="M8 3.2v9.6M3.2 8h9.6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
          </svg>
        </button>
      </template>
      <template #send-button="{ disabled, onClick }">
        <div class="cpk:mr-[10px]">
          <button
            type="button"
            class="mesh-send-btn"
            data-testid="copilot-chat-input-send"
            :disabled="disabled && !hasFiles"
            aria-label="发送"
            @click="onSend(onClick)"
          >
            <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
              <path
                d="M8 12.2V3.8M4.4 7.2 8 3.8l3.6 3.4"
                stroke="currentColor"
                stroke-width="1.6"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
            </svg>
          </button>
        </div>
      </template>
    </CopilotChatInput>
    <div v-if="open" class="mesh-slash" role="listbox" aria-label="命令与技能">
      <button
        v-for="(item, index) in filtered"
        :key="item.kind + ':' + item.name"
        type="button"
        role="option"
        class="mesh-slash-item"
        :class="{ on: index === highlight }"
        :aria-selected="index === highlight"
        @mouseenter="highlight = index"
        @mousedown.prevent="pick(item)"
      >
        <span class="mesh-slash-name">/{{ item.name }}</span>
        <span v-if="item.argument_hint" class="mesh-slash-hint">{{ item.argument_hint }}</span>
        <span class="mesh-slash-desc">{{ item.description }}</span>
        <span class="mesh-slash-kind">{{ kindLabel(item.kind) }}</span>
      </button>
    </div>
  </div>
</template>
