/** Fingerprint one OpenAI/mesh chunk so SSE and WebSocket can share a feed. */
export function chunkFingerprint(obj: Record<string, unknown>): string {
  const mesh = (obj.agent_mesh || obj.mesh || {}) as {
    type?: string;
    tool?: string;
    session?: string;
    preview?: string;
    role?: string;
  };
  const choices = obj.choices as
    | { delta?: { content?: string }; finish_reason?: string | null }[]
    | undefined;
  const delta = choices?.[0]?.delta?.content || "";
  const reason = choices?.[0]?.finish_reason || "";
  return [
    String(obj.id || ""),
    mesh.session || "",
    mesh.type || "",
    mesh.role || "",
    mesh.tool || "",
    reason || "",
    delta,
    mesh.preview || "",
  ].join("\x1f");
}

/** Returns true when this fingerprint was already applied. */
export function rememberChunk(seen: Set<string>, fp: string, cap = 4000): boolean {
  if (!fp) return false;
  if (seen.has(fp)) return true;
  seen.add(fp);
  if (seen.size > cap) {
    const keep = Array.from(seen).slice(-Math.floor(cap / 2));
    seen.clear();
    for (const key of keep) seen.add(key);
  }
  return false;
}

/** Prefer incremental pieces when the engine repeats cumulative text. */
export function nextPiece(prev: string, incoming: string): string {
  if (!incoming) return "";
  if (prev && incoming.startsWith(prev)) return incoming.slice(prev.length);
  if (prev && prev.endsWith(incoming) && incoming.length < 8) return "";
  return incoming;
}
