import type { MeshEvent } from "../types";

/** Shape expected by meshAgent feed/pushLiveChunk (OpenAI chunk + agent_mesh). */
export function eventToChunk(ev: {
  type?: string;
  role?: string;
  tool?: string;
  engine?: string;
  session?: string;
  text?: string;
  preview?: string;
  done?: boolean;
}): Record<string, unknown> {
  const content = ev.text || "";
  return {
    choices: [
      {
        index: 0,
        delta: { content },
        finish_reason: ev.done ? "stop" : null,
      },
    ],
    agent_mesh: {
      type: ev.type || "assistant",
      tool: ev.tool || "",
      preview: ev.preview || "",
      session: ev.session || "",
      engine: ev.engine || "",
      role: ev.role || "",
    },
  };
}

export function asMeshEvents(body: unknown): MeshEvent[] {
  return Array.isArray(body) ? (body as MeshEvent[]) : [];
}
