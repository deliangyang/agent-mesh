<script setup lang="ts">
import { computed, watch } from "vue";
import { attachJobs, dropAttachJob, jobOf, syncAttachJobs } from "../attachUpload";
import { t } from "../locale";

type AttachRow = {
  id: string;
  type?: string;
  filename?: string;
  size?: number;
  status?: string;
  source?: { type: string; value: string; mimeType?: string };
};

const props = defineProps<{
  items: AttachRow[];
}>();

const emit = defineEmits<{
  remove: [id: string];
}>();

watch(
  () => props.items,
  (rows) => {
    syncAttachJobs(rows);
  },
  { deep: true },
);

type Card = {
  id: string;
  name: string;
  kind: "image" | "file";
  preview: string;
  status: "uploading" | "ready" | "error";
  progress: number;
  indeterminate: boolean;
  error: string;
  jobKey: string;
};

const cards = computed((): Card[] => {
  const out: Card[] = [];
  const seen = new Set<string>();
  for (const row of props.items || []) {
    const name = row.filename || t("attach.file");
    const size = row.size || 0;
    const job = jobOf(name, size);
    const key = job?.key || name + "\0" + size;
    seen.add(key);
    const uploading = row.status === "uploading";
    out.push({
      id: row.id,
      name,
      kind: row.type === "image" || (row.source?.mimeType || "").startsWith("image/") ? "image" : "file",
      preview: previewOf(row, job?.preview || ""),
      status: uploading ? "uploading" : "ready",
      progress: uploading ? job?.progress || 0 : 100,
      indeterminate: !!(uploading && job?.indeterminate),
      error: "",
      jobKey: key,
    });
  }
  for (const job of attachJobs) {
    if (!job.error || seen.has(job.key)) continue;
    out.push({
      id: "",
      name: job.name,
      kind: job.mime.startsWith("image/") ? "image" : "file",
      preview: job.preview,
      status: "error",
      progress: 0,
      indeterminate: false,
      error: job.error,
      jobKey: job.key,
    });
  }
  return out;
});

function previewOf(row: AttachRow, fallback: string): string {
  const src = row.source;
  if (!src || !src.value) return fallback;
  if (src.type === "url") return src.value;
  const mime = src.mimeType || "application/octet-stream";
  return "data:" + mime + ";base64," + src.value;
}

function dismiss(card: Card): void {
  if (card.status === "error") {
    dropAttachJob(card.jobKey);
    return;
  }
  if (card.id) emit("remove", card.id);
}

function labelOf(card: Card): string {
  if (card.status === "error") return card.error;
  if (card.status === "ready") return card.name;
  if (card.indeterminate) return t("attach.readingEllipsis");
  return card.progress + "%";
}
</script>

<template>
  <div v-if="cards.length" class="mesh-attach" role="list" :aria-label="t('attach.list')">
    <div
      v-for="card in cards"
      :key="card.id || card.jobKey"
      class="mesh-attach-card"
      :class="{
        'is-error': card.status === 'error',
        'is-uploading': card.status === 'uploading',
        'is-file': card.kind !== 'image',
      }"
      role="listitem"
      :title="labelOf(card)"
    >
      <template v-if="card.status === 'error'">
        <img
          v-if="card.preview && card.kind === 'image'"
          class="mesh-attach-err-thumb"
          :src="card.preview"
          :alt="card.name"
        />
        <p class="mesh-attach-err">{{ card.error }}</p>
      </template>
      <template v-else>
        <img
          v-if="card.preview && card.kind === 'image'"
          class="mesh-attach-thumb"
          :src="card.preview"
          :alt="card.name"
        />
        <div v-else class="mesh-attach-fallback">{{ card.name }}</div>
        <div v-if="card.status === 'uploading'" class="mesh-attach-veil">
          <span class="mesh-attach-pct">{{ card.indeterminate ? t("attach.reading") : card.progress + "%" }}</span>
          <span class="mesh-attach-bar" :class="{ indet: card.indeterminate }">
            <span
              class="mesh-attach-bar-fill"
              :style="{ width: card.indeterminate ? '40%' : card.progress + '%' }"
            ></span>
          </span>
        </div>
      </template>
      <button
        type="button"
        class="mesh-attach-x"
        :aria-label="card.status === 'error' ? t('attach.dismiss') : t('attach.remove')"
        @click="dismiss(card)"
      >
        ×
      </button>
    </div>
  </div>
</template>
