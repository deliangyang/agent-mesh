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

export function fetchJSON(input: RequestInfo | URL, init: RequestInit = {}, ms = 15000): Promise<Response> {
  if (ms <= 0) return fetch(input, init);
  return fetch(input, { ...init, signal: timeoutSignal(ms, init.signal || undefined) });
}
