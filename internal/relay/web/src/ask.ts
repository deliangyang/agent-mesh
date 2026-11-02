import { reactive } from "vue";
import { fetchJSON } from "./http.ts";
import { t } from "./locale.ts";
import { meshTarget } from "./target.ts";
import { toastError } from "./toast.ts";
import type { AskOption, AskPrompt, AskQuestion, MeshEvent } from "./types.ts";

export const meshAsk = reactive({
  prompt: null as AskPrompt | null,
  answering: false,
  error: "",
  dismissed: "",
});

function asObject(v: unknown): Record<string, unknown> {
  if (v && typeof v === "object" && !Array.isArray(v)) return v as Record<string, unknown>;
  if (typeof v === "string") {
    try {
      const parsed = JSON.parse(v) as unknown;
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
        return parsed as Record<string, unknown>;
      }
    } catch {
      return {};
    }
  }
  return {};
}

function str(v: unknown): string {
  return typeof v === "string" ? v.trim() : "";
}

function askToolName(name: string): boolean {
  const n = String(name || "")
    .toLowerCase()
    .replace(/[_\-\s]/g, "");
  return (
    n === "askuserquestion" ||
    n === "askquestion" ||
    n === "askuser" ||
    n === "question" ||
    n.includes("askuser") ||
    n.endsWith("question")
  );
}

function parseOptions(v: unknown): AskOption[] {
  if (!Array.isArray(v)) return [];
  const out: AskOption[] = [];
  for (const item of v) {
    if (typeof item === "string" && item.trim()) {
      out.push({ id: item.trim(), label: item.trim() });
      continue;
    }
    const m = asObject(item);
    const label = str(m.label) || str(m.title) || str(m.text) || str(m.name) || str(m.value);
    const id = str(m.id) || str(m.value) || label;
    if (!label) continue;
    out.push({ id, label, hint: str(m.description) || str(m.hint) || str(m.detail) });
  }
  return out;
}

function parseQuestion(v: unknown): AskQuestion | null {
  const m = asObject(v);
  const prompt = str(m.question) || str(m.prompt) || str(m.text) || str(m.title) || str(m.message);
  const options = parseOptions(m.options);
  if (!prompt && !options.length) return null;
  return {
    id: str(m.id) || str(m.header) || prompt || "q",
    prompt: prompt || str(m.header) || t("ask.pleaseChoose"),
    header: str(m.header),
    multi: Boolean(m.multiSelect || m.multi),
    options,
  };
}

export function parseAskPrompt(raw: unknown, fallbackName = ""): AskPrompt | null {
  const m = asObject(raw);
  if (m.questions || m.options || m.question || m.prompt) {
    const questions: AskQuestion[] = [];
    if (Array.isArray(m.questions)) {
      for (const item of m.questions) {
        const q = parseQuestion(item);
        if (q) questions.push(q);
      }
    } else if (m.questions) {
      const q = parseQuestion(m.questions);
      if (q) questions.push(q);
    } else {
      const q = parseQuestion(m);
      if (q) questions.push(q);
    }
    if (!questions.length) return null;
    return {
      id: str(m.id),
      kind: str(m.kind) || "question",
      title: str(m.title) || str(m.header) || questions[0]?.header || questions[0]?.prompt,
      questions,
    };
  }
  if (fallbackName && askToolName(fallbackName)) {
    return parseAskPrompt({ ...m, kind: "question" });
  }
  return null;
}

function promptKey(p: AskPrompt): string {
  return p.id || p.title || p.questions.map((q) => q.prompt).join("\n");
}

function showPrompt(prompt: AskPrompt | null): void {
  if (!prompt) return;
  const key = promptKey(prompt);
  if (key && key === meshAsk.dismissed) return;
  if (key && meshAsk.dismissed && meshAsk.dismissed !== key) meshAsk.dismissed = "";
  meshAsk.prompt = prompt;
  meshAsk.error = "";
}

function settleAsk(): void {
  if (!meshAsk.prompt) return;
  meshAsk.prompt = null;
  meshAsk.answering = false;
  meshAsk.error = "";
}

function isAskToolCall(type: string, tool: string): boolean {
  return (
    (type === "tool_call" || type === "tool_use" || type === "tool-call" || type === "function_call") &&
    askToolName(tool)
  );
}

function isToolCall(type: string): boolean {
  return type === "tool_call" || type === "tool_use" || type === "tool-call" || type === "function_call";
}

function isToolResult(type: string): boolean {
  return type === "tool_result" || type === "tool-result" || type === "function_call_result";
}

export function noteAskEvent(ev: MeshEvent): void {
  if (!ev) return;
  const type = ev.type || "";
  const tool = ev.tool || "";
  if (type === "ask") {
    showPrompt(parseAskPrompt(ev.text));
    return;
  }
  if (isAskToolCall(type, tool) && ev.text) {
    showPrompt(parseAskPrompt(ev.text, tool));
    return;
  }
  if (!meshAsk.prompt) return;
  if (isToolResult(type) || type === "user" || ev.role === "user") {
    settleAsk();
    return;
  }
  if (isToolCall(type) && tool && tool !== "tool" && !askToolName(tool)) {
    settleAsk();
    return;
  }
  if ((type === "assistant" || type === "delta") && (ev.text || "").trim()) {
    settleAsk();
  }
}

export function noteAskEvents(evs: MeshEvent[], reset: boolean): void {
  if (reset) {
    meshAsk.prompt = null;
    meshAsk.error = "";
  }
  for (const ev of evs || []) noteAskEvent(ev);
}

export function noteAskFromTool(name: string, args: unknown, id?: string): void {
  if (!askToolName(name)) {
    if (name && name !== "tool") settleAsk();
    return;
  }
  const prompt = parseAskPrompt(args, name);
  if (!prompt) return;
  if (id) prompt.id = id;
  showPrompt(prompt);
}

export function dismissAsk(): void {
  if (meshAsk.prompt) meshAsk.dismissed = promptKey(meshAsk.prompt);
  meshAsk.prompt = null;
  meshAsk.answering = false;
  meshAsk.error = "";
}

export function clearAsk(): void {
  meshAsk.prompt = null;
  meshAsk.answering = false;
  meshAsk.error = "";
  meshAsk.dismissed = "";
}

export function formatAskAnswer(prompt: AskPrompt, picks: Record<string, string[]>): string {
  if (prompt.kind === "permission") {
    for (const q of prompt.questions) {
      const sel = picks[q.id || q.prompt] || picks[q.prompt] || [];
      if (!sel.length) continue;
      const v = sel[0].toLowerCase();
      if (v === "allow" || v === "允许") return "允许";
      if (v === "deny" || v === "拒绝") return "拒绝";
      return sel[0];
    }
    return "";
  }
  const answers: Record<string, string> = {};
  const lines: string[] = [];
  for (const q of prompt.questions) {
    const key = q.id || q.prompt;
    const sel = picks[key] || picks[q.prompt] || [];
    if (!sel.length) continue;
    const joined = sel.join(", ");
    answers[q.prompt] = joined;
    lines.push((q.header || q.prompt) + " → " + joined);
  }
  const json = JSON.stringify({ answers });
  return lines.length ? lines.join("\n") + "\n" + json : json;
}

export async function submitAsk(prompt: AskPrompt, picks: Record<string, string[]>): Promise<boolean> {
  const text = formatAskAnswer(prompt, picks).trim();
  if (!text) {
    meshAsk.error = t("ask.pickFirst");
    return false;
  }
  meshAsk.answering = true;
  meshAsk.error = "";
  try {
    const resp = await fetchJSON("/v1/openai/choice", {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        node: meshTarget.node,
        engine: meshTarget.engine,
        session: meshTarget.session,
        cwd: meshTarget.cwd,
        text,
      }),
    });
    if (!resp.ok) throw new Error((await resp.text()) || "http " + resp.status);
    dismissAsk();
    return true;
  } catch (err) {
    meshAsk.error = err instanceof Error ? err.message : String(err);
    toastError(err);
    return false;
  } finally {
    meshAsk.answering = false;
  }
}
