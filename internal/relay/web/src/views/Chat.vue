<script setup lang="ts">
import { CopilotChat, CopilotChatUserMessage, useDefaultRenderTool } from "@copilotkit/vue/v2";
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { catalog, connectCatalog, meshLink, refreshCatalog, watchSession } from "../catalog";
import MeshAskCard from "../components/MeshAskCard.vue";
import MeshAttachQueue from "../components/MeshAttachQueue.vue";
import MeshDiffPanel from "../components/MeshDiffPanel.vue";
import MeshFollowups from "../components/MeshFollowups.vue";
import MeshSettings from "../components/MeshSettings.vue";
import MeshSlashInput from "../components/MeshSlashInput.vue";
import MeshThinkWindow from "../components/MeshThinkWindow.vue";
import MeshToolCallsView from "../components/MeshToolCallsView.vue";
import MeshToolWindow from "../components/MeshToolWindow.vue";
import MeshUserParts from "../components/MeshUserParts.vue";
import { fetchJSON } from "../http";
import { engineEmptyHint, nodeEngines, nodeMissingEngines } from "../engineCards";
import { capsAllowDiff, capsAllowFollowups, capsBadges, engineCaps } from "../engineCaps";
import { formatEngineInstall } from "../engineInstall";
import { resolveHealthPick } from "../healthMatrix";
import { loadLastPick, pickToQuery, saveLastPick } from "../lastPick";
import { liveHintMode } from "../liveFlags";
import { bindHistoryThread, expectHistory, meshAgent, meshLive, resetDraft } from "../meshAgent";
import {
  cycleLocale,
  localeButtonLabel,
  localeButtonTitle,
  localePref,
  t,
} from "../locale";
import { filterNodesByOs, nodePlatforms } from "../nodeFilter";
import { platformLabel } from "../platform";
import { loadTheme, toggleTheme } from "../theme";
import { pushFollowup, pullLastFollowup, shouldQueueFollowup, watchFollowups } from "../followup";
import { slashItems, watchSlashCatalog } from "../slash";
import { meshTarget, threadIdOf } from "../target";
import { clearAttachJobs, onComposerUploadFailed, uploadComposerFile } from "../attachUpload";
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
const settingsOpen = ref(false);
/** Gear opens relay settings (local node) and/or the engines×nodes health matrix. */
const meshSettings = ref(true);
const picksOpen = reactive({ node: false, engine: false });
const composerGuarded = ref(false);
let composerGuardTimer = 0;

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

function closeNavIfMobile(afterClick = false): void {
  if (!isMobileNav()) return;
  if (!afterClick) {
    navOpen.value = false;
    return;
  }
  // wait until this click finishes so closing the drawer does not punch through to the composer and open the keyboard
  window.setTimeout(() => {
    navOpen.value = false;
  }, 500);
}

function guardComposer(): void {
  composerGuarded.value = true;
  window.clearTimeout(composerGuardTimer);
  composerGuardTimer = window.setTimeout(() => {
    composerGuarded.value = false;
  }, 700);
}

const labels = computed(() => ({
  chatInputPlaceholder: t("chat.placeholder"),
  welcomeMessageText: t("chat.welcome"),
  chatDisclaimerText: t("chat.disclaimer"),
  modalHeaderTitle: t("chat.title"),
  reasoningMessageThinkingLabel: t("chat.thinking"),
  chatInputToolbarAddButtonLabel: t("chat.addFiles"),
}));

const copied = ref(false);
let copiedTimer = 0;

const linkHint = computed(() => {
  if (meshLink.state === "live") return "";
  if (meshLink.state === "connecting") return t("link.connecting");
  return t("link.disconnected");
});

const attachConfig = {
  enabled: true,
  maxSize: 20 * 1024 * 1024,
  onUpload: uploadComposerFile,
  onUploadFailed: onComposerUploadFailed,
};

const filters = reactive({
  node: String(route.query.node || savedPick?.node || ""),
  engine: String(route.query.engine || savedPick?.engine || ""),
  model: String(route.query.model || savedPick?.model || ""),
  dir: String(route.query.dir || savedPick?.dir || ""),
});

const osFilter = ref(String(route.query.os || savedPick?.os || ""));

const engineCards = computed(() => nodeEngines(catalog.engines, filters.node));

const missingEngines = computed(() => nodeMissingEngines(catalog.engines, filters.node));

const selectedNodeOs = computed(() => catalog.nodes.find((row) => row.id === filters.node)?.os || "");

const engineHint = computed(() => (engineCards.value.length ? "" : engineEmptyHint(filters.node)));

const selectedCaps = computed(() => engineCaps(catalog.engines, filters.node, filters.engine));

const showDiff = computed(() => capsAllowDiff(selectedCaps.value));

const showFollowups = computed(() => capsAllowFollowups(selectedCaps.value));

const platformOptions = computed(() => nodePlatforms(catalog.nodes));

const visibleNodes = computed(() => filterNodesByOs(catalog.nodes, osFilter.value));

const nodePickLabel = computed(() => {
  const n = catalog.nodes.find((row) => row.id === filters.node);
  return n ? nodeName(n.id, n.name) : t("nav.none");
});

const enginePickLabel = computed(() => {
  const e = engineCards.value.find((row) => row.name === filters.engine);
  return e ? e.title || e.name : t("nav.none");
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
  if (sessionStreaming(s)) return Date.now() / 1000;
  return s.updated_at || 0;
}

function sessionListable(s: Session): boolean {
  if (selected.value && selected.value.id === s.id && selected.value.engine === s.engine) return true;
  if (s.busy || sessionStreaming(s)) return true;
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

type SessionGroup = { key: string; label: string; items: Session[] };

const sessionGroups = computed((): SessionGroup[] => {
  const now = new Date();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const yesterday = today - 86400000;
  const week = today - 6 * 86400000;
  const groups: SessionGroup[] = [
    { key: "today", label: t("group.today"), items: [] },
    { key: "yesterday", label: t("group.yesterday"), items: [] },
    { key: "week", label: t("group.week"), items: [] },
    { key: "older", label: t("group.older"), items: [] },
  ];
  for (const s of sessionRows.value) {
    const stamp = sessionStamp(s) * 1000;
    if (stamp >= today) groups[0].items.push(s);
    else if (stamp >= yesterday) groups[1].items.push(s);
    else if (stamp >= week) groups[2].items.push(s);
    else groups[3].items.push(s);
  }
  return groups.filter((g) => g.items.length || (creating.value && g.key === "today"));
});

const threadId = computed(() => {
  const s = selected.value;
  if (!s) return "";
  return threadIdOf(s.node_id || filters.node, s.engine, s.id);
});

const isDraftChat = computed(() => chatKey.value === "draft" || chatKey.value.startsWith("draft-"));
const chatThreadId = computed(() => (isDraftChat.value ? chatKey.value : threadId.value || undefined));

const chatTitle = computed(() => {
  if (creating.value) return meshLive.title || t("chat.new");
  return selected.value?.title || meshLive.title || t("chat.title");
});

const welcomeHint = computed(() => {
  const node = catalog.nodes.find((n) => n.id === filters.node);
  const engine = engineCards.value.find((e) => e.name === filters.engine);
  const nodeLabel = nodeName(filters.node, node?.name);
  const engineLabel = engine?.title || engine?.name || "";
  if (engineLabel && filters.node) return t("chat.willSend", { engine: engineLabel, node: nodeLabel });
  if (filters.node && !engineCards.value.length) {
    return t("chat.noEnginesOn", { node: nodeLabel });
  }
  if (filters.node) return t("chat.nodeLabel", { node: nodeLabel });
  return t("chat.pickNodeEngine");
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
  if (!s) return false;
  if (s.busy) return true;
  if (!meshLive.streaming) return false;
  if (meshLive.session) return s.id === meshLive.session;
  if (creating.value && !s.id) return true;
  return !!(
    selected.value &&
    selected.value.id === s.id &&
    selected.value.engine === s.engine &&
    (selected.value.node_id || "") === (s.node_id || "")
  );
}

const chatStreaming = computed(() => {
  if (creating.value && !selected.value?.id) return meshLive.streaming;
  return sessionStreaming(selected.value);
});

const liveHint = computed(() =>
  liveHintMode({ streaming: chatStreaming.value, thinking: meshLive.thinking }),
);

function liveHintAfter(message: { id?: string }): boolean {
  if (liveHint.value === "off") return false;
  const rows = meshAgent.messages || [];
  if (!rows.length) return false;
  return message.id === rows[rows.length - 1]?.id;
}

function thinkHintAfter(message: { id?: string }): boolean {
  return liveHint.value === "think" && liveHintAfter(message);
}

function nodeName(id: string, name?: string): string {
  return name || id || t("chat.nodeFallback");
}

function modelLabel(id: string): string {
  if (!id) return t("chat.pickModel");
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
    os: osFilter.value || "",
  };
  saveLastPick(pick);
  router.replace({ path: "/", query: pickToQuery(pick) });
}

function setOsFilter(os: string): void {
  osFilter.value = os;
  syncQuery(selected.value);
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
  guardComposer();
  closeNavIfMobile(true);
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

function onHealthPick(p: { nodeId: string; engine?: string; focusMissing?: boolean }): void {
  const next = resolveHealthPick(catalog.engines, p.nodeId, p.engine);
  filters.node = next.node;
  filters.engine = p.focusMissing ? "" : next.engine;
  syncModelToEngine();
  ensureWorkDir();
  if (p.focusMissing) {
    picksOpen.node = false;
    picksOpen.engine = true;
    navOpen.value = true;
    syncQuery(selected.value);
  } else if (filters.engine) {
    openLatestSessionOrNew();
  } else {
    syncQuery(selected.value);
  }
  settingsOpen.value = false;
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
      title: engine || "session",
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

watch(platformOptions, (opts) => {
  if (osFilter.value && !opts.includes(osFilter.value)) {
    setOsFilter("");
  }
});

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
        title: meshLive.title || t("chat.new"),
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
    // draft chats stay on a draft-* thread; changing thread-id mid-turn aborts the first stream.
    if (isDraftChat.value) return;
    meshAgent.threadId = threadId.value;
  },
  { immediate: true },
);

watch(
  threadId,
  (tid) => {
    if (isDraftChat.value || !tid) return;
    expectHistory(tid);
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
  () => [filters.node, catalog.engines] as const,
  () => {
    if (!filters.engine) return;
    if (engineCards.value.some((e) => e.name === filters.engine)) return;
    filters.engine = engineCards.value[0]?.name || "";
    syncModelToEngine();
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

watch(chatKey, () => {
  clearAttachJobs();
});

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

function composerAttachments(slot: object): {
  id: string;
  filename?: string;
  size?: number;
  status?: string;
  type?: string;
  source?: { type: string; value: string; mimeType?: string };
}[] {
  const rows = (slot as { attachments?: unknown }).attachments;
  return Array.isArray(rows) ? rows : [];
}

function removeComposerAttach(id: string, rows: { id?: string }[]): void {
  const i = rows.findIndex((row) => row.id === id);
  if (i < 0) return;
  const queue = document.querySelector('[data-testid="copilot-chat-attachment-queue"]');
  const item = queue?.querySelectorAll('[data-testid="copilot-chat-attachment-item"]')[i];
  const buttons = item?.querySelectorAll("button");
  const btn = buttons?.[buttons.length - 1];
  if (btn instanceof HTMLButtonElement) btn.click();
}

function composerBusy(slot: { isRunning?: boolean }): boolean {
  if (slot.isRunning) return true;
  return chatStreaming.value;
}

function stopTurn(slot: { onStop?: () => void }): void {
  try {
    slot.onStop?.();
  } catch {
    /* CopilotKit did not attach stop */
  }
  try {
    if (meshAgent.isRunning) meshAgent.abortRun();
  } catch {
    /* no in-flight request */
  }
}

function onComposerSubmit(
  slot: {
    isRunning: boolean;
    onSubmitMessage: (value: string) => void;
    onUpdateModelValue: (value: string) => void;
  },
  text: string,
): void {
  if (!fileCountOf(slot) && shouldQueueFollowup(composerBusy(slot))) {
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
}

function hideKitCursor(): void {
  document.querySelectorAll('[data-testid="copilot-loading-cursor"]').forEach((node) => {
    const el = node as HTMLElement;
    el.hidden = true;
    el.setAttribute("aria-hidden", "true");
    el.style.setProperty("display", "none", "important");
  });
}

let cursorObs: MutationObserver | null = null;

onMounted(() => {
  hydrateFromQuery();
  connectCatalog();
  document.addEventListener("click", onDocClick);
  window.addEventListener("mesh-link-up", onLinkUp);
  hideKitCursor();
  cursorObs = new MutationObserver(hideKitCursor);
  const host = document.querySelector(".mesh-chat") || document.body;
  cursorObs.observe(host, { childList: true, subtree: true });
});

onBeforeUnmount(() => {
  document.removeEventListener("click", onDocClick);
  window.removeEventListener("mesh-link-up", onLinkUp);
  window.clearTimeout(copiedTimer);
  window.clearTimeout(endedTimer);
  window.clearTimeout(composerGuardTimer);
  cursorObs?.disconnect();
  cursorObs = null;
  clearAttachJobs();
});
</script>

<style>
[data-testid="copilot-loading-cursor"] {
  display: none !important;
  visibility: hidden !important;
  width: 0 !important;
  height: 0 !important;
  overflow: hidden !important;
  animation: none !important;
}
</style>

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
          v-if="meshSettings"
          type="button"
          class="ghost mesh-theme-btn"
          :title="t('nav.nodeSettings')"
          :aria-label="t('nav.nodeSettings')"
          @click="settingsOpen = true"
        >
          <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <circle cx="8" cy="8" r="2.2" stroke="currentColor" stroke-width="1.4" />
            <path
              d="M8 1.7v1.3M8 13v1.3M1.7 8h1.3M13 8h1.3M3.2 3.2l.9.9M12 12l.9.9M3.2 12.8l.9-.9M12 4l.9-.9"
              stroke="currentColor"
              stroke-width="1.4"
              stroke-linecap="round"
            />
          </svg>
        </button>
        <button
          type="button"
          class="ghost mesh-theme-btn"
          :title="theme === 'dark' ? t('nav.themeLight') : t('nav.themeDark')"
          :aria-label="theme === 'dark' ? t('nav.themeLight') : t('nav.themeDark')"
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
        <button
          type="button"
          class="ghost mesh-theme-btn mesh-locale-btn"
          :title="localeButtonTitle(localePref)"
          :aria-label="localeButtonTitle(localePref)"
          @click="cycleLocale"
        >
          <span>{{ localeButtonLabel(localePref) }}</span>
        </button>
        <form method="post" action="/logout">
          <button type="submit" class="ghost">{{ t("nav.signOut") }}</button>
        </form>
        <button type="button" class="ghost mesh-nav-close" :aria-label="t('nav.closeSidebar')" @click="closeNav">
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
          <span class="mesh-pick-kicker">{{ t("nav.node") }}</span>
          <span class="mesh-pick-current">{{ nodePickLabel }}</span>
          <svg class="mesh-pick-chevron" viewBox="0 0 12 12" fill="none" aria-hidden="true">
            <path d="M4.2 2.4 8.4 6 4.2 9.6" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
        <div v-if="picksOpen.node" id="mesh-node-cards" class="mesh-cards">
          <div v-if="platformOptions.length > 1" class="mesh-os-filter" role="group" :aria-label="t('nav.filterOs')">
            <button
              type="button"
              class="mesh-os-chip"
              :class="{ sel: !osFilter }"
              @click="setOsFilter('')"
            >
              {{ t("nav.filterAll") }}
            </button>
            <button
              v-for="os in platformOptions"
              :key="os"
              type="button"
              class="mesh-os-chip"
              :class="{ sel: osFilter === os }"
              @click="setOsFilter(os)"
            >
              {{ platformLabel({ os }) || os }}
            </button>
          </div>
          <button
            v-for="n in visibleNodes"
            :key="n.id"
            type="button"
            class="mesh-card mesh-card-node"
            :class="{ sel: filters.node === n.id }"
            @click="pickNode(n.id)"
          >
            <span class="mesh-card-title">{{ nodeName(n.id, n.name) }}</span>
            <span v-if="platformLabel(n)" class="mesh-card-meta">{{ platformLabel(n) }}</span>
          </button>
          <div v-if="!visibleNodes.length" class="mesh-pick-empty">{{ t("nav.connectNode") }}</div>
        </div>
        <button
          type="button"
          class="mesh-pick-toggle"
          :class="{ open: picksOpen.engine }"
          :aria-expanded="picksOpen.engine"
          aria-controls="mesh-engine-cards"
          @click="picksOpen.engine = !picksOpen.engine"
        >
          <span class="mesh-pick-kicker">{{ t("nav.engine") }}</span>
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
            class="mesh-card mesh-card-engine"
            :class="{ sel: filters.engine === e.name }"
            @click="pickEngine(e.name)"
          >
            <span class="mesh-card-title">{{ e.title || e.name }}</span>
            <span v-if="capsBadges(e.caps).length" class="mesh-card-caps">
              <span v-for="b in capsBadges(e.caps)" :key="e.name + b" class="mesh-cap">{{ b }}</span>
            </span>
          </button>
          <div v-if="engineHint" class="mesh-pick-empty">{{ engineHint }}</div>
          <div v-if="missingEngines.length" class="mesh-engine-hints">
            <div v-for="e in missingEngines" :key="'miss-' + e.name" class="mesh-engine-hint">
              <div class="mesh-engine-hint-title">
                <span>{{ e.title || e.name }}</span>
                <span class="mesh-engine-hint-badge">{{ t("engine.missing") }}</span>
              </div>
              <p>{{ formatEngineInstall(e, selectedNodeOs) }}</p>
            </div>
          </div>
        </div>
      </div>
      <button class="mesh-new" type="button" @click="newSession">
        <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
          <path d="M8 3.2v9.6M3.2 8h9.6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
        </svg>
        {{ t("nav.newChat") }}
      </button>
      <div class="mesh-drawer-toolbar">
        <span>{{ t("nav.chat") }}</span>
        <button class="ghost" type="button" @click="refreshCatalog">{{ t("nav.refresh") }}</button>
      </div>
      <div class="mesh-threads">
        <div v-for="g in sessionGroups" :key="g.key" class="mesh-time-group">
          <div class="mesh-time-label">{{ g.label }}</div>
          <button
            v-if="creating && !selected?.id && g.key === 'today'"
            type="button"
            class="mesh-thread sel"
            :class="{ live: chatStreaming }"
          >
            <span v-if="chatStreaming" class="mesh-lamp" aria-hidden="true"></span>
            <span class="mesh-thread-name">{{ meshLive.title || t("chat.new") }}</span>
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
            <span class="mesh-thread-name">{{ s.title || t("nav.untitled") }}</span>
          </button>
        </div>
        <div v-if="!sessionRows.length && !creating" class="mesh-empty">{{ t("nav.noChats") }}</div>
      </div>
    </aside>
    <section class="mesh-chat">
      <div class="mesh-chat-top">
        <button
          type="button"
          class="ghost mesh-nav-toggle"
          :aria-label="navOpen ? t('nav.hideSidebar') : t('nav.showSidebar')"
          :title="navOpen ? t('nav.hideSidebar') : t('nav.showSidebar')"
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
          :class="{ live: liveHint === 'live', copied: copied, ended: turnEnded }"
          :title="copied ? t('chat.copiedTitle') : t('chat.copyTitle')"
          :aria-label="copied ? t('chat.copiedTitle') : t('chat.copyTitle')"
          @click="copyTitle"
        >
          <span
            v-if="liveHint === 'live'"
            class="mesh-lamp"
            role="status"
            :aria-label="t('chat.streaming')"
          ></span>
          <span
            v-else-if="turnEnded"
            class="mesh-ended"
            role="status"
            :aria-label="t('chat.done')"
          >{{ t("chat.done") }}</span>
          <span class="mesh-chat-title-text">{{ copied ? t("chat.copied") : chatTitle }}</span>
        </button>
        <button
          v-if="showDiff"
          type="button"
          class="ghost mesh-diff-toggle"
          :class="{ on: diffOpen }"
          :title="t('chat.viewDiff')"
          :aria-label="t('chat.viewDiff')"
          @click="diffOpen = !diffOpen"
        >
          <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <path d="M4 3.5h8M4 8h5.5M4 12.5h8" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
            <path d="M11.2 6.2 13 8l-1.8 1.8" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
          {{ t("chat.diff") }}
        </button>
      </div>
      <MeshDiffPanel v-if="showDiff" :open="diffOpen" @close="diffOpen = false" />
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
              <h1>{{ t("chat.welcome") }}</h1>
              <p>{{ welcomeHint }}</p>
            </div>
          </template>
          <template #reasoning-message>
            <div class="mesh-reason-skip" hidden></div>
          </template>
          <template #message-after="{ message }">
            <MeshThinkWindow v-if="thinkHintAfter(message)" mode="think" />
          </template>
          <template #cursor>
            <span class="mesh-cursor-slot" hidden></span>
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
                  :title="copied ? t('chat.copied') : label"
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
              :title="copied ? t('chat.copied') : label"
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
            <div class="mesh-composer" :class="{ 'is-guarded': composerGuarded }">
              <div v-if="composerGuarded" class="mesh-composer-guard" aria-hidden="true"></div>
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
                  <div v-if="openPick === 'model'" class="mesh-pop" role="listbox" :aria-label="t('chat.pickModel')">
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
                    <div v-if="!modelOptions.length" class="mesh-pop-empty">{{ t("chat.noModels") }}</div>
                  </div>
                </div>
                <div class="mesh-chip">
                  <button
                    type="button"
                    class="mesh-chip-btn is-dir"
                    :title="filters.dir || t('chat.pickDir')"
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
                    <span>{{ filters.dir ? dirBase(filters.dir) : t("chat.workspace") }}</span>
                  </button>
                  <div v-if="openPick === 'dir'" class="mesh-pop" role="listbox" :aria-label="t('chat.pickDir')">
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
                    <div v-if="!dirOptions.length" class="mesh-pop-empty">{{ t("chat.noWorkspaces") }}</div>
                  </div>
                </div>
              </div>
              <MeshAskCard />
              <MeshFollowups
                v-if="showFollowups"
                :running="composerBusy(slot)"
                :can-stop="composerBusy(slot)"
                @pull="slot.onUpdateModelValue"
                @stop="stopTurn(slot)"
              />
              <MeshAttachQueue
                :items="composerAttachments(slot)"
                @remove="(id) => removeComposerAttach(id, composerAttachments(slot))"
              />
              <MeshSlashInput
                :model-value="slot.modelValue"
                :is-running="composerBusy(slot)"
                :input-mode="slot.inputMode"
                :items="slashItems"
                :can-stop="composerBusy(slot)"
                :can-add-file="slot.canAddFile"
                :can-transcribe="slot.canTranscribe"
                :file-count="fileCountOf(slot)"
                :auto-focus="chatKey.startsWith('draft-')"
                :on-update-model-value="slot.onUpdateModelValue"
                :on-submit-message="(text) => onComposerSubmit(slot, text)"
                :on-pull-followup="() => onComposerPull(slot)"
                :on-stop="() => stopTurn(slot)"
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
        <p class="mesh-link-copy">{{ t("link.copy") }}</p>
        <button type="button" class="mesh-link-reload" @click="reloadPage">{{ t("link.reload") }}</button>
      </div>
    </div>
    <MeshSettings v-model:open="settingsOpen" @pick="onHealthPick" />
  </div>
</template>
