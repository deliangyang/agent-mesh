import { reactive } from "vue";
import { meshTarget } from "./target";
import type { AskOption, AskPrompt, AskQuestion, MeshEvent } from "./types";

export const meshAsk = reactive({
  prompt: null as AskPrompt | null,
  answering: false,
  error: "",
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
    prompt: prompt || str(m.header) || "请选择",
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

export function noteAskEvent(ev: MeshEvent): void {
  if (!ev) return;
  if (ev.type === "ask") {
    const prompt = parseAskPrompt(ev.text);
    if (prompt) meshAsk.prompt = prompt;
    return;
  }
  if (ev.type === "tool_call" && askToolName(ev.tool || "") && ev.text) {
    const prompt = parseAskPrompt(ev.text, ev.tool);
    if (prompt) meshAsk.prompt = prompt;
    return;
  }
  if (ev.type === "tool_result" && askToolName(ev.tool || "")) {
    meshAsk.prompt = null;
    meshAsk.error = "";
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
  if (!askToolName(name)) return;
  const prompt = parseAskPrompt(args, name);
  if (!prompt) return;
  if (id) prompt.id = id;
  meshAsk.prompt = prompt;
}

export function clearAsk(): void {
  meshAsk.prompt = null;
  meshAsk.answering = false;
  meshAsk.error = "";
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
    const joined = sel.join("、");
    answers[q.prompt] = joined;
    lines.push((q.header || q.prompt) + " → " + joined);
  }
  const json = JSON.stringify({ answers });
  return lines.length ? lines.join("\n") + "\n" + json : json;
}

export async function submitAsk(prompt: AskPrompt, picks: Record<string, string[]>): Promise<boolean> {
  const text = formatAskAnswer(prompt, picks).trim();
  if (!text) {
    meshAsk.error = "先选一项";
    return false;
  }
  meshAsk.answering = true;
  meshAsk.error = "";
  try {
    const resp = await fetch("/v1/openai/choice", {
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
    meshAsk.prompt = null;
    return true;
  } catch (err) {
    meshAsk.error = err instanceof Error ? err.message : String(err);
    return false;
  } finally {
    meshAsk.answering = false;
  }
}
