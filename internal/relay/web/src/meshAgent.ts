import { AbstractAgent, EventType } from "@ag-ui/client";
import type { BaseEvent, Message, RunAgentInput } from "@ag-ui/core";
import { Observable } from "rxjs";
import { reactive } from "vue";
import { clearAsk, noteAskEvent, noteAskEvents, noteAskFromTool } from "./ask";
import { keepMediaContent, lastUserPayload } from "./attach";
import { fetchJSON } from "./http";
import { showToast, toastError, unwrapFailText } from "./toast";
import { cleanUserText, ensureTurnThink, eventsToMessages, isInternalDump, lastTurnMessages, sameEvents, sameMessages, sessionIdFromChunk } from "./events";
import { chatModelOf } from "./chatModel";
import { flagsAfterBusy, flagsAfterChunk, flagsAfterEvents } from "./liveFlags";
import { meshTarget, parseThreadId } from "./target";
import type { MeshEvent } from "./types";

export const meshLive = reactive({ session: "", title: "", streaming: false, think: "", thinking: false });
export const chatLoad = reactive({ loading: false, thread: "" });

let heldBusy = false;
let heldBusySession = "";

function runningNow(): boolean {
  try {
    return meshAgent.isRunning;
  } catch {
    return false;
  }
}

function putFlags(next: { streaming: boolean; thinking: boolean }): void {
  meshLive.streaming = next.streaming;
  meshLive.thinking = next.thinking;
}

export function applyCatalogBusy(busy: boolean, session = ""): void {
  const same = Boolean(session) && (!heldBusySession || session === heldBusySession);
  if (session) heldBusySession = session;
  const next = flagsAfterBusy(
    { streaming: meshLive.streaming, thinking: meshLive.thinking },
    busy,
    heldBusy && same,
    runningNow(),
    same,
  );
  heldBusy = busy;
  if (busy && session && !meshLive.session) meshLive.session = session;
  putFlags(next);
}

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

function chunkFail(obj: LiveChunk): string {
  const top = obj.error;
  if (typeof top === "string" && top.trim()) return top.replace(/\s+/g, " ").trim();
  if (top && typeof top === "object") {
    const msg = (top as { message?: unknown }).message;
    if (typeof msg === "string" && msg.trim()) return msg.replace(/\s+/g, " ").trim();
  }
  const mesh = meshOf(obj);
  if ((mesh.type || "").toLowerCase() !== "error") return "";
  return (chunkDelta(obj) || mesh.preview || "").replace(/\s+/g, " ").trim() || "Request failed";
}

async function failBody(resp: Response): Promise<string> {
  return unwrapFailText(await resp.text(), resp.status) || "http " + resp.status;
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
  const kind = mesh.type || (mesh.tool ? "tool_call" : "assistant");
  putFlags(
    flagsAfterChunk(
      { streaming: meshLive.streaming, thinking: meshLive.thinking },
      kind,
      chunkTerminal(obj),
      runningNow(),
    ),
  );
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
    throw new Error("not signed in");
  }
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
    super({ agentId: "default", description: "Agent Mesh session" });
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
      // replay the current list when the same session is already on screen; switching sessions uses that session's cache instead of the old snapshot.
      const reuseLive = historyThread === threadId && this.messages.length > 0;
      if (cached.length) {
        historyEvents = cached;
        historyThread = threadId;
      }
      const snapshot = reuseLive
        ? this.messages.slice()
        : cached.length
          ? eventsToMessages(cached)
          : [];
      sub.next({ type: EventType.RUN_STARTED, threadId, runId });
      sub.next({
        type: EventType.MESSAGES_SNAPSHOT,
        messages: snapshot,
      });
      sub.next({ type: EventType.RUN_FINISHED, threadId, runId });
      sub.complete();
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
        meshLive.thinking = false;
        meshLive.streaming = heldBusy;
        if (historyEvents.length && pushHasUser(sentText)) {
          paintMessages(historyEvents, false);
        }
      };
      (async () => {
        sub.next({ type: EventType.RUN_STARTED, threadId, runId });
        const failRun = (message: string, toast = true) => {
          if (toast) showToast(message, "error");
          sub.next({ type: EventType.RUN_ERROR, message });
          sub.complete();
        };
        const payload = lastUserPayload(input.messages || []);
        const text = payload.text || (payload.files.length ? "Please look at the attachments." : "");
        sentText = text;
        if (!text) {
          failRun("missing user text");
          return;
        }
        // a new chat has an empty meshTarget.session; do not reuse a leftover CopilotChat thread.
        const bound = threadId.includes("|") ? parseThreadId(threadId) : { node: "", engine: "", session: "" };
        const session = meshTarget.session;
        const engine = meshTarget.engine || bound.engine;
        const node = meshTarget.node || bound.node;
        const model = chatModelOf(meshTarget.model, node, engine);
        if (!model) {
          failRun("Pick an engine or model first");
          return;
        }
        if (!session && !meshTarget.cwd) {
          failRun("Pick a working directory first");
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
          throw new Error(await failBody(resp));
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
        let toolParentId = "";
        let lastClosedToolId = "";
        let lastClosedToolName = "";
        let thinkAcc = "";
        let sawContent = false;
        let ended = false;
        let runFailed = false;
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
        const resetToolGroup = () => {
          toolParentId = "";
        };
        const ensureToolParent = (): string => {
          if (!toolParentId) toolParentId = crypto.randomUUID();
          return toolParentId;
        };
        const ensureText = () => {
          if (textId) return;
          resetToolGroup();
          textId = crypto.randomUUID();
          sub.next({
            type: EventType.TEXT_MESSAGE_START,
            messageId: textId,
            role: "assistant",
          });
        };
        const feed = (obj: LiveChunk): boolean => {
          const fail = chunkFail(obj);
          if (fail) {
            ended = true;
            runFailed = true;
            showToast(fail, "error");
            sub.next({ type: EventType.RUN_ERROR, message: fail });
            return true;
          }
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
                parentMessageId: ensureToolParent(),
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
                parentMessageId: ensureToolParent(),
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
          historyEvents = historyEvents.concat([{ type: "thinking", text: thinkAcc }]);
          if (threadId) historyCache.set(threadId, historyEvents);
        }
        cacheLiveThread(threadId);
        // AG-UI forbids RUN_FINISHED after RUN_ERROR.
        if (runFailed) {
          sub.complete();
          return;
        }
        sub.next({ type: EventType.RUN_FINISHED, threadId, runId });
        sub.complete();
      })()
        .catch((err: unknown) => {
          toastError(err);
          sub.next({
            type: EventType.RUN_ERROR,
            message: err instanceof Error ? err.message : String(err),
          });
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
  heldBusy = false;
  heldBusySession = "";
  clearAsk();
  try {
    if (meshAgent.isRunning) meshAgent.abortRun();
  } catch {
    /* no in-flight request */
  }
  meshAgent.threadId = "";
  meshAgent.setMessages([]);
}

export function expectHistory(threadId: string): void {
  if (!threadId) return;
  chatLoad.thread = threadId;
  const cached = historyCache.get(threadId);
  if (cached && cached.length) {
    const sameThread = historyThread === threadId;
    historyEvents = cached;
    historyThread = threadId;
    if (historySource === "none") historySource = "http";
    chatLoad.loading = false;
    if (!sameThread || meshAgent.messages.length === 0) {
      noteAskEvents(cached, true);
      paintMessages(cached, meshAgent.messages.length === 0);
    }
    return;
  }
  chatLoad.loading = true;
  clearAsk();
  void refreshHistory(threadId);
}

function paintMessages(evs: MeshEvent[], progressive: boolean): void {
  if (!runningNow()) noteAskEvents(evs, true);
  const prev = meshAgent.messages.slice();
  const all = ensureTurnThink(keepMediaContent(eventsToMessages(evs), prev), meshLive.think);
  const first = progressive && prev.length === 0 ? lastTurnMessages(all, FIRST_PAINT_TURNS) : all;
  if (!runningNow() && !sameMessages(prev, first)) meshAgent.setMessages(first);
  chatLoad.loading = false;
  if (!progressive || first.length >= all.length || prev.length > 0) return;
  requestAnimationFrame(() => {
    requestAnimationFrame(() => {
      if (runningNow()) return;
      if (historyEvents !== evs) return;
      if (sameMessages(meshAgent.messages, all)) return;
      meshAgent.setMessages(all);
    });
  });
}

function keepLiveTranscript(evs: MeshEvent[]): boolean {
  return (!evs || evs.length === 0) && (runningNow() || meshAgent.messages.length > 0);
}

export function applyHistory(evs: MeshEvent[], reset: boolean): void {
  const incoming = evs || [];
  // a new session's history may still be empty; an empty snapshot must not overwrite a live stream.
  if (reset && keepLiveTranscript(incoming)) return;
  const thread = meshAgent.threadId || historyThread;
  if (reset) {
    if (historyThread === thread && sameEvents(historyEvents, incoming)) {
      historySource = "ws";
      chatLoad.loading = false;
      return;
    }
    historyEvents = incoming.slice();
  } else {
    if (!incoming.length) return;
    historyEvents = historyEvents.concat(incoming);
  }
  noteAskEvents(incoming, reset);
  historySource = "ws";
  historyThread = thread;
  if (thread) historyCache.set(thread, historyEvents);
  paintMessages(historyEvents, reset && meshAgent.messages.length === 0);
  putFlags(
    flagsAfterEvents(
      { streaming: meshLive.streaming, thinking: meshLive.thinking },
      incoming.map((ev) => ev.type || ""),
      !reset,
      runningNow(),
    ),
  );
}

export async function refreshHistory(threadId: string, signal?: AbortSignal): Promise<void> {
  const { node, engine, session } = parseThreadId(threadId);
  if (!session) {
    chatLoad.loading = false;
    chatLoad.thread = "";
    return;
  }
  if (historyThread === threadId && historyEvents.length && historySource !== "none") {
    chatLoad.loading = false;
    return;
  }
  if (historyThread !== threadId) historySource = "none";
  chatLoad.thread = threadId;
  chatLoad.loading = historyEvents.length === 0 || historyThread !== threadId;
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
    if (historyThread === threadId && sameEvents(historyEvents, rows)) {
      historySource = "http";
      chatLoad.loading = false;
      return;
    }
    historyEvents = rows;
    historyThread = threadId;
    historySource = "http";
    historyCache.set(threadId, historyEvents);
    paintMessages(historyEvents, meshAgent.messages.length === 0);
  } catch (err) {
    if (ac.signal.aborted || gen !== historyGen) return;
    chatLoad.loading = false;
    toastError(err);
  }
}
