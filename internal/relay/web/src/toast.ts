import { reactive } from "vue";

export type ToastKind = "error" | "info";

export type ToastItem = {
  id: number;
  kind: ToastKind;
  text: string;
};

export const toasts = reactive<ToastItem[]>([]);

let seq = 0;
const timers = new Map<number, number>();

export function showToast(text: string, kind: ToastKind = "info", ms = 4200): void {
  const value = String(text || "").replace(/\s+/g, " ").trim();
  if (!value) return;
  const last = toasts[toasts.length - 1];
  if (last && last.text === value && last.kind === kind) {
    bump(last.id, ms);
    return;
  }
  const id = ++seq;
  toasts.push({ id, kind, text: Array.from(value).slice(0, 240).join("") });
  if (toasts.length > 4) {
    const old = toasts.shift();
    if (old) clearTimer(old.id);
  }
  bump(id, ms);
}

export function dismissToast(id: number): void {
  clearTimer(id);
  const i = toasts.findIndex((t) => t.id === id);
  if (i >= 0) toasts.splice(i, 1);
}

export function explainError(err: unknown): string {
  if (typeof navigator !== "undefined" && navigator.onLine === false) {
    return "network dropped; check the connection and try again";
  }
  if (isAbort(err)) return "";
  const raw = err instanceof Error ? err.message : String(err || "");
  const msg = unwrapFailText(raw);
  if (!msg) return "Request failed";
  if (/failed to fetch|networkerror|load failed|err_network|err_internet|net::/i.test(msg)) {
    return "network error; check the connection and try again";
  }
  if (msg === "not signed in") return msg;
  if (msg.startsWith("<") || msg.length > 240) {
    const code = msg.match(/\b(http\s*)?(\d{3})\b/i);
    if (code) return "Request failed (" + code[2] + ")";
    return "Request failed";
  }
  return msg;
}

/** Prefer a human message from plain text or JSON error bodies. */
export function unwrapFailText(raw: string, status = 0): string {
  const text = String(raw || "").replace(/\s+/g, " ").trim();
  if (!text) return status ? "Request failed (" + status + ")" : "";
  if (text.startsWith("{") || text.startsWith("[")) {
    try {
      const j = JSON.parse(text) as { error?: unknown; message?: unknown };
      if (typeof j.error === "string" && j.error.trim()) return j.error.replace(/\s+/g, " ").trim();
      if (j.error && typeof j.error === "object") {
        const nested = (j.error as { message?: unknown }).message;
        if (typeof nested === "string" && nested.trim()) return nested.replace(/\s+/g, " ").trim();
      }
      if (typeof j.message === "string" && j.message.trim()) return j.message.replace(/\s+/g, " ").trim();
    } catch {
      /* plain text */
    }
  }
  return text;
}

export function toastError(err: unknown): void {
  const text = explainError(err);
  if (text) showToast(text, "error");
}

function isAbort(err: unknown): boolean {
  if (!err || typeof err !== "object") return false;
  const name = (err as { name?: string }).name;
  return name === "AbortError";
}

function bump(id: number, ms: number): void {
  clearTimer(id);
  if (ms <= 0) return;
  timers.set(
    id,
    window.setTimeout(() => {
      dismissToast(id);
    }, ms),
  );
}

function clearTimer(id: number): void {
  const t = timers.get(id);
  if (t) window.clearTimeout(t);
  timers.delete(id);
}
