import { AbstractAgent, EventType } from "@ag-ui/client";
import type { BaseEvent, Message, RunAgentInput } from "@ag-ui/core";
import { Observable } from "rxjs";
import { reactive } from "vue";
import { clearAsk, noteAskEvent, noteAskEvents, noteAskFromTool } from "./ask";
import { keepMediaContent, lastUserPayload } from "./attach";
import { fetchJSON } from "./http";
import { showToast, toastError } from "./toast";
import { cleanUserText, ensureTurnThink, eventsToMessages, isInternalDump, lastTurnMessages, sessionIdFromChunk } from "./events";
import { meshTarget, parseThreadId } from "./target";
import type { MeshEvent } from "./types";

export const meshLive = reactive({ session: "", title: "", streaming: false, think: "", thinking: false });
export const chatLoad = reactive({ loading: false, thread: "" });

const FIRST_PAINT_TURNS = 2;
const historyCache = new Map<string, MeshEvent[]>();
let historyEvents: MeshEvent[] = [];
let historyThread = "";
let historySource: "none" | "http" | "ws" = "none";
let historyGen = 0;
let historyAbort: AbortController | null = null;

type LiveChunk = Record<string, unknown>;
type LiveFn = (obj: LiveChunk) => void;
const liveFns = new Set<LiveFn>();

export function onLiveChunk(fn: LiveFn): () => void {
  liveFns.add(fn);
  return () => {
    liveFns.delete(fn);
  };
}

function meshOf(obj: LiveChunk): {
  type?: string;
  tool?: string;
  preview?: string;
  session?: string;
  role?: string;
} {
  return (obj.agent_mesh || obj.mesh || {}) as {
    type?: string;
    tool?: string;
    preview?: string;
    session?: string;
    role?: string;
  };
}

function chunkDelta(obj: LiveChunk): string {
  const choices = obj.choices as { delta?: { content?: string } }[] | undefined;
  return choices?.[0]?.delta?.content || "";
}

function chunkTerminal(obj: LiveChunk): boolean {
  const choices = obj.choices as { finish_reason?: string | null }[] | undefined;
  const reason = choices?.[0]?.finish_reason || "";
  const kind = meshOf(obj).type || "";
  if (reason === "stop" || reason === "end") return true;
  return kind === "done" || kind === "result";
}

function noteSession(obj: LiveChunk): string {
  const sid = sessionIdFromChunk(obj);
  if (!sid) return meshLive.session;
  const mesh = meshOf(obj).session || "";
  if (mesh || !meshLive.session) {
    meshLive.session = sid;
    if (!meshTarget.session) meshTarget.session = sid;
  }
  return meshLive.session;
}

function cacheLiveThread(threadId: string): void {
  if (!threadId || !historyEvents.length) return;
  historyCache.set(threadId, historyEvents);
  if (!meshLive.session) return;
  const parsed = threadId.includes("|") ? parseThreadId(threadId) : { node: "", engine: "", session: "" };
  const real = [parsed.node || meshTarget.node, parsed.engine || meshTarget.engine, meshLive.session].join("|");
  if (real && real !== threadId) historyCache.set(real, historyEvents);
}

export function bindHistoryThread(threadId: string): void {
  if (!threadId) return;
  historyThread = threadId;
  cacheLiveThread(threadId);
}

export function pushLiveChunk(obj: LiveChunk): void {
  const mesh = meshOf(obj);
  noteSession(obj);
  if ((mesh.type || "") === "title") {
    const title = (chunkDelta(obj) || mesh.preview || "").replace(/\s+/g, " ").trim();
    if (title) meshLive.title = Array.from(title).slice(0, 80).join("");
  }
  for (const fn of liveFns) fn(obj);
}

function waitLive(signal: AbortSignal, onObj: (obj: LiveChunk) => boolean): Promise<void> {
  return new Promise((resolve) => {
    let settled = false;
    const finish = () => {
      if (settled) return;
      settled = true;
      off();
      signal.removeEventListener("abort", finish);
      resolve();
    };
    const off = onLiveChunk((obj) => {
      if (onObj(obj)) finish();
    });
    if (signal.aborted) {
      finish();
      return;
    }
    signal.addEventListener("abort", finish, { once: true });
  });
}

async function sessionIsActive(session: string): Promise<boolean> {
  if (!session) return false;
  const mod = await import("./catalog");
  return mod.catalog.sessions.some((s) => s.id === session && (s.live || s.busy));
}

async function waitTurn(signal: AbortSignal, onObj: (obj: LiveChunk) => boolean, session: string): Promise<void> {
  let finished = false;
  const live = waitLive(signal, (obj) => {
    const stop = onObj(obj);
    if (stop) finished = true;
    return stop;
  });
  const idle = (async () => {
    let seenActive = await sessionIsActive(session);
    const started = Date.now();
    while (!signal.aborted && !finished) {
      const active = await sessionIsActive(session);
      if (active) seenActive = true;
      if (seenActive && !active) return;
      if (!seenActive && Date.now() - started > 4000) return;
      await new Promise((r) => window.setTimeout(r, 400));
    }
  })();
  await Promise.race([live, idle]);
}

export function isLiveChat(): boolean {
  return meshLive.streaming;
}

function pushHasUser(text: string): boolean {
  const want = cleanUserText(text).replace(/\s+/g, " ").trim();
  if (!want) return false;
  for (let i = historyEvents.length - 1; i >= 0; i--) {
    const ev = historyEvents[i];
    const type = ev.type || "";
    if (type.includes("tool")) continue;
    if (type !== "user" && ev.role !== "user") continue;
    const got = cleanUserText(ev.text || "").replace(/\s+/g, " ").trim();
    if (got && (got === want || got.includes(want) || want.includes(got))) return true;
  }
  return false;
}

function redirectIfUnauthorized(resp: Response): void {
  if (resp.status === 401) {
    location.href = "/login";
    throw new Error("未登录");
  }
}

function modelFitsEngine(model: string, engine: string, node: string): boolean {
  if (!engine) return true;
  if (!model) return false;
  const parts = model.split("/");
  if (parts[0] === engine) return true;
  if (node && parts[0] === node && parts[1] === engine) return true;
  return false;
}

function chatModelOf(model: string, node: string, engine: string): string {
  if (modelFitsEngine(model, engine, node)) return model;
  if (node && engine) return node + "/" + engine;
  return engine || model;
}

function nextPiece(prev: string, incoming: string): string {
  if (!incoming) return "";
  if (prev && incoming.startsWith(prev)) return incoming.slice(prev.length);
  if (prev && prev.endsWith(incoming) && incoming.length < 8) return "";
  return incoming;
}

async function readSSE(
  resp: Response,
  onEvent: (obj: Record<string, unknown>) => boolean | void,
  signal: AbortSignal,
): Promise<void> {
  const reader = resp.body?.getReader();
  if (!reader) return;
  const dec = new TextDecoder();
  let buf = "";
  while (!signal.aborted) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += dec.decode(value, { stream: true });
    const parts = buf.split("\n\n");
    buf = parts.pop() || "";
    for (const part of parts) {
      const line = part.trim();
      if (!line.startsWith("data: ")) continue;
      const data = line.slice(6);
      if (data === "[DONE]") continue;
      try {
        if (onEvent(JSON.parse(data) as Record<string, unknown>)) return;
      } catch {
        /* skip malformed chunk */
      }
    }
  }
}

export class MeshAgent extends AbstractAgent {
  constructor() {
    super({ agentId: "default", description: "Agent Mesh 会话" });
  }

  clone(): MeshAgent {
    const next = new MeshAgent();
    next.threadId = this.threadId;
    next.messages = this.messages.slice();
    next.state = this.state;
    return next;
  }

  protected connect(input: RunAgentInput): Observable<BaseEvent> {
    return new Observable((sub) => {
      const runId = input.runId || crypto.randomUUID();
      const threadId = input.threadId || this.threadId;
      const cached = historyCache.get(threadId) || [];
      if (cached.length) {
        historyEvents = cached;
        historyThread = threadId;
      }
      // 正在流式输出或本轮已有消息时回放当前列表，避免空快照把对话清掉。
      const current = this.messages.slice();
      const snapshot = current.length
        ? current
        : meshLive.streaming
          ? current
          : cached.length
            ? lastTurnMessages(eventsToMessages(cached), FIRST_PAINT_TURNS)
            : [];
      sub.next({ type: EventType.RUN_STARTED, threadId, runId });
      sub.next({
        type: EventType.MESSAGES_SNAPSHOT,
        messages: snapshot,
      });
      sub.next({ type: EventType.RUN_FINISHED, threadId, runId });
      sub.complete();
      if (!meshLive.streaming) void refreshHistory(threadId);
      return () => {};
    });
  }

  run(input: RunAgentInput): Observable<BaseEvent> {
    return new Observable((sub) => {
      const ac = new AbortController();
      const runId = input.runId || crypto.randomUUID();
      const threadId = input.threadId || this.threadId;
      meshLive.streaming = true;
      meshLive.think = "";
      meshLive.thinking = false;
      let sentText = "";
      const finish = () => {
        meshLive.streaming = false;
        meshLive.thinking = false;
        if (historyEvents.length && pushHasUser(sentText)) {
          paintMessages(historyEvents, false);
        }
      };
      (async () => {
        sub.next({ type: EventType.RUN_STARTED, threadId, runId });
        const payload = lastUserPayload(input.messages || []);
        const text = payload.text || (payload.files.length ? "请查看附件。" : "");
        sentText = text;
        if (!text) {
          showToast("缺少用户文本", "error");
          sub.next({ type: EventType.RUN_ERROR, message: "缺少用户文本" });
          sub.next({ type: EventType.RUN_FINISHED, threadId, runId });
          sub.complete();
          return;
        }
        // 新建对话时 meshTarget.session 为空，不能沿用 CopilotChat 残留的旧 thread。
        const bound = threadId.includes("|") ? parseThreadId(threadId) : { node: "", engine: "", session: "" };
        const session = meshTarget.session;
        const engine = meshTarget.engine || bound.engine;
        const node = meshTarget.node || bound.node;
        const model = chatModelOf(meshTarget.model, node, engine);
        if (!model) {
          showToast("请先选择引擎或模型", "error");
          sub.next({ type: EventType.RUN_ERROR, message: "请先选择引擎或模型" });
          sub.next({ type: EventType.RUN_FINISHED, threadId, runId });
          sub.complete();
          return;
        }
        if (!session && !meshTarget.cwd) {
          showToast("请先选择工作目录", "error");
          sub.next({ type: EventType.RUN_ERROR, message: "请先选择工作目录" });
          sub.next({ type: EventType.RUN_FINISHED, threadId, runId });
          sub.complete();
          return;
        }
        const body: Record<string, unknown> = {
          model,
          stream: true,
          messages: [{ role: "user", content: text }],
        };
        if (engine) body.engine = engine;
        if (session) body.session = session;
        if (meshTarget.cwd) body.cwd = meshTarget.cwd;
        if (payload.files.length) body.files = payload.files;
        if (!session) {
          meshLive.session = "";
          meshLive.title = Array.from(text.replace(/\s+/g, " ").trim()).slice(0, 48).join("");
        }
        const resp = await fetchJSON("/v1/chat/completions", {
          method: "POST",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
          signal: ac.signal,
        }, 0);
        redirectIfUnauthorized(resp);
        if (!resp.ok) {
          throw new Error((await resp.text()) || "http " + resp.status);
        }
        const ctype = resp.headers.get("content-type") || "";
        let queued = false;
        if (ctype.includes("application/json") && !ctype.includes("text/event-stream")) {
          const data = (await resp.json()) as { queued?: boolean };
          queued = Boolean(data && data.queued);
        }
        let textId = "";
        let toolId = "";
        let toolName = "";
        let toolAcc = "";
        let lastClosedToolId = "";
        let lastClosedToolName = "";
        let thinkAcc = "";
        let sawContent = false;
        let ended = false;
        const closeText = () => {
          if (!textId) return;
          sub.next({ type: EventType.TEXT_MESSAGE_END, messageId: textId });
          textId = "";
        };
        const closeTool = () => {
          if (!toolId) return;
          sub.next({ type: EventType.TOOL_CALL_END, toolCallId: toolId });
          lastClosedToolId = toolId;
          lastClosedToolName = toolName;
          toolId = "";
          toolName = "";
          toolAcc = "";
        };
        const ensureText = () => {
          if (textId) return;
          textId = crypto.randomUUID();
          sub.next({
            type: EventType.TEXT_MESSAGE_START,
            messageId: textId,
            role: "assistant",
          });
        };
        const feed = (obj: LiveChunk): boolean => {
          const mesh = meshOf(obj);
          const delta = chunkDelta(obj);
          const kind = mesh.type || (mesh.tool ? "tool_call" : "assistant");
          if (noteSession(obj)) cacheLiveThread(threadId);
          const stop = (): boolean => {
            const done = chunkTerminal(obj);
            if (done) ended = true;
            return done;
          };
          if (kind === "user" || mesh.role === "user") {
            noteAskEvent({ type: "user", role: "user", text: (delta || "").trim() });
            return stop();
          }
          if (kind === "session" || kind === "system") {
            cacheLiveThread(threadId);
            return stop();
          }
          if (kind === "title") {
            const title = (delta || mesh.preview || "").replace(/\s+/g, " ").trim();
            if (title) meshLive.title = Array.from(title).slice(0, 80).join("");
            return false;
          }
          if (kind === "ask") {
            meshLive.thinking = false;
            sawContent = true;
            noteAskEvent({
              type: "ask",
              tool: mesh.tool || "",
              text: (delta || "").trim(),
              preview: mesh.preview || "",
            });
            return false;
          }
          if (
            kind === "thinking" ||
            kind === "reasoning" ||
            kind === "thought" ||
            kind === "thinking_delta"
          ) {
            sawContent = true;
            closeTool();
            closeText();
            const piece = nextPiece(thinkAcc, delta);
            thinkAcc += piece;
            if (piece) {
              meshLive.think += piece;
              meshLive.thinking = true;
            }
            return false;
          }
          if (kind === "tool_result" || kind === "tool-result") {
            meshLive.thinking = false;
            sawContent = true;
            closeText();
            const name = mesh.tool || toolName || lastClosedToolName || "tool";
            if (toolId) closeTool();
            let id = lastClosedToolId;
            if (!id || (lastClosedToolName && name && lastClosedToolName !== name)) {
              id = crypto.randomUUID();
              sub.next({
                type: EventType.TOOL_CALL_START,
                toolCallId: id,
                toolCallName: name,
              });
              sub.next({ type: EventType.TOOL_CALL_END, toolCallId: id });
              lastClosedToolId = id;
              lastClosedToolName = name;
            }
            const content = delta || mesh.preview || "";
            sub.next({
              type: EventType.TOOL_CALL_RESULT,
              toolCallId: id,
              messageId: crypto.randomUUID(),
              content,
              role: "tool",
            });
            noteAskEvent({ type: "tool_result", tool: name, text: content });
            return false;
          }
          if (kind === "tool_call" || kind === "tool_use" || kind === "tool_call_update") {
            meshLive.thinking = false;
            sawContent = true;
            closeText();
            const name = mesh.tool || "tool";
            const args = delta || mesh.preview || "";
            if (toolId && toolName !== name) closeTool();
            if (!toolId) {
              toolId = crypto.randomUUID();
              toolName = name;
              toolAcc = "";
              sub.next({
                type: EventType.TOOL_CALL_START,
                toolCallId: toolId,
                toolCallName: name,
              });
            }
            const piece = nextPiece(toolAcc, args);
            toolAcc += piece;
            if (piece) {
              sub.next({ type: EventType.TOOL_CALL_ARGS, toolCallId: toolId, delta: piece });
            }
            noteAskFromTool(name, toolAcc, toolId);
            noteAskEvent({ type: "tool_call", tool: name, text: toolAcc });
            return false;
          }
          if (!delta) return stop();
          if (isInternalDump(delta)) return stop();
          meshLive.thinking = false;
          sawContent = true;
          closeTool();
          ensureText();
          sub.next({
            type: EventType.TEXT_MESSAGE_CONTENT,
            messageId: textId,
            delta,
          });
          noteAskEvent({ type: "assistant", text: delta });
          return stop();
        };
        if (!queued) {
          await readSSE(resp, feed, ac.signal);
        }
        if (!ac.signal.aborted && !ended && queued) {
          await waitTurn(ac.signal, feed, meshLive.session || session);
        } else if (!ac.signal.aborted && !ended && !sawContent && (meshLive.session || session)) {
          await waitTurn(ac.signal, feed, meshLive.session || session);
        }
        closeTool();
        closeText();
        if (thinkAcc.trim()) {
          const id = crypto.randomUUID();
          sub.next({ type: EventType.REASONING_START, messageId: id });
          sub.next({ type: EventType.REASONING_MESSAGE_START, messageId: id, role: "reasoning" });
          sub.next({ type: EventType.REASONING_MESSAGE_CONTENT, messageId: id, delta: thinkAcc });
          sub.next({ type: EventType.REASONING_MESSAGE_END, messageId: id });
          sub.next({ type: EventType.REASONING_END, messageId: id });
          historyEvents = historyEvents.concat([{ type: "thinking", text: thinkAcc }]);
          if (threadId) historyCache.set(threadId, historyEvents);
        }
        cacheLiveThread(threadId);
        sub.next({ type: EventType.RUN_FINISHED, threadId, runId });
        sub.complete();
      })()
        .catch((err: unknown) => {
          toastError(err);
          sub.next({
            type: EventType.RUN_ERROR,
            message: err instanceof Error ? err.message : String(err),
          });
          sub.next({ type: EventType.RUN_FINISHED, threadId, runId });
          sub.complete();
        })
        .finally(finish);
      return () => {
        finish();
        ac.abort();
      };
    });
  }
}

export const meshAgent = new MeshAgent();

export function resetDraft(): void {
  historyAbort?.abort();
  historyAbort = null;
  historyGen++;
  historyEvents = [];
  historyThread = "";
  historySource = "none";
  chatLoad.loading = false;
  chatLoad.thread = "";
  meshLive.session = "";
  meshLive.title = "";
  meshLive.streaming = false;
  meshLive.think = "";
  meshLive.thinking = false;
  clearAsk();
  try {
    if (meshAgent.isRunning) meshAgent.abortRun();
  } catch {
    /* 没有进行中的请求 */
  }
  meshAgent.threadId = "";
  meshAgent.setMessages([]);
}

export function expectHistory(threadId: string): void {
  if (!threadId) return;
  chatLoad.thread = threadId;
  const cached = historyCache.get(threadId);
  chatLoad.loading = !cached || cached.length === 0;
  if (cached && cached.length) {
    historyEvents = cached;
    historyThread = threadId;
    noteAskEvents(cached, true);
    paintMessages(cached, true);
  } else {
    clearAsk();
  }
  void refreshHistory(threadId);
}

function paintMessages(evs: MeshEvent[], progressive: boolean): void {
  if (!meshLive.streaming) noteAskEvents(evs, true);
  const prev = meshAgent.messages.slice();
  const all = ensureTurnThink(keepMediaContent(eventsToMessages(evs), prev), meshLive.think);
  const first = progressive ? lastTurnMessages(all, FIRST_PAINT_TURNS) : all;
  if (!meshLive.streaming) meshAgent.setMessages(first);
  chatLoad.loading = false;
  if (!progressive || first.length >= all.length) return;
  requestAnimationFrame(() => {
    requestAnimationFrame(() => {
      if (meshLive.streaming) return;
      if (historyEvents !== evs) return;
      meshAgent.setMessages(all);
    });
  });
}

function keepLiveTranscript(evs: MeshEvent[]): boolean {
  return (!evs || evs.length === 0) && (meshLive.streaming || meshAgent.messages.length > 0);
}

export function applyHistory(evs: MeshEvent[], reset: boolean): void {
  const incoming = evs || [];
  // 新建会话的历史可能尚为空；空快照不能覆盖正在流式输出的对话。
  if (reset && keepLiveTranscript(incoming)) return;
  noteAskEvents(incoming, reset);
  const thread = meshAgent.threadId || historyThread;
  if (reset) historyEvents = incoming.slice();
  else historyEvents = historyEvents.concat(incoming);
  historySource = "ws";
  historyThread = thread;
  if (thread) historyCache.set(thread, historyEvents);
  paintMessages(historyEvents, reset);
}

export async function refreshHistory(threadId: string, signal?: AbortSignal): Promise<void> {
  const { node, engine, session } = parseThreadId(threadId);
  if (!session) {
    chatLoad.loading = false;
    chatLoad.thread = "";
    return;
  }
  if (historyThread !== threadId) historySource = "none";
  const cached = historyCache.get(threadId);
  chatLoad.thread = threadId;
  chatLoad.loading = !cached || cached.length === 0;
  if (cached && cached.length) {
    historyEvents = cached;
    historyThread = threadId;
    paintMessages(cached, true);
  }
  historyAbort?.abort();
  const ac = new AbortController();
  historyAbort = ac;
  if (signal) {
    if (signal.aborted) ac.abort();
    else signal.addEventListener("abort", () => ac.abort(), { once: true });
  }
  const gen = ++historyGen;
  try {
    const q = new URLSearchParams({
      node,
      engine,
      session,
      tail: "8000",
      turns: "20",
    });
    const resp = await fetchJSON("/v1/openai/history?" + q.toString(), {
      credentials: "same-origin",
      signal: ac.signal,
    });
    redirectIfUnauthorized(resp);
    if (!resp.ok) throw new Error("http " + resp.status);
    const evs = (await resp.json()) as MeshEvent[];
    if (gen !== historyGen) return;
    if (historySource === "ws" && historyThread === threadId && historyEvents.length) {
      chatLoad.loading = false;
      return;
    }
    const rows = Array.isArray(evs) ? evs : [];
    if (keepLiveTranscript(rows)) {
      chatLoad.loading = false;
      return;
    }
    historyEvents = rows;
    historyThread = threadId;
    historySource = "http";
    historyCache.set(threadId, historyEvents);
    paintMessages(historyEvents, true);
  } catch (err) {
    if (ac.signal.aborted || gen !== historyGen) return;
    chatLoad.loading = false;
    toastError(err);
  }
}
