<script setup lang="ts">
import { CopilotChat, CopilotChatUserMessage, useDefaultRenderTool } from "@copilotkit/vue/v2";
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { catalog, connectCatalog, meshLink, refreshCatalog, watchSession } from "../catalog";
import MeshAskCard from "../components/MeshAskCard.vue";
import MeshDiffPanel from "../components/MeshDiffPanel.vue";
import MeshFollowups from "../components/MeshFollowups.vue";
import MeshSlashInput from "../components/MeshSlashInput.vue";
import MeshThinkWindow from "../components/MeshThinkWindow.vue";
import MeshToolCallsView from "../components/MeshToolCallsView.vue";
import MeshToolWindow from "../components/MeshToolWindow.vue";
import MeshUserParts from "../components/MeshUserParts.vue";
import { loadLastPick, pickToQuery, saveLastPick } from "../lastPick";
import { bindHistoryThread, expectHistory, isLiveChat, meshAgent, meshLive, resetDraft } from "../meshAgent";
import { loadTheme, toggleTheme } from "../theme";
import { pushFollowup, pullLastFollowup, shouldQueueFollowup, watchFollowups } from "../followup";
import { slashItems, watchSlashCatalog } from "../slash";
import { meshTarget, threadIdOf } from "../target";
import type { DirRow, Session } from "../types";

useDefaultRenderTool({ render: MeshToolWindow });

const route = useRoute();
const router = useRouter();
const savedPick = loadLastPick();
const selected = ref<Session | null>(null);
const chatKey = ref("draft");
const creating = ref(false);
const turnEnded = ref(false);
let endedTimer = 0;
const theme = ref(loadTheme());
const navOpen = ref(typeof window !== "undefined" && window.matchMedia("(min-width: 861px)").matches);
const openPick = ref("");
const diffOpen = ref(false);
const picksOpen = reactive({ node: false, engine: false });

function flipTheme(): void {
  theme.value = toggleTheme();
}

function isMobileNav(): boolean {
  return window.matchMedia("(max-width: 860px)").matches;
}

function toggleNav(): void {
  navOpen.value = !navOpen.value;
}

function closeNav(): void {
  navOpen.value = false;
}

function closeNavIfMobile(): void {
  if (isMobileNav()) navOpen.value = false;
}

const labels = {
  chatInputPlaceholder: "输入消息，/ 调用命令或技能",
  welcomeMessageText: "有什么可以帮你的？",
  chatDisclaimerText: "AI 可能会出错，请核实重要信息。",
  modalHeaderTitle: "对话",
  reasoningMessageThinkingLabel: "思考中…",
  chatInputToolbarAddButtonLabel: "添加图片或文件",
};

const copied = ref(false);
let copiedTimer = 0;

const linkHint = computed(() => {
  if (meshLink.state === "live") return "";
  if (meshLink.state === "connecting") return "正在连接节点";
  return "连接已断开，正在重连";
});

const attachConfig = {
  enabled: true,
  maxSize: 20 * 1024 * 1024,
};

function lastTurnStart(messages: { role?: string }[]): number {
  for (let i = messages.length - 1; i >= 0; i--) {
    if (messages[i].role === "user") return i + 1;
  }
  return 0;
}

function lastAssistantId(messages: { id?: string; role?: string }[] | undefined): string {
  const rows = messages || [];
  const start = lastTurnStart(rows);
  let last = "";
  for (let i = start; i < rows.length; i++) {
    if (rows[i].role === "assistant") last = rows[i].id || "";
  }
  return last;
}

function showThinkAfter(message: { id?: string; role?: string }): boolean {
  if (!meshLive.thinking) return false;
  const rows = meshAgent.messages || [];
  const lastAsst = lastAssistantId(rows);
  if (lastAsst) return message.role === "assistant" && message.id === lastAsst;
  return rows.length > 0 && message.id === rows[rows.length - 1]?.id;
}

const thinkInComposer = computed(() => meshLive.thinking && !lastAssistantId(meshAgent.messages));

const filters = reactive({
  node: String(route.query.node || savedPick?.node || ""),
  engine: String(route.query.engine || savedPick?.engine || ""),
  model: String(route.query.model || savedPick?.model || ""),
  dir: String(route.query.dir || savedPick?.dir || ""),
});

const engineCards = computed(() => {
  const seen: Record<string, boolean> = {};
  return catalog.engines.filter((e) => {
    if (filters.node && e.node_id && e.node_id !== filters.node) return false;
    if (seen[e.name]) return false;
    seen[e.name] = true;
    return true;
  });
});

const nodePickLabel = computed(() => {
  const n = catalog.nodes.find((row) => row.id === filters.node);
  return n ? nodeName(n.id, n.name) : "未选";
});

const enginePickLabel = computed(() => {
  const e = engineCards.value.find((row) => row.name === filters.engine);
  return e ? e.title || e.name : "未选";
});

const modelOptions = computed(() => {
  return catalog.models
    .filter((m) => {
      if (filters.node && !String(m.id).startsWith(filters.node + "/")) return false;
      if (filters.engine) {
        const id = String(m.id);
        if (m.owned_by !== filters.engine && !id.includes("/" + filters.engine)) return false;
      }
      return true;
    })
    .slice(0, 80);
});

const dirOptions = computed(() => {
  return catalog.dirs.filter(
    (d) => (!filters.node || d.node_id === filters.node) && (!filters.engine || d.engine === filters.engine),
  );
});

function sessionStamp(s: Session): number {
  if (meshLive.streaming && meshLive.session && s.id === meshLive.session) {
    return Date.now() / 1000;
  }
  return s.updated_at || 0;
}

function sessionListable(s: Session): boolean {
  if (selected.value && selected.value.id === s.id && selected.value.engine === s.engine) return true;
  if (meshLive.streaming && meshLive.session && s.id === meshLive.session) return true;
  return !!(s.title || "").trim();
}

const sessionRows = computed(() => {
  const rows = catalog.sessions.filter((s) => {
    if (filters.node && s.node_id !== filters.node) return false;
    if (filters.engine && s.engine !== filters.engine) return false;
    if (!sessionListable(s)) return false;
    return true;
  });
  const sel = selected.value;
  let list = rows;
  if (sel?.id && !rows.some((s) => s.id === sel.id && s.engine === sel.engine)) {
    if (
      (!filters.node || sel.node_id === filters.node) &&
      (!filters.engine || sel.engine === filters.engine) &&
      sessionListable(sel)
    ) {
      list = rows.concat(sel);
    }
  }
  return list.slice().sort((a, b) => sessionStamp(b) - sessionStamp(a));
});

type SessionGroup = { label: string; items: Session[] };

const sessionGroups = computed((): SessionGroup[] => {
  const now = new Date();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const yesterday = today - 86400000;
  const week = today - 6 * 86400000;
  const groups: SessionGroup[] = [
    { label: "今天", items: [] },
    { label: "昨天", items: [] },
    { label: "7天内", items: [] },
    { label: "更早", items: [] },
  ];
  for (const s of sessionRows.value) {
    const t = sessionStamp(s) * 1000;
    if (t >= today) groups[0].items.push(s);
    else if (t >= yesterday) groups[1].items.push(s);
    else if (t >= week) groups[2].items.push(s);
    else groups[3].items.push(s);
  }
  return groups.filter((g) => g.items.length || (creating.value && g.label === "今天"));
});

const threadId = computed(() => {
  const s = selected.value;
  if (!s) return "";
  return threadIdOf(s.node_id || filters.node, s.engine, s.id);
});

const isDraftChat = computed(() => chatKey.value === "draft" || chatKey.value.startsWith("draft-"));
const chatThreadId = computed(() => (isDraftChat.value ? chatKey.value : threadId.value || undefined));

const chatTitle = computed(() => {
  if (creating.value) return meshLive.title || "新会话";
  return selected.value?.title || meshLive.title || "对话";
});

const welcomeHint = computed(() => {
  const node = catalog.nodes.find((n) => n.id === filters.node);
  const engine = engineCards.value.find((e) => e.name === filters.engine);
  const nodeLabel = nodeName(filters.node, node?.name);
  const engineLabel = engine?.title || engine?.name || "";
  if (engineLabel && filters.node) return `将发到 ${nodeLabel} 的 ${engineLabel}`;
  if (filters.node) return `节点 ${nodeLabel}`;
  return "先在左侧选好节点和引擎";
});

async function copyTitle(): Promise<void> {
  const title = chatTitle.value || "";
  const sid = selected.value?.id || meshLive.session || "";
  const text = sid ? title + "\n" + sid : title;
  if (!text.trim()) return;
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    const box = document.createElement("textarea");
    box.value = text;
    box.setAttribute("readonly", "");
    box.style.position = "fixed";
    box.style.left = "-9999px";
    document.body.appendChild(box);
    box.select();
    document.execCommand("copy");
    document.body.removeChild(box);
  }
  copied.value = true;
  window.clearTimeout(copiedTimer);
  copiedTimer = window.setTimeout(() => {
    copied.value = false;
  }, 1600);
}

function reloadPage(): void {
  location.reload();
}

function sessionStreaming(s: Session | null): boolean {
  if (!meshLive.streaming || !s) return false;
  if (meshLive.session && s.id === meshLive.session) return true;
  if (creating.value && !s.id) return true;
  return !!(
    selected.value &&
    selected.value.id === s.id &&
    selected.value.engine === s.engine &&
    (selected.value.node_id || "") === (s.node_id || "")
  );
}

function nodeName(id: string, name?: string): string {
  return name || id || "节点";
}

function modelLabel(id: string): string {
  if (!id) return "选择模型";
  for (const n of catalog.nodes) {
    if (n.id && id.startsWith(n.id + "/")) return id.slice(n.id.length + 1);
  }
  if (filters.node && id.startsWith(filters.node + "/")) return id.slice(filters.node.length + 1);
  return id;
}

function dirBase(path: string): string {
  const p = path.replace(/[\\/]+$/, "");
  const i = Math.max(p.lastIndexOf("/"), p.lastIndexOf("\\"));
  return i >= 0 ? p.slice(i + 1) : p || path;
}

function syncQuery(s: Session | null): void {
  const pick = {
    node: (s && s.node_id) || filters.node,
    engine: (s && s.engine) || filters.engine,
    model: filters.model,
    dir: filters.dir,
    session: s?.id || "",
  };
  saveLastPick(pick);
  router.replace({ path: "/", query: pickToQuery(pick) });
}

function findSession(node: string, engine: string, sid: string): Session | undefined {
  if (!sid) return undefined;
  const rows = catalog.sessions.filter(
    (s) => s.id === sid && (!node || !s.node_id || s.node_id === node),
  );
  return rows.find((s) => !engine || s.engine === engine) || rows[0];
}

function applySessionTitle(s: Session): Session {
  const next = { ...s };
  if (meshLive.title && (!meshLive.session || meshLive.session === next.id)) {
    next.title = meshLive.title;
  } else if (next.title && meshLive.session === next.id && !meshLive.title) {
    meshLive.title = next.title;
  }
  const prev = selected.value;
  if (prev && prev.id === next.id) {
    next.updated_at = Math.max(next.updated_at || 0, prev.updated_at || 0) || undefined;
    next.title = next.title || prev.title;
  }
  return next;
}

function openSession(s: Session): void {
  creating.value = false;
  selected.value = s;
  if (s.node_id) filters.node = s.node_id;
  if (s.engine) filters.engine = s.engine;
  if (s.cwd) filters.dir = s.cwd;
  meshTarget.session = s.id;
  syncModelToEngine();
  chatKey.value = threadIdOf(s.node_id || filters.node, s.engine, s.id) || "draft";
  syncQuery(s);
  watchSession(s);
  closeNavIfMobile();
}

function pickNode(id: string): void {
  filters.node = filters.node === id ? "" : id;
  if (filters.node && !engineCards.value.some((e) => e.name === filters.engine)) {
    filters.engine = engineCards.value[0]?.name || "";
  }
  syncModelToEngine();
  ensureWorkDir();
  picksOpen.node = false;
}

function syncModelToEngine(): void {
  const models = modelOptions.value;
  if (!models.length) return;
  if (models.some((m) => m.id === filters.model)) return;
  filters.model = models[0].id;
}

function pickEngine(name: string): void {
  const next = filters.engine === name ? "" : name;
  filters.engine = next;
  syncModelToEngine();
  ensureWorkDir();
  if (next) openLatestSessionOrNew();
  picksOpen.engine = false;
}

function ensureWorkDir(): void {
  const dirs = dirOptions.value;
  if (!dirs.length) return;
  const on = dirs.some(
    (d) =>
      d.path === filters.dir &&
      (!d.node_id || !filters.node || d.node_id === filters.node) &&
      (!d.engine || !filters.engine || d.engine === filters.engine),
  );
  if (on) return;
  const d = dirs[0];
  if (d.node_id) filters.node = d.node_id;
  if (d.engine) filters.engine = d.engine;
  filters.dir = d.path;
}

function pickModel(id: string): void {
  filters.model = id;
  openPick.value = "";
}

function pickDir(d: DirRow): void {
  if (d.node_id) filters.node = d.node_id;
  if (d.engine) filters.engine = d.engine;
  filters.dir = d.path;
  syncModelToEngine();
  openPick.value = "";
}

function togglePick(name: string): void {
  openPick.value = openPick.value === name ? "" : name;
}

function onDocClick(ev: MouseEvent): void {
  const t = ev.target as HTMLElement | null;
  if (t && t.closest(".mesh-chip")) return;
  openPick.value = "";
}

function newSession(): void {
  if (!filters.node && catalog.nodes.length) filters.node = catalog.nodes[0].id;
  if (!filters.engine && engineCards.value.length) filters.engine = engineCards.value[0].name;
  if (!filters.model && modelOptions.value.length) filters.model = modelOptions.value[0].id;
  ensureWorkDir();
  creating.value = true;
  selected.value = null;
  meshTarget.node = filters.node;
  meshTarget.engine = filters.engine;
  meshTarget.session = "";
  meshTarget.model = filters.model;
  meshTarget.cwd = filters.dir;
  watchSession(null);
  resetDraft();
  chatKey.value = "draft-" + Date.now();
  syncQuery(null);
  closeNavIfMobile();
}

function openLatestSessionOrNew(): void {
  const latest = sessionRows.value[0];
  if (latest) {
    const cur = selected.value;
    if (cur && cur.id === latest.id && cur.engine === latest.engine) return;
    openSession(latest);
    return;
  }
  newSession();
}

function hydrateFromQuery(): void {
  const q = route.query;
  const pick = loadLastPick();
  const node = String(q.node || pick?.node || "");
  const engine = String(q.engine || pick?.engine || "");
  const model = String(q.model || pick?.model || "");
  const dir = String(q.dir || pick?.dir || "");
  const sid = String(q.session || pick?.session || "");
  if (node) filters.node = node;
  if (engine) filters.engine = engine;
  if (model) filters.model = model;
  if (dir) filters.dir = dir;
  syncModelToEngine();
  if (!sid) {
    creating.value = true;
    selected.value = null;
    meshTarget.session = "";
    ensureWorkDir();
    watchSession(null);
    resetDraft();
    chatKey.value = "draft-" + Date.now();
    syncQuery(null);
    return;
  }
  const hit =
    findSession(node, engine, sid) ||
    ({
      id: sid,
      engine,
      node_id: node,
      title: engine || "会话",
    } as Session);
  if (hit.engine) filters.engine = hit.engine;
  syncModelToEngine();
  selected.value = hit;
  meshTarget.session = hit.id;
  chatKey.value = threadIdOf(hit.node_id || node, hit.engine, hit.id) || "draft";
  watchSession(hit);
  syncQuery(hit);
}

watch(
  () => catalog.sessions,
  () => {
    const sid = creating.value
      ? meshLive.session
      : selected.value?.id || meshLive.session || String(route.query.session || "");
    if (!sid) return;
    const hit = findSession(
      selected.value?.node_id || filters.node,
      selected.value?.engine || filters.engine,
      sid,
    );
    if (hit) {
      selected.value = applySessionTitle({
        ...hit,
        title: hit.title || selected.value?.title,
      });
      creating.value = false;
      watchSession(hit);
    }
  },
);

watch(
  () => [meshLive.session, meshLive.title] as const,
  () => {
    const sid = meshLive.session;
    if (!sid) {
      if (creating.value && meshLive.title && selected.value && !selected.value.id) {
        selected.value = { ...selected.value, title: meshLive.title };
      }
      return;
    }
    const known = findSession(filters.node, filters.engine, sid);
    const hit =
      known ||
      ({
        id: sid,
        engine: filters.engine,
        node_id: filters.node,
        cwd: filters.dir,
        title: meshLive.title || "新会话",
        live: true,
        updated_at: Math.floor(Date.now() / 1000),
      } as Session);
    selected.value = applySessionTitle(hit);
    meshTarget.session = hit.id;
    creating.value = !known;
    syncQuery(hit);
    watchSession(hit);
    bindHistoryThread(threadIdOf(hit.node_id || filters.node, hit.engine, hit.id));
  },
);

watch(
  () => [filters.node, filters.engine, filters.model, filters.dir, selected.value] as const,
  () => {
    const s = selected.value;
    meshTarget.node = (s && s.node_id) || filters.node;
    meshTarget.engine = (s && s.engine) || filters.engine;
    meshTarget.session = s?.id || meshLive.session || "";
    meshTarget.model = filters.model;
    meshTarget.cwd = filters.dir;
    // 草稿会话钉在 draft-* thread 上；中途改 thread-id 会 abort 首轮流式输出。
    if (isDraftChat.value) return;
    meshAgent.threadId = threadId.value;
    expectHistory(threadId.value);
  },
  { immediate: true },
);

watch(
  () => [filters.node, filters.engine, filters.model, filters.dir] as const,
  () => {
    syncQuery(selected.value);
  },
);

watch(
  () => dirOptions.value,
  () => {
    if (creating.value || isDraftChat.value) ensureWorkDir();
  },
);

watch(
  modelOptions,
  () => {
    syncModelToEngine();
  },
  { immediate: true },
);

watchSlashCatalog();
watchFollowups();

watch(
  () => meshLive.streaming,
  (now, was) => {
    if (was && !now) {
      turnEnded.value = true;
      window.clearTimeout(endedTimer);
      endedTimer = window.setTimeout(() => {
        turnEnded.value = false;
      }, 1800);
    } else if (now) {
      turnEnded.value = false;
      window.clearTimeout(endedTimer);
    }
  },
);

function fileCountOf(slot: object): number {
  const rows = (slot as { attachments?: unknown }).attachments;
  return Array.isArray(rows) ? rows.length : 0;
}

function onComposerSubmit(
  slot: {
    isRunning: boolean;
    onSubmitMessage: (value: string) => void;
    onUpdateModelValue: (value: string) => void;
  },
  text: string,
): void {
  if (!fileCountOf(slot) && shouldQueueFollowup(slot.isRunning && isLiveChat())) {
    void pushFollowup(text);
    slot.onUpdateModelValue("");
    return;
  }
  slot.onSubmitMessage(text);
}

async function onComposerPull(
  slot: { onUpdateModelValue: (value: string) => void },
): Promise<boolean> {
  const text = await pullLastFollowup();
  if (!text) return false;
  slot.onUpdateModelValue(text);
  return true;
}

function onLinkUp(): void {
  refreshCatalog();
  const tid = threadId.value;
  if (tid) expectHistory(tid);
}

onMounted(() => {
  hydrateFromQuery();
  connectCatalog();
  document.addEventListener("click", onDocClick);
  window.addEventListener("mesh-link-up", onLinkUp);
});

onBeforeUnmount(() => {
  document.removeEventListener("click", onDocClick);
  window.removeEventListener("mesh-link-up", onLinkUp);
  window.clearTimeout(copiedTimer);
  window.clearTimeout(endedTimer);
});
</script>

<template>
  <div class="mesh-app" :class="{ 'nav-open': navOpen }">
    <div class="mesh-nav-mask" @click="closeNav"></div>
    <aside class="mesh-drawer">
      <div class="mesh-drawer-header">
        <div class="mesh-brand">
          <svg class="mesh-mark" viewBox="0 0 32 32" fill="none" aria-hidden="true">
            <path
              d="M16 8 L8.4 21.4 H23.6 Z"
              stroke="currentColor"
              stroke-width="1.6"
              stroke-linejoin="round"
            />
            <circle cx="16" cy="8" r="2.7" fill="currentColor" />
            <circle cx="8.4" cy="21.4" r="2.7" fill="currentColor" />
            <circle cx="23.6" cy="21.4" r="2.7" fill="currentColor" />
          </svg>
          <span class="mesh-brand-word">Mesh</span>
        </div>
        <button
          type="button"
          class="ghost mesh-theme-btn"
          :title="theme === 'dark' ? '切换到浅色主题' : '切换到暗黑主题'"
          :aria-label="theme === 'dark' ? '切换到浅色主题' : '切换到暗黑主题'"
          @click="flipTheme"
        >
          <svg v-if="theme === 'dark'" viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <circle cx="8" cy="8" r="3.1" stroke="currentColor" stroke-width="1.4" />
            <path
              d="M8 1.6v1.5M8 12.9v1.5M1.6 8h1.5M12.9 8h1.5M3.3 3.3l1.1 1.1M11.6 11.6l1.1 1.1M3.3 12.7l1.1-1.1M11.6 4.4l1.1-1.1"
              stroke="currentColor"
              stroke-width="1.4"
              stroke-linecap="round"
            />
          </svg>
          <svg v-else viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <path
              d="M13.2 10.2A5.4 5.4 0 0 1 5.8 2.8 6.1 6.1 0 1 0 13.2 10.2Z"
              stroke="currentColor"
              stroke-width="1.4"
              stroke-linejoin="round"
            />
          </svg>
        </button>
        <form method="post" action="/logout">
          <button type="submit" class="ghost">退出</button>
        </form>
        <button type="button" class="ghost mesh-nav-close" aria-label="关闭侧边栏" @click="closeNav">
          <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
          </svg>
        </button>
      </div>
      <div class="mesh-picks">
        <button
          type="button"
          class="mesh-pick-toggle"
          :class="{ open: picksOpen.node }"
          :aria-expanded="picksOpen.node"
          aria-controls="mesh-node-cards"
          @click="picksOpen.node = !picksOpen.node"
        >
          <span class="mesh-pick-kicker">节点</span>
          <span class="mesh-pick-current">{{ nodePickLabel }}</span>
          <svg class="mesh-pick-chevron" viewBox="0 0 12 12" fill="none" aria-hidden="true">
            <path d="M4.2 2.4 8.4 6 4.2 9.6" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
        <div v-if="picksOpen.node" id="mesh-node-cards" class="mesh-cards">
          <button
            v-for="n in catalog.nodes"
            :key="n.id"
            type="button"
            class="mesh-card"
            :class="{ sel: filters.node === n.id }"
            @click="pickNode(n.id)"
          >
            {{ nodeName(n.id, n.name) }}
          </button>
          <div v-if="!catalog.nodes.length" class="mesh-pick-empty">接入节点后即可开始</div>
        </div>
        <button
          type="button"
          class="mesh-pick-toggle"
          :class="{ open: picksOpen.engine }"
          :aria-expanded="picksOpen.engine"
          aria-controls="mesh-engine-cards"
          @click="picksOpen.engine = !picksOpen.engine"
        >
          <span class="mesh-pick-kicker">引擎</span>
          <span class="mesh-pick-current">{{ enginePickLabel }}</span>
          <svg class="mesh-pick-chevron" viewBox="0 0 12 12" fill="none" aria-hidden="true">
            <path d="M4.2 2.4 8.4 6 4.2 9.6" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
        <div v-if="picksOpen.engine" id="mesh-engine-cards" class="mesh-cards">
          <button
            v-for="e in engineCards"
            :key="e.name"
            type="button"
            class="mesh-card"
            :class="{ sel: filters.engine === e.name }"
            @click="pickEngine(e.name)"
          >
            {{ e.title || e.name }}
          </button>
          <div v-if="!engineCards.length" class="mesh-pick-empty">先选一个节点</div>
        </div>
      </div>
      <button class="mesh-new" type="button" @click="newSession">
        <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
          <path d="M8 3.2v9.6M3.2 8h9.6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
        </svg>
        新会话
      </button>
      <div class="mesh-drawer-toolbar">
        <span>对话</span>
        <button class="ghost" type="button" @click="refreshCatalog">刷新</button>
      </div>
      <div class="mesh-threads">
        <div v-for="g in sessionGroups" :key="g.label" class="mesh-time-group">
          <div class="mesh-time-label">{{ g.label }}</div>
          <button
            v-if="creating && !selected?.id && g.label === '今天'"
            type="button"
            class="mesh-thread sel"
            :class="{ live: meshLive.streaming }"
          >
            <span v-if="meshLive.streaming" class="mesh-lamp" aria-hidden="true"></span>
            <span class="mesh-thread-name">{{ meshLive.title || "新会话" }}</span>
          </button>
          <button
            v-for="s in g.items"
            :key="s.node_id + s.engine + s.id"
            type="button"
            class="mesh-thread"
            :class="{
              sel: selected && selected.id === s.id && selected.engine === s.engine,
              live: sessionStreaming(s),
            }"
            @click="openSession(s)"
          >
            <span v-if="sessionStreaming(s)" class="mesh-lamp" aria-hidden="true"></span>
            <span class="mesh-thread-name">{{ s.title || "未命名对话" }}</span>
          </button>
        </div>
        <div v-if="!sessionRows.length && !creating" class="mesh-empty">还没有对话，点上面开一轮</div>
      </div>
    </aside>
    <section class="mesh-chat">
      <div class="mesh-chat-top">
        <button
          type="button"
          class="ghost mesh-nav-toggle"
          :aria-label="navOpen ? '收起侧边栏' : '展开侧边栏'"
          :title="navOpen ? '收起侧边栏' : '展开侧边栏'"
          @click="toggleNav"
        >
          <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <rect x="2.5" y="3" width="11" height="10" rx="1.6" stroke="currentColor" stroke-width="1.4" />
            <path d="M6.5 3v10" stroke="currentColor" stroke-width="1.4" />
          </svg>
        </button>
        <button
          type="button"
          class="mesh-chat-title"
          :class="{ live: meshLive.streaming, copied: copied, ended: turnEnded }"
          :title="copied ? '已复制标题和会话编号' : '点击复制标题和会话编号'"
          :aria-label="copied ? '已复制标题和会话编号' : '复制标题和会话编号'"
          @click="copyTitle"
        >
          <span
            v-if="meshLive.streaming"
            class="mesh-lamp"
            role="status"
            aria-label="正在输出"
          ></span>
          <span
            v-else-if="turnEnded"
            class="mesh-ended"
            role="status"
            aria-label="已结束"
          >已结束</span>
          <span class="mesh-chat-title-text">{{ copied ? "已复制" : chatTitle }}</span>
        </button>
        <button
          type="button"
          class="ghost mesh-diff-toggle"
          :class="{ on: diffOpen }"
          title="查看项目改动"
          aria-label="查看项目改动"
          @click="diffOpen = !diffOpen"
        >
          <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <path d="M4 3.5h8M4 8h5.5M4 12.5h8" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
            <path d="M11.2 6.2 13 8l-1.8 1.8" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
          改动
        </button>
      </div>
      <MeshDiffPanel :open="diffOpen" @close="diffOpen = false" />
      <div class="mesh-chat-main">
        <CopilotChat
          :key="chatKey"
          agent-id="default"
          :thread-id="chatThreadId"
          :attachments="attachConfig"
          :labels="labels"
        >
          <template #welcome-message>
            <div class="mesh-welcome">
              <h1>有什么可以帮你的？</h1>
              <p>{{ welcomeHint }}</p>
            </div>
          </template>
          <template #reasoning-message></template>
          <template #message-after="{ message }">
            <MeshThinkWindow v-if="showThinkAfter(message)" :streaming="true" />
          </template>
          <template #user-message="{ message }">
            <CopilotChatUserMessage :message="message">
              <template #message-renderer="{ content, isMultiline }">
                <div class="mesh-user-card" :data-multiline="isMultiline ? 'true' : undefined">
                  <MeshUserParts :content="message.content" />
                  <div v-if="content" class="mesh-user-text">{{ content }}</div>
                </div>
              </template>
              <template #copy-button="{ onCopy, copied, label }">
                <button
                  type="button"
                  class="mesh-copy"
                  :aria-label="label"
                  :title="copied ? '已复制' : label"
                  @click="onCopy"
                >
                  <svg v-if="copied" viewBox="0 0 16 16" fill="none" aria-hidden="true">
                    <path
                      d="M3.5 8.5 6.5 11.5 12.5 4.5"
                      stroke="currentColor"
                      stroke-width="1.6"
                      stroke-linecap="round"
                      stroke-linejoin="round"
                    />
                  </svg>
                  <svg v-else viewBox="0 0 16 16" fill="none" aria-hidden="true">
                    <rect x="5.5" y="5.5" width="8" height="8" rx="1.4" stroke="currentColor" stroke-width="1.4" />
                    <path
                      d="M3.5 10.5V3.8A1.3 1.3 0 0 1 4.8 2.5h6.7"
                      stroke="currentColor"
                      stroke-width="1.4"
                      stroke-linecap="round"
                    />
                  </svg>
                </button>
              </template>
            </CopilotChatUserMessage>
          </template>
          <template #tool-calls-view="{ message, messages }">
            <MeshToolCallsView :message="message" :messages="messages || []" />
          </template>
          <template #copy-button="{ onCopy, copied, label }">
            <button
              type="button"
              class="mesh-copy"
              :aria-label="label"
              :title="copied ? '已复制' : label"
              @click="onCopy"
            >
              <svg v-if="copied" viewBox="0 0 16 16" fill="none" aria-hidden="true">
                <path
                  d="M3.5 8.5 6.5 11.5 12.5 4.5"
                  stroke="currentColor"
                  stroke-width="1.6"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                />
              </svg>
              <svg v-else viewBox="0 0 16 16" fill="none" aria-hidden="true">
                <rect x="5.5" y="5.5" width="8" height="8" rx="1.4" stroke="currentColor" stroke-width="1.4" />
                <path
                  d="M3.5 10.5V3.8A1.3 1.3 0 0 1 4.8 2.5h6.7"
                  stroke="currentColor"
                  stroke-width="1.4"
                  stroke-linecap="round"
                />
              </svg>
            </button>
          </template>
          <template #input="slot">
            <div class="mesh-composer">
              <MeshThinkWindow v-if="thinkInComposer" :streaming="true" />
              <div class="mesh-composer-meta">
                <div class="mesh-chip">
                  <button type="button" class="mesh-chip-btn is-model" @click.stop="togglePick('model')">
                    <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
                      <circle cx="8" cy="8" r="2.2" stroke="currentColor" stroke-width="1.4" />
                      <path
                        d="M8 1.7v1.4M8 12.9v1.4M1.7 8h1.4M12.9 8h1.4M3.4 3.4l1 1M11.6 11.6l1 1M3.4 12.6l1-1M11.6 4.4l1-1"
                        stroke="currentColor"
                        stroke-width="1.3"
                        stroke-linecap="round"
                      />
                    </svg>
                    <span>{{ modelLabel(filters.model) }}</span>
                  </button>
                  <div v-if="openPick === 'model'" class="mesh-pop" role="listbox" aria-label="选择模型">
                    <button
                      v-for="m in modelOptions"
                      :key="m.id"
                      type="button"
                      class="mesh-pop-item"
                      :class="{ on: filters.model === m.id }"
                      @click.stop="pickModel(m.id)"
                    >
                      {{ modelLabel(m.id) }}
                    </button>
                    <div v-if="!modelOptions.length" class="mesh-pop-empty">暂无模型</div>
                  </div>
                </div>
                <div class="mesh-chip">
                  <button
                    type="button"
                    class="mesh-chip-btn is-dir"
                    :title="filters.dir || '选择工作目录'"
                    @click.stop="togglePick('dir')"
                  >
                    <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
                      <path
                        d="M2.5 4.2A1.2 1.2 0 0 1 3.7 3h3.1l1.2 1.5h4.3A1.2 1.2 0 0 1 13.5 5.7v6.1A1.2 1.2 0 0 1 12.3 13H3.7A1.2 1.2 0 0 1 2.5 11.8V4.2Z"
                        stroke="currentColor"
                        stroke-width="1.4"
                        stroke-linejoin="round"
                      />
                    </svg>
                    <span>{{ filters.dir ? dirBase(filters.dir) : "工作目录" }}</span>
                  </button>
                  <div v-if="openPick === 'dir'" class="mesh-pop" role="listbox" aria-label="选择工作目录">
                    <button
                      v-for="d in dirOptions"
                      :key="(d.node_id || '') + d.path + (d.engine || '')"
                      type="button"
                      class="mesh-pop-item"
                      :class="{ on: filters.dir === d.path }"
                      :title="d.path"
                      @click.stop="pickDir(d)"
                    >
                      <span class="mesh-pop-dir">{{ d.path }}</span>
                    </button>
                    <div v-if="!dirOptions.length" class="mesh-pop-empty">暂无工作目录</div>
                  </div>
                </div>
              </div>
              <MeshAskCard />
              <MeshFollowups
                :running="slot.isRunning"
                :can-stop="slot.canStop"
                @pull="slot.onUpdateModelValue"
                @stop="slot.onStop"
              />
              <MeshSlashInput
                :model-value="slot.modelValue"
                :is-running="slot.isRunning"
                :input-mode="slot.inputMode"
                :items="slashItems"
                :can-stop="slot.canStop"
                :can-add-file="slot.canAddFile"
                :can-transcribe="slot.canTranscribe"
                :file-count="fileCountOf(slot)"
                :on-update-model-value="slot.onUpdateModelValue"
                :on-submit-message="(text) => onComposerSubmit(slot, text)"
                :on-pull-followup="() => onComposerPull(slot)"
                :on-stop="slot.onStop"
                :on-add-file="slot.onAddFile"
                :on-start-transcribe="slot.onStartTranscribe"
                :on-cancel-transcribe="slot.onCancelTranscribe"
                :on-finish-transcribe="slot.onFinishTranscribe"
                :on-finish-transcribe-with-audio="slot.onFinishTranscribeWithAudio"
              />
            </div>
          </template>
        </CopilotChat>
      </div>
    </section>
    <div
      v-if="meshLink.state !== 'live'"
      class="mesh-link-veil"
      role="status"
      aria-live="polite"
    >
      <div class="mesh-link-card">
        <span class="mesh-lamp mesh-link-lamp" aria-hidden="true"></span>
        <p class="mesh-link-title">{{ linkHint }}</p>
        <p class="mesh-link-copy">连上之后会自动刷新对话、目录和进行中的进度。</p>
        <button type="button" class="mesh-link-reload" @click="reloadPage">刷新页面</button>
      </div>
    </div>
  </div>
</template>
