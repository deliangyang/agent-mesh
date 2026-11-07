export type StreamFlags = {
  streaming: boolean;
  thinking: boolean;
};

export type LiveHint = "off" | "think" | "live";

export function liveHintMode(flags: StreamFlags): LiveHint {
  if (!flags.streaming) return "off";
  return flags.thinking ? "think" : "live";
}

export function thinkingKind(kind: string): boolean {
  return (
    kind === "thinking" ||
    kind === "reasoning" ||
    kind === "thought" ||
    kind === "thinking_delta" ||
    kind === "redacted_thinking"
  );
}

function contentKind(kind: string): boolean {
  return (
    kind === "ask" ||
    kind === "assistant" ||
    kind === "delta" ||
    kind === "tool_call" ||
    kind === "tool_use" ||
    kind === "tool_call_update" ||
    kind === "tool_result" ||
    kind === "tool-result"
  );
}

export function flagsAfterChunk(
  prev: StreamFlags,
  kind: string,
  terminal: boolean,
  running: boolean,
): StreamFlags {
  if (thinkingKind(kind)) return { streaming: true, thinking: true };
  if (terminal && !running) return { streaming: false, thinking: false };
  if (contentKind(kind)) return { streaming: true, thinking: false };
  if (kind === "title" || kind === "session" || kind === "system" || kind === "user") return prev;
  return { streaming: true, thinking: prev.thinking };
}

export function flagsAfterBusy(
  prev: StreamFlags,
  busy: boolean,
  wasBusy: boolean,
  running: boolean,
  sameSession: boolean,
): StreamFlags {
  if (running) return prev;
  if (!sameSession) {
    return busy ? { streaming: true, thinking: false } : { streaming: false, thinking: false };
  }
  if (busy) return { streaming: true, thinking: prev.thinking };
  if (wasBusy) return { streaming: false, thinking: false };
  return prev;
}

export function flagsAfterEvents(prev: StreamFlags, types: string[], incremental: boolean, running: boolean): StreamFlags {
  if (running || !types.length) return prev;
  const last = types[types.length - 1] || "";
  if (thinkingKind(last)) return { streaming: true, thinking: true };
  if (!incremental) return prev;
  if (contentKind(last)) return { streaming: true, thinking: false };
  return { streaming: true, thinking: prev.thinking };
}
