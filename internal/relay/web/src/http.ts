import { acceptLanguageHeader } from "./locale.ts";

export function timeoutSignal(ms: number, parent?: AbortSignal): AbortSignal {
  const ac = new AbortController();
  const timer = window.setTimeout(() => ac.abort(), ms);
  const stop = () => window.clearTimeout(timer);
  ac.signal.addEventListener("abort", stop, { once: true });
  if (parent) {
    if (parent.aborted) ac.abort();
    else parent.addEventListener("abort", () => ac.abort(), { once: true });
  }
  return ac.signal;
}

function withAcceptLanguage(init: RequestInit = {}): RequestInit {
  const headers = new Headers(init.headers || undefined);
  if (!headers.has("Accept-Language")) {
    headers.set("Accept-Language", acceptLanguageHeader());
  }
  return { ...init, headers };
}

export function fetchJSON(input: RequestInfo | URL, init: RequestInit = {}, ms = 15000): Promise<Response> {
  const next = withAcceptLanguage(init);
  if (ms <= 0) return fetch(input, next);
  return fetch(input, { ...next, signal: timeoutSignal(ms, next.signal || undefined) });
}
