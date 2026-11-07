import type { MeshEvent } from "./types";

type LiveChunk = Record<string, unknown>;

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

/** Turn an OpenAI/mesh live chunk into a transcript event for passive watch painting. */
export function liveChunkToEvent(obj: LiveChunk): MeshEvent | null {
  const mesh = meshOf(obj);
  const kind = String(mesh.type || (mesh.tool ? "tool_call" : "")).toLowerCase();
  if (!kind || kind === "ping" || kind === "started" || kind === "session" || kind === "system" || kind === "title") {
    return null;
  }
  if (kind === "error") return null;
  const text = chunkDelta(obj);
  const preview = mesh.preview || "";
  const body = text || preview;
  const terminal = chunkTerminal(obj);
  if (!body && !terminal && kind !== "tool_call" && kind !== "tool_use" && kind !== "tool_call_update") {
    return null;
  }
  if (!body && terminal && (kind === "done" || kind === "log" || kind === "result")) {
    return null;
  }
  return {
    type: kind,
    role: mesh.role || undefined,
    tool: mesh.tool || undefined,
    text: body,
    preview: preview || undefined,
    done: terminal || undefined,
  };
}

/** Prefer the richer of two transcripts so a lagging history fetch cannot wipe a live reply. */
export function historyBehindLive(rows: MeshEvent[], liveCount: number, liveChars: number): boolean {
  if (liveCount <= 0) return false;
  if (!rows.length) return true;
  // Rough richness: event count and total text length.
  let chars = 0;
  for (const ev of rows) chars += String(ev.text || "").length + String(ev.preview || "").length;
  if (rows.length < liveCount) return true;
  if (chars + 64 < liveChars) return true;
  return false;
}
