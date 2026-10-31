<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { meshTarget } from "../target";
import type { GitBundle, GitFile, GitRepo } from "../types";

const props = defineProps<{ open: boolean }>();
const emit = defineEmits<{ close: [] }>();

type DiffLine = { kind: "hunk" | "add" | "del" | "ctx" | "meta"; text: string };

const repos = ref<GitRepo[]>([]);
const bundle = ref<GitBundle | null>(null);
const active = ref("");
const loading = ref(false);
const error = ref("");
const openFile = ref("");

const cwd = computed(() => meshTarget.cwd);

watch(
  () => [props.open, cwd.value] as const,
  ([open]) => {
    if (open) void loadRepos();
  },
);

onMounted(() => {
  if (props.open) void loadRepos();
});

async function loadRepos(): Promise<void> {
  if (!cwd.value) {
    repos.value = [];
    bundle.value = null;
    error.value = "先选一个工作目录";
    return;
  }
  loading.value = true;
  error.value = "";
  try {
    const q = new URLSearchParams({ cwd: cwd.value, node: meshTarget.node });
    const resp = await fetch("/v1/openai/git?" + q.toString(), { credentials: "same-origin" });
    if (!resp.ok) throw new Error((await resp.text()) || "http " + resp.status);
    const data = (await resp.json()) as { repos?: GitRepo[] };
    repos.value = data.repos || [];
    if (!repos.value.length) {
      bundle.value = null;
      active.value = "";
      return;
    }
    const keep = repos.value.find((r) => r.path === active.value) || repos.value[0];
    await loadDiff(keep.path);
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    loading.value = false;
  }
}

async function loadDiff(path: string): Promise<void> {
  active.value = path;
  openFile.value = "";
  loading.value = true;
  error.value = "";
  try {
    const q = new URLSearchParams({ repo: path, node: meshTarget.node, cwd: cwd.value });
    const resp = await fetch("/v1/openai/git/diff?" + q.toString(), { credentials: "same-origin" });
    if (!resp.ok) throw new Error((await resp.text()) || "http " + resp.status);
    bundle.value = (await resp.json()) as GitBundle;
    const first = (bundle.value.files || []).find((f) => f.patch);
    openFile.value = first?.path || "";
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
    bundle.value = null;
  } finally {
    loading.value = false;
  }
}

function parsePatch(patch: string): DiffLine[] {
  const out: DiffLine[] = [];
  for (const raw of (patch || "").split("\n")) {
    if (raw.startsWith("diff --git ") || raw.startsWith("index ") || raw.startsWith("new file") || raw.startsWith("deleted file")) {
      continue;
    }
    if (raw.startsWith("--- ") || raw.startsWith("+++ ")) {
      out.push({ kind: "meta", text: raw });
      continue;
    }
    if (raw.startsWith("@@")) {
      out.push({ kind: "hunk", text: raw });
      continue;
    }
    if (raw.startsWith("+")) {
      out.push({ kind: "add", text: raw.slice(1) });
      continue;
    }
    if (raw.startsWith("-")) {
      out.push({ kind: "del", text: raw.slice(1) });
      continue;
    }
    if (raw.startsWith("\\")) continue;
    out.push({ kind: "ctx", text: raw.startsWith(" ") ? raw.slice(1) : raw });
  }
  return out;
}

function linesOf(file: GitFile): DiffLine[] {
  return parsePatch(file.patch || "");
}

function toggleFile(path: string): void {
  openFile.value = openFile.value === path ? "" : path;
}

const emptyHint = computed(() => {
  if (error.value) return "";
  if (!cwd.value) return "选好工作目录后再看改动。";
  if (!repos.value.length) return "这个目录下面还没有 git 仓库。";
  if (bundle.value && !(bundle.value.files || []).length) return "工作区是干净的。";
  return "";
});
</script>

<template>
  <div v-if="open" class="mesh-diff-mask" @click="emit('close')"></div>
  <aside v-if="open" class="mesh-diff" role="dialog" aria-label="项目改动">
    <header class="mesh-diff-head">
      <div>
        <div class="mesh-diff-name">改动</div>
        <div class="mesh-diff-cwd">{{ cwd || "未选工作目录" }}</div>
      </div>
      <button type="button" class="ghost" aria-label="关闭改动" @click="emit('close')">关闭</button>
    </header>
    <div class="mesh-diff-repos">
      <button
        v-for="r in repos"
        :key="r.path"
        type="button"
        class="mesh-diff-repo"
        :class="{ on: active === r.path }"
        @click="loadDiff(r.path)"
      >
        <span class="mesh-diff-repo-name">{{ r.name }}</span>
        <span class="mesh-diff-repo-meta">{{ r.branch || "—" }}{{ r.dirty ? " · 有改动" : "" }}</span>
      </button>
    </div>
    <div class="mesh-diff-body">
      <div v-if="loading" class="mesh-diff-empty">正在读取 git diff…</div>
      <div v-else-if="emptyHint" class="mesh-diff-empty">{{ emptyHint }}</div>
      <div v-else>
        <button
          v-for="f in bundle?.files || []"
          :key="f.path"
          type="button"
          class="mesh-diff-file"
          :class="{ on: openFile === f.path }"
          @click="toggleFile(f.path)"
        >
          <span class="mesh-diff-file-head">
            <span class="mesh-diff-file-path">{{ f.path }}</span>
            <span class="mesh-diff-file-st">{{ f.status }}</span>
          </span>
          <div v-if="openFile === f.path" class="mesh-diff-hunks" @click.stop>
            <pre v-if="!f.patch" class="mesh-diff-empty">没有可展开的 diff</pre>
            <div v-else class="mesh-diff-code">
              <div v-for="(ln, i) in linesOf(f)" :key="i" class="mesh-diff-ln" :data-k="ln.kind">
                {{ ln.text }}
              </div>
            </div>
          </div>
        </button>
      </div>
    </div>
  </aside>
</template>
