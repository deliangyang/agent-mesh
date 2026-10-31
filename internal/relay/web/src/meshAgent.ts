import { AbstractAgent, EventType } from "@ag-ui/client";
import type { BaseEvent, Message, RunAgentInput } from "@ag-ui/core";
import { Observable } from "rxjs";
import { reactive } from "vue";
import { clearAsk, noteAskEvent, noteAskEvents, noteAskFromTool } from "./ask";
import { keepMediaContent, lastUserPayload } from "./attach";
import { eventsToMessages, lastTurnMessages } from "./events";
import { meshTarget, parseThreadId } from "./target";
import type { MeshEvent } from "./types";

export const meshLive = reactive({ session: "", title: "", streaming: false });
export const chatLoad = reactive({ loading: false, thread: "" });

const FIRST_PAINT_TURNS = 2;
const historyCache = new Map<string, MeshEvent[]>();
let historyEvents: MeshEvent[] = [];
let historyThread = "";
let historySource: "none" | "http" | "ws" = "none";
let historyGen = 0;
let historyAbort: AbortController | null = null;

export function isLiveChat(): boolean {
  return meshLive.streaming;
}

function redirectIfUnauthorized(resp: Response): void {
  if (resp.status === 401) {
    location.href = "/login";
    throw new Error("未登录");
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
  onEvent: (obj: Record<string, unknown>) => void,
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
        onEvent(JSON.parse(data) as Record<string, unknown>);
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
      // 正在流式输出时只回放当前消息，避免空快照把输入气泡清掉。
      const snapshot = meshLive.streaming
        ? this.messages.slice()
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
      const finish = () => {
        meshLive.streaming = false;
      };
      (async () => {
        sub.next({ type: EventType.RUN_STARTED, threadId, runId });
        const payload = lastUserPayload(input.messages || []);
        const text = payload.text || (payload.files.length ? "请查看附件。" : "");
        if (!text) {
          sub.next({ type: EventType.RUN_ERROR, message: "缺少用户文本" });
          sub.next({ type: EventType.RUN_FINISHED, threadId, runId });
          sub.complete();
          return;
        }
        const parsed = parseThreadId(threadId);
        const session = parsed.session || meshTarget.session;
        const model =
          meshTarget.model ||
          (parsed.node && parsed.engine ? parsed.node + "/" + parsed.engine : parsed.engine);
        if (!model) {
          sub.next({ type: EventType.RUN_ERROR, message: "请先选择引擎或模型" });
          sub.next({ type: EventType.RUN_FINISHED, threadId, runId });
          sub.complete();
          return;
        }
        if (!session && !meshTarget.cwd) {
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
        if (session) body.session = session;
        if (meshTarget.cwd) body.cwd = meshTarget.cwd;
        if (payload.files.length) body.files = payload.files;
        if (!session) {
          meshLive.session = "";
          meshLive.title = text.replace(/\s+/g, " ").trim().slice(0, 48);
        }
        const resp = await fetch("/v1/chat/completions", {
          method: "POST",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
          signal: ac.signal,
        });
        redirectIfUnauthorized(resp);
        if (!resp.ok) {
          throw new Error((await resp.text()) || "http " + resp.status);
        }
        const ctype = resp.headers.get("content-type") || "";
        if (ctype.includes("application/json")) {
          const data = (await resp.json()) as { queued?: boolean };
          if (data && data.queued) {
            sub.next({ type: EventType.RUN_FINISHED, threadId, runId });
            sub.complete();
            return;
          }
        }
        let textId = "";
        let toolId = "";
        let toolName = "";
        let toolAcc = "";
        let lastClosedToolId = "";
        let lastClosedToolName = "";
        let thinkId = "";
        let thinkAcc = "";
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
        const closeThink = () => {
          if (!thinkId) return;
          sub.next({ type: EventType.REASONING_MESSAGE_END, messageId: thinkId });
          sub.next({ type: EventType.REASONING_END, messageId: thinkId });
          thinkId = "";
          thinkAcc = "";
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
        await readSSE(
          resp,
          (obj) => {
            const mesh = (obj.agent_mesh || obj.mesh || {}) as {
              type?: string;
              tool?: string;
              preview?: string;
              session?: string;
            };
            const choices = obj.choices as { delta?: { content?: string } }[] | undefined;
            const delta = choices?.[0]?.delta?.content || "";
            const kind = mesh.type || (mesh.tool ? "tool_call" : "assistant");
            if (mesh.session) meshLive.session = mesh.session;
            if (kind === "session" || kind === "system") {
              meshLive.session = mesh.session || meshLive.session;
              return;
            }
            if (kind === "title") {
              const title = (delta || mesh.preview || "").replace(/\s+/g, " ").trim();
              if (title) meshLive.title = title.slice(0, 80);
              return;
            }
            if (kind === "ask") {
              noteAskEvent({
                type: "ask",
                tool: mesh.tool || "",
                text: (delta || "").trim(),
                preview: mesh.preview || "",
              });
              return;
            }
            if (
              kind === "thinking" ||
              kind === "reasoning" ||
              kind === "thought" ||
              kind === "thinking_delta"
            ) {
              closeTool();
              closeText();
              if (!thinkId) {
                thinkId = crypto.randomUUID();
                sub.next({ type: EventType.REASONING_START, messageId: thinkId });
                sub.next({
                  type: EventType.REASONING_MESSAGE_START,
                  messageId: thinkId,
                  role: "reasoning",
                });
              }
              const piece = nextPiece(thinkAcc, delta);
              thinkAcc += piece;
              if (piece) {
                sub.next({
                  type: EventType.REASONING_MESSAGE_CONTENT,
                  messageId: thinkId,
                  delta: piece,
                });
              }
              return;
            }
            if (kind === "tool_result" || kind === "tool-result") {
              closeThink();
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
              return;
            }
            if (kind === "tool_call" || kind === "tool_use" || kind === "tool_call_update") {
              closeThink();
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
              return;
            }
            if (!delta) return;
            closeThink();
            closeTool();
            ensureText();
            sub.next({
              type: EventType.TEXT_MESSAGE_CONTENT,
              messageId: textId,
              delta,
            });
          },
          ac.signal,
        );
        closeThink();
        closeTool();
        closeText();
        sub.next({ type: EventType.RUN_FINISHED, threadId, runId });
        sub.complete();
      })()
        .catch((err: unknown) => {
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
  const prev = meshAgent.messages.slice();
  const all = keepMediaContent(eventsToMessages(evs), prev);
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
  noteAskEvents(incoming, reset);
  // 新建会话的历史可能尚为空；空快照不能覆盖正在流式输出的对话。
  if (reset && keepLiveTranscript(incoming)) return;
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
    const resp = await fetch("/v1/openai/history?" + q.toString(), {
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
  } catch {
    if (ac.signal.aborted || gen !== historyGen) return;
    chatLoad.loading = false;
  }
}
