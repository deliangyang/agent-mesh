import type { Message } from "@ag-ui/core";

export type MeshFile = {
  name: string;
  mime: string;
  data: string;
};

export type AttachRef = {
  path: string;
  mime: string;
  name?: string;
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

/** Client-side media bank so history refreshes do not drop optimistic attachments. */
const mediaBank = new Map<string, unknown>();

export function rememberMedia(text: string, content: unknown): void {
  if (!Array.isArray(content) || !mediaParts(content).length) return;
  const key = normalizeMediaKey(text) || "__attach_only__";
  mediaBank.set(key, content);
}

export function recallMedia(text: string): unknown | null {
  const key = normalizeMediaKey(text) || "__attach_only__";
  return mediaBank.get(key) || null;
}

export function clearMediaBank(): void {
  mediaBank.clear();
}

function normalizeMediaKey(text: string): string {
  return String(text || "")
    .replace(/\s+/g, " ")
    .trim();
}

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
  return next.map((m, i) => {
    if (m.role !== "user") return m;
    if (Array.isArray(m.content) && mediaParts(m.content).length) {
      rememberMedia(flattenText(m.content), m.content);
      return m;
    }
    const text = flattenText(m.content);
    const hit =
      media.find((old) => flattenText(old.content) === text) ||
      (text ? null : media[media.length - 1]);
    if (hit) {
      rememberMedia(text, hit.content);
      return { ...m, content: hit.content };
    }
    const bank = recallMedia(text);
    if (bank) return { ...m, content: bank as Message["content"] };
    // positional fallback: same user-turn index among user messages
    const userIdx = next.slice(0, i + 1).filter((row) => row.role === "user").length - 1;
    const prevUsers = media;
    if (userIdx >= 0 && userIdx < prevUsers.length) {
      const old = prevUsers[userIdx];
      rememberMedia(flattenText(old.content), old.content);
      return { ...m, content: old.content };
    }
    return m;
  });
}

/** Build AG-UI content parts from text + inbox attach refs. */
export function contentWithAttachRefs(
  text: string,
  files: AttachRef[] | undefined,
  nodeId: string,
): string | Array<Record<string, unknown>> {
  if (!files || !files.length) return text || "";
  const parts: Array<Record<string, unknown>> = [];
  if (text) parts.push({ type: "text", text });
  for (const f of files) {
    const mime = f.mime || guessMime(f.path || f.name || "");
    const type = modalityOf(mime);
    const q = new URLSearchParams({ path: f.path });
    if (nodeId) q.set("node", nodeId);
    parts.push({
      type,
      source: {
        type: "url",
        value: "/v1/openai/file?" + q.toString(),
        mimeType: mime,
      },
      metadata: { filename: f.name || baseName(f.path) },
    });
  }
  return parts;
}

export function parseAttachBlock(raw: string): AttachRef[] {
  const open = "<<MESH_ATTACH>>";
  const close = "<</MESH_ATTACH>>";
  const out: AttachRef[] = [];
  let s = String(raw || "");
  while (true) {
    const i = s.indexOf(open);
    if (i < 0) break;
    const j = s.indexOf(close, i);
    if (j < 0) break;
    const block = s.slice(i + open.length, j);
    s = s.slice(j + close.length);
    for (const line of block.split("\n")) {
      const trimmed = line.trim();
      if (!trimmed.startsWith("- ")) continue;
      let rest = trimmed.slice(2).trim();
      let mime = "";
      const p = rest.lastIndexOf(" (");
      if (p > 0 && rest.endsWith(")")) {
        mime = rest.slice(p + 2, -1).trim();
        rest = rest.slice(0, p).trim();
      }
      if (!rest.includes("/.agent-mesh/inbox/") && !rest.includes("\\.agent-mesh\\inbox\\")) continue;
      out.push({ path: rest, mime, name: baseName(rest) });
    }
  }
  return out;
}

function modalityOf(mime: string): MediaPart["type"] {
  if (mime.startsWith("image/")) return "image";
  if (mime.startsWith("audio/")) return "audio";
  if (mime.startsWith("video/")) return "video";
  return "document";
}

function guessMime(name: string): string {
  const lower = name.toLowerCase();
  if (lower.endsWith(".png")) return "image/png";
  if (lower.endsWith(".jpg") || lower.endsWith(".jpeg")) return "image/jpeg";
  if (lower.endsWith(".gif")) return "image/gif";
  if (lower.endsWith(".webp")) return "image/webp";
  if (lower.endsWith(".svg")) return "image/svg+xml";
  if (lower.endsWith(".pdf")) return "application/pdf";
  return "application/octet-stream";
}

function baseName(path: string): string {
  const s = String(path || "").replace(/\\/g, "/");
  const i = s.lastIndexOf("/");
  return i >= 0 ? s.slice(i + 1) : s;
}

function fallbackName(type: string, mime: string): string {
  const ext = mime.split("/")[1] || "bin";
  if (type === "image") return "image." + ext;
  if (type === "audio") return "audio." + ext;
  if (type === "video") return "video." + ext;
  return "file." + ext;
}
