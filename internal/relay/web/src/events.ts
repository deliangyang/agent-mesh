import type { AssistantMessage, Message, ReasoningMessage } from "@ag-ui/core";
import type { MeshEvent } from "./types";

const TURN_LIMIT = 20;

function isToolType(type: string): boolean {
  return (
    type === "tool_call" ||
    type === "tool_result" ||
    type === "tool_use" ||
    type === "tool-call" ||
    type === "tool-result" ||
    type === "tool_call_update" ||
    type === "function_call" ||
    type === "function_call_result"
  );
}

function isThinkingType(type: string): boolean {
  return (
    type === "thinking" ||
    type === "reasoning" ||
    type === "thought" ||
    type === "thinking_delta" ||
    type === "redacted_thinking"
  );
}

function normalizeType(ev: MeshEvent): string {
  const type = ev.type || "assistant";
  if (isToolType(type)) {
    return type.includes("result") ? "tool_result" : "tool_call";
  }
  if (isThinkingType(type)) return "thinking";
  if (type === "delta" || type === "log") return "assistant";
  return type;
}

function isUser(ev: MeshEvent): boolean {
  const type = normalizeType(ev);
  if (isToolType(type) || isThinkingType(type)) return false;
  return type === "user" || ev.role === "user";
}

const USER_QUERY_RE = /<user_query>([\s\S]*?)<\/user_query>/gi;
const ENVELOPE_OPEN_RE = /<([a-z][a-z0-9_]*)\b[^>]*>/gi;
const LEFTOVER_ENVELOPE_TAG_RE =
  /<\/?(?:[a-z][a-z0-9]*_[a-z0-9_]*|timestamp|communication)\b[^>]*>/gi;

function isEnvelopeTag(name: string): boolean {
  const n = String(name || "").toLowerCase();
  if (!n || n === "user_query") return false;
  return n === "timestamp" || n === "communication" || n.includes("_");
}

function stripEnvelopeTags(text: string): string {
  let t = String(text || "");
  let start = 0;
  for (let i = 0; i < 64; i++) {
    ENVELOPE_OPEN_RE.lastIndex = start;
    const m = ENVELOPE_OPEN_RE.exec(t);
    if (!m || m.index === undefined) break;
    const name = String(m[1] || "").toLowerCase();
    const abs0 = m.index;
    const abs1 = abs0 + m[0].length;
    if (!isEnvelopeTag(name)) {
      start = abs1;
      continue;
    }
    const close = "</" + name + ">";
    const rest = t.slice(abs1);
    const idx = rest.toLowerCase().indexOf(close);
    if (idx < 0) {
      t = t.slice(0, abs0) + t.slice(abs1);
      continue;
    }
    t = t.slice(0, abs0) + "\n" + rest.slice(idx + close.length);
    start = abs0;
  }
  return t.replace(LEFTOVER_ENVELOPE_TAG_RE, "").trim();
}

const TASK_PREFIX_RE =
  /^\s*Briefly inform the user about the task result[^\n]*(?:\n+|$)/;
const SESSION_ID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function isInternalDump(text: string): boolean {
  const s = String(text || "").trim();
  if (!s) return false;
  if (s.startsWith("Tool {") || s.startsWith("Tool{")) return true;
  if (!s.startsWith("{")) return false;
  if (/"type"\s*:\s*"thinking"/.test(s) && /"subtype"\s*:\s*"completed"/.test(s)) return true;
  return s.length < 500 && /"type"\s*:\s*"thinking"/.test(s) && /"session_id"/.test(s);
}

function strId(v: unknown): string {
  return typeof v === "string" ? v.trim() : "";
}

function sessionIdFromObj(obj: Record<string, unknown> | null | undefined): string {
  if (!obj) return "";
  const mesh = (obj.agent_mesh || obj.mesh || {}) as Record<string, unknown>;
  const kind = strId(obj.type);
  const direct =
    strId(mesh.session) ||
    strId(obj.session_id) ||
    strId(obj.sessionId) ||
    strId(obj.sessionID) ||
    ((kind === "session" || kind === "session_meta") && strId(obj.id)) ||
    strId(obj.session);
  if (direct) return direct;
  const msg = obj.message as Record<string, unknown> | undefined;
  if (msg && typeof msg === "object") {
    const nested = strId(msg.session_id) || strId(msg.sessionId);
    if (nested) return nested;
  }
  if (kind === "session" || kind === "session_meta") {
    const payload = obj.payload as Record<string, unknown> | undefined;
    if (payload && typeof payload === "object") {
      const nested = strId(payload.id) || strId(payload.session_id) || strId(payload.sessionId);
      if (nested) return nested;
    }
  }
  return "";
}

export function sessionIdFromText(text: string): string {
  const s = String(text || "").trim();
  if (!s.startsWith("{") || !/session/i.test(s)) return "";
  try {
    return sessionIdFromObj(JSON.parse(s) as Record<string, unknown>);
  } catch {
    const m = s.match(/"session_id"\s*:\s*"([^"]+)"/i) || s.match(/"sessionId"\s*:\s*"([^"]+)"/);
    return m ? m[1] : "";
  }
}

export function sessionIdFromChunk(obj: Record<string, unknown> | null | undefined): string {
  const sid = sessionIdFromObj(obj);
  if (sid) return sid;
  const choices = obj?.choices as { delta?: { content?: string } }[] | undefined;
  return sessionIdFromText(choices?.[0]?.delta?.content || "");
}

function looksLikeSessionMeta(text: string): boolean {
  const s = String(text || "").trim();
  if (!s) return true;
  const compact = s.toLowerCase().replace(/\s+/g, "");
  if (compact === "当前session" || compact === "当前会话" || compact === "session" || compact === "currentsession") {
    return true;
  }
  return SESSION_ID_RE.test(s);
}

function stripMeshAttach(text: string): string {
  let s = String(text || "");
  const open = "<<MESH_ATTACH>>";
  const close = "<</MESH_ATTACH>>";
  while (true) {
    const i = s.indexOf(open);
    if (i < 0) break;
    const j = s.indexOf(close, i);
    if (j < 0) {
      s = s.slice(0, i).trim();
      break;
    }
    s = (s.slice(0, i) + s.slice(j + close.length)).trim();
  }
  return s;
}

function dropInjectedUserPrefix(text: string): string {
  const t = stripMeshAttach(String(text || "").replace(TASK_PREFIX_RE, "").trim());
  if (looksLikeSessionMeta(t)) return "";
  return t;
}

export function cleanUserText(text: string): string {
  const raw = stripMeshAttach(String(text || ""));
  const queries: string[] = [];
  raw.replace(USER_QUERY_RE, (_m, inner: string) => {
    const s = String(inner || "").trim();
    if (s) queries.push(s);
    return "";
  });
  if (queries.length) return dropInjectedUserPrefix(queries.join("\n"));
  return dropInjectedUserPrefix(stripEnvelopeTags(raw));
}

export function mergeEvents(evs: MeshEvent[]): MeshEvent[] {
  const out: MeshEvent[] = [];
  for (const ev of evs || []) {
    const type = normalizeType(ev);
    const user = isUser({ ...ev, type });
    const text = user ? cleanUserText(ev.text || "") : ev.text || "";
    if (type === "title" || type === "session" || type === "system") continue;
    if (type === "ask") {
      out.push({
        type: "ask",
        role: ev.role || "",
        tool: ev.tool || "",
        text: ev.text || ev.preview || "",
        preview: ev.preview || "",
        unix: ev.unix || 0,
      });
      continue;
    }
    if (user && !text) continue;
    if (!text && !isToolType(type) && !isThinkingType(type) && !ev.tool) continue;
    if (type === "log" && ev.done && !text) continue;
    if (text && !isToolType(type) && !isThinkingType(type) && isInternalDump(text)) continue;
    const last = out[out.length - 1];
    if (user && last && isUser(last) && (last.text || "") === text) continue;
    if (last && type === "thinking" && last.type === "thinking") {
      last.text = (last.text || "") + text;
      continue;
    }
    if (
      last &&
      !isToolType(type) &&
      type !== "thinking" &&
      (type === "assistant" || type === "delta") &&
      (last.type === "assistant" || last.type === "delta") &&
      last.role !== "user"
    ) {
      last.text = joinAssistantText(last.text || "", text, (ev.type || "") !== "delta");
      continue;
    }
    out.push({
      type,
      role: ev.role || "",
      tool: ev.tool || "",
      text,
      preview: ev.preview || "",
      unix: ev.unix || 0,
    });
  }
  return out;
}

function joinAssistantText(prev: string, next: string, separate: boolean): string {
  if (!prev) return next;
  if (!next) return prev;
  if (separate && !prev.endsWith("\n") && !next.startsWith("\n")) {
    return prev + "\n\n" + next;
  }
  return prev + next;
}

export function lastTurnEvents(evs: MeshEvent[], n = TURN_LIMIT): MeshEvent[] {
  const rows = evs || [];
  if (n <= 0 || !rows.length) return rows;
  const starts: number[] = [];
  rows.forEach((ev, i) => {
    if (isUser(ev)) starts.push(i);
  });
  if (starts.length <= n) return rows;
  return rows.slice(starts[starts.length - n]);
}

export function lastTurnMessages(msgs: Message[], n: number): Message[] {
  const rows = msgs || [];
  if (n <= 0 || rows.length === 0) return rows;
  let users = 0;
  for (let i = rows.length - 1; i >= 0; i--) {
    if (rows[i].role === "user") {
      users += 1;
      if (users >= n) return rows.slice(i);
    }
  }
  return rows;
}

function argsContinue(prev: string, next: string): boolean {
  if (!next) return true;
  const a = (prev || "").trim();
  const b = next.trim();
  if (!a || a === "{}") return true;
  return b.startsWith(a);
}

type TurnBuf = {
  user?: Message;
  tools: AssistantMessage | null;
  results: Message[];
  think: string;
  text: AssistantMessage | null;
};

export function eventsToMessages(evs: MeshEvent[]): Message[] {
  const rows = lastTurnEvents(mergeEvents(evs));
  const out: Message[] = [];
  let n = 0;
  const nextId = () => "m" + String(++n);
  const lastToolId: Record<string, string> = {};
  let lastAnyToolId = "";
  let turn: TurnBuf = { tools: null, results: [], think: "", text: null };

  const flush = (t: TurnBuf) => {
    if (t.user) out.push(t.user);
    if (t.tools && t.tools.toolCalls && t.tools.toolCalls.length) out.push(t.tools);
    for (const row of t.results) out.push(row);
    if (t.think.trim()) {
      out.push({ id: nextId(), role: "reasoning", content: t.think } as ReasoningMessage);
    }
    if (t.text && String(t.text.content || "").trim()) out.push(t.text);
  };

  const ensureTools = (): AssistantMessage => {
    if (!turn.tools) {
      turn.tools = { id: nextId(), role: "assistant", content: "", toolCalls: [] };
    }
    return turn.tools;
  };

  for (const ev of rows) {
    if (ev.type === "ask") continue;
    if (isUser(ev)) {
      flush(turn);
      turn = {
        user: { id: nextId(), role: "user", content: ev.text || "" },
        tools: null,
        results: [],
        think: "",
        text: null,
      };
      continue;
    }
    if (ev.type === "thinking") {
      turn.think += ev.text || "";
      continue;
    }
    if (ev.type === "tool_call") {
      const asst = ensureTools();
      asst.toolCalls = asst.toolCalls || [];
      const name = ev.tool || "tool";
      const args = ev.text || ev.preview || "";
      const lastTc = asst.toolCalls[asst.toolCalls.length - 1];
      if (lastTc && lastTc.function.name === name && argsContinue(lastTc.function.arguments, args)) {
        lastToolId[name] = lastTc.id;
        lastAnyToolId = lastTc.id;
        if (args) lastTc.function.arguments = args;
        continue;
      }
      const tcId = nextId();
      lastToolId[name] = tcId;
      lastAnyToolId = tcId;
      asst.toolCalls.push({
        id: tcId,
        type: "function",
        function: { name, arguments: args || "{}" },
      });
      continue;
    }
    if (ev.type === "tool_result") {
      const tcId = (ev.tool && lastToolId[ev.tool]) || lastAnyToolId || nextId();
      turn.results.push({
        id: nextId(),
        role: "tool",
        toolCallId: tcId,
        content: ev.text || ev.preview || "",
      });
      continue;
    }
    if (turn.text) {
      const prev = typeof turn.text.content === "string" ? turn.text.content : "";
      turn.text.content = joinAssistantText(prev, ev.text || "", (ev.type || "") !== "delta");
    } else {
      turn.text = { id: nextId(), role: "assistant", content: ev.text || "" };
    }
  }
  flush(turn);
  return out;
}

function lastTurnStart(msgs: Message[]): number {
  for (let i = msgs.length - 1; i >= 0; i--) {
    if (msgs[i].role === "user") return i + 1;
  }
  return 0;
}

export function ensureTurnThink(msgs: Message[], think: string): Message[] {
  const text = String(think || "").trim();
  if (!text || !msgs.length) return msgs;
  const start = lastTurnStart(msgs);
  for (let i = msgs.length - 1; i >= start; i--) {
    if (msgs[i].role !== "reasoning") continue;
    const cur = String((msgs[i] as ReasoningMessage).content || "");
    if (!cur || text.startsWith(cur) || cur.startsWith(text.slice(0, Math.min(80, text.length)))) {
      if (cur === text) return msgs;
      const out = msgs.slice();
      out[i] = { ...msgs[i], content: text } as ReasoningMessage;
      return out;
    }
  }
  const out = msgs.slice();
  let insertAt = out.length;
  for (let i = start; i < out.length; i++) {
    const m = out[i];
    if (m.role === "assistant" && !(m as AssistantMessage).toolCalls?.length) {
      insertAt = i;
      break;
    }
    insertAt = i + 1;
  }
  out.splice(insertAt, 0, { id: "think-live", role: "reasoning", content: text } as ReasoningMessage);
  return out;
}
