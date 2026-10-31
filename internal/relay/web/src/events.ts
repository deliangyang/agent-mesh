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
    if (type === "title" || type === "session" || type === "system" || type === "ask") continue;
    if (user && !text) continue;
    if (!text && !isToolType(type) && !isThinkingType(type) && !ev.tool) continue;
    if (type === "log" && ev.done && !text) continue;
    const last = out[out.length - 1];
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
      last.text = (last.text || "") + text;
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

export function eventsToMessages(evs: MeshEvent[]): Message[] {
  const rows = lastTurnEvents(mergeEvents(evs));
  const out: Message[] = [];
  let n = 0;
  const nextId = () => "m" + String(++n);
  const lastToolId: Record<string, string> = {};
  let lastAnyToolId = "";
  for (const ev of rows) {
    if (isUser(ev)) {
      out.push({ id: nextId(), role: "user", content: ev.text || "" });
      continue;
    }
    if (ev.type === "thinking") {
      const last = out[out.length - 1] as ReasoningMessage | undefined;
      if (last && last.role === "reasoning") {
        last.content = (typeof last.content === "string" ? last.content : "") + (ev.text || "");
      } else {
        out.push({ id: nextId(), role: "reasoning", content: ev.text || "" });
      }
      continue;
    }
    if (ev.type === "tool_call") {
      let last = out[out.length - 1];
      if (!last || last.role !== "assistant") {
        last = { id: nextId(), role: "assistant", content: "", toolCalls: [] };
        out.push(last);
      }
      const asst = last as AssistantMessage;
      asst.toolCalls = asst.toolCalls || [];
      const tcId = nextId();
      lastToolId[ev.tool || "tool"] = tcId;
      lastAnyToolId = tcId;
      asst.toolCalls.push({
        id: tcId,
        type: "function",
        function: { name: ev.tool || "tool", arguments: ev.text || ev.preview || "{}" },
      });
      continue;
    }
    if (ev.type === "tool_result") {
      const tcId = (ev.tool && lastToolId[ev.tool]) || lastAnyToolId || nextId();
      out.push({
        id: nextId(),
        role: "tool",
        toolCallId: tcId,
        content: ev.text || ev.preview || "",
      });
      continue;
    }
    const last = out[out.length - 1];
    if (last && last.role === "assistant" && !(last as AssistantMessage).toolCalls?.length) {
      last.content = (typeof last.content === "string" ? last.content : "") + (ev.text || "");
    } else {
      out.push({ id: nextId(), role: "assistant", content: ev.text || "" });
    }
  }
  return out;
}
