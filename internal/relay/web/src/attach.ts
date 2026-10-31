import type { Message } from "@ag-ui/core";

export type MeshFile = {
  name: string;
  mime: string;
  data: string;
};

export type MediaPart = {
  type: "image" | "audio" | "video" | "document";
  filename?: string;
  source: {
    type: "data" | "url";
    value: string;
    mimeType?: string;
  };
};

const MEDIA = new Set(["image", "audio", "video", "document"]);

export function flattenText(content: unknown): string {
  if (typeof content === "string") return content.trim();
  if (!Array.isArray(content)) return "";
  const parts: string[] = [];
  for (const item of content) {
    if (!item || typeof item !== "object") continue;
    const row = item as { type?: string; text?: string };
    if (row.type === "text" && typeof row.text === "string" && row.text.trim()) {
      parts.push(row.text);
    }
  }
  return parts.join("\n").trim();
}

export function mediaParts(content: unknown): MediaPart[] {
  if (!Array.isArray(content)) return [];
  const out: MediaPart[] = [];
  for (const item of content) {
    if (!item || typeof item !== "object") continue;
    const row = item as {
      type?: string;
      source?: { type?: string; value?: string; mimeType?: string };
      metadata?: { filename?: string; name?: string };
    };
    const type = row.type || "";
    if (!MEDIA.has(type) || !row.source || !row.source.value) continue;
    if (row.source.type !== "data" && row.source.type !== "url") continue;
    const filename = row.metadata?.filename || row.metadata?.name || "";
    out.push({
      type: type as MediaPart["type"],
      filename: filename || undefined,
      source: {
        type: row.source.type,
        value: row.source.value,
        mimeType: row.source.mimeType,
      },
    });
  }
  return out;
}

export function lastUserPayload(messages: Message[]): { text: string; files: MeshFile[] } {
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i];
    if (m.role !== "user") continue;
    return payloadOf(m.content);
  }
  return { text: "", files: [] };
}

export function payloadOf(content: unknown): { text: string; files: MeshFile[] } {
  const text = flattenText(content);
  const files: MeshFile[] = [];
  for (const part of mediaParts(content)) {
    if (part.source.type !== "data") continue;
    files.push({
      name: part.filename || fallbackName(part.type, part.source.mimeType || ""),
      mime: part.source.mimeType || "",
      data: part.source.value,
    });
  }
  return { text, files };
}

export function keepMediaContent(next: Message[], prev: Message[]): Message[] {
  const media = prev.filter((m) => m.role === "user" && Array.isArray(m.content));
  if (!media.length) return next;
  return next.map((m) => {
    if (m.role !== "user" || Array.isArray(m.content)) return m;
    const text = flattenText(m.content);
    const hit = media.find((old) => flattenText(old.content) === text);
    return hit ? { ...m, content: hit.content } : m;
  });
}

function fallbackName(type: string, mime: string): string {
  const ext = mime.split("/")[1] || "bin";
  if (type === "image") return "image." + ext;
  if (type === "audio") return "audio." + ext;
  if (type === "video") return "video." + ext;
  return "file." + ext;
}
