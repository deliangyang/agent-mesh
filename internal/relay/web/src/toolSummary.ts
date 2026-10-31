const KIND_MAP: Record<string, string> = {
  read: "read",
  readfile: "read",
  read_file: "read",
  write: "write",
  writefile: "write",
  write_file: "write",
  createfile: "write",
  create_file: "write",
  edit: "edit",
  strreplace: "edit",
  str_replace: "edit",
  searchreplace: "edit",
  search_replace: "edit",
  applypatch: "edit",
  apply_patch: "edit",
  bash: "command",
  shell: "command",
  command: "command",
  runcommand: "command",
  run_command: "command",
  runterminalcmd: "command",
  run_terminal_cmd: "command",
  powershell: "command",
  grep: "grep",
  rg: "grep",
  search: "grep",
  codebasesearch: "grep",
  codebase_search: "grep",
  glob: "glob",
  globfilesearch: "glob",
  glob_file_search: "glob",
  find: "glob",
  ls: "list",
  list: "list",
  listdir: "list",
  list_dir: "list",
  listdirectory: "list",
  list_directory: "list",
  delete: "delete",
  deletefile: "delete",
  delete_file: "delete",
  webfetch: "fetch",
  web_fetch: "fetch",
  fetch: "fetch",
  websearch: "search",
  web_search: "search",
};

const PATH_KEYS = [
  "path",
  "file_path",
  "filePath",
  "target_file",
  "targetFile",
  "filename",
  "file",
  "uri",
  "target",
];

const GREP_PATH_KEYS = ["path", "glob", "target_directory", "targetDirectory", "directory", "dir", "cwd"];
const PATTERN_KEYS = ["pattern", "query", "regex", "search"];
const COMMAND_KEYS = ["command", "cmd", "script"];
const URL_KEYS = ["url", "uri"];

export type ToolKind =
  | "read"
  | "write"
  | "edit"
  | "grep"
  | "glob"
  | "list"
  | "command"
  | "delete"
  | "fetch"
  | "search"
  | "tool";

export type ToolRowSummary = {
  kind: ToolKind;
  line: string;
};

function keyOf(name: string): string {
  return String(name || "tool")
    .trim()
    .toLowerCase()
    .replace(/[\s-]+/g, "_");
}

export function kindOf(name: string): ToolKind {
  const key = keyOf(name);
  return (KIND_MAP[key] || KIND_MAP[key.replace(/_/g, "")] || "tool") as ToolKind;
}

function asObject(args: unknown): Record<string, unknown> {
  if (args && typeof args === "object" && !Array.isArray(args)) {
    return args as Record<string, unknown>;
  }
  if (typeof args === "string") {
    const s = args.trim();
    if (!s) return {};
    try {
      const parsed = JSON.parse(s) as unknown;
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
        return parsed as Record<string, unknown>;
      }
    } catch {
      return { command: s };
    }
  }
  return {};
}

function firstString(obj: Record<string, unknown>, keys: string[]): string {
  for (const k of keys) {
    const v = obj[k];
    if (typeof v === "string" && v.trim()) return v.trim();
  }
  return "";
}

function asNumber(v: unknown): number | null {
  if (typeof v === "number" && Number.isFinite(v)) return v;
  if (typeof v === "string" && v.trim() && Number.isFinite(Number(v))) return Number(v);
  return null;
}

function oneLine(s: string, n: number): string {
  const t = s.replace(/\s+/g, " ").trim();
  if (t.length <= n) return t;
  return t.slice(0, n - 1) + "…";
}

function shortPath(p: string, max = 56): string {
  const norm = p.replace(/\\/g, "/").replace(/\/+/g, "/");
  if (norm.length <= max) return norm;
  const parts = norm.split("/").filter(Boolean);
  if (!parts.length) return oneLine(norm, max);
  let out = parts.pop() as string;
  while (parts.length) {
    const next = parts[parts.length - 1] + "/" + out;
    if (next.length + 4 > max) break;
    parts.pop();
    out = next;
  }
  return ".../" + out;
}

function lineRange(obj: Record<string, unknown>): string {
  const start =
    asNumber(obj.offset) ??
    asNumber(obj.start_line) ??
    asNumber(obj.startLine) ??
    asNumber(obj.line_start) ??
    asNumber(obj.lineStart);
  const limit = asNumber(obj.limit) ?? asNumber(obj.line_limit) ?? asNumber(obj.lineLimit);
  const end =
    asNumber(obj.end_line) ?? asNumber(obj.endLine) ?? asNumber(obj.line_end) ?? asNumber(obj.lineEnd);
  if (start != null && end != null && end >= start) return " lines " + start + "-" + end;
  if (start != null && limit != null && limit > 0) return " lines " + start + "-" + (start + limit - 1);
  if (start != null) return " lines " + start;
  return "";
}

function quoted(s: string, max = 48): string {
  return '"' + oneLine(s.replace(/^["']|["']$/g, ""), max) + '"';
}

function grepFileCount(result: unknown): number {
  if (typeof result !== "string" || !result.trim()) return 0;
  const files = new Set<string>();
  for (const line of result.split(/\r?\n/)) {
    const m = line.match(/^([^:\n]+):\d+/);
    if (m && m[1] && !m[1].startsWith(" ")) files.add(m[1]);
  }
  return files.size;
}

export function formatToolLine(name: string, args: unknown): string {
  const kind = kindOf(name);
  const obj = asObject(args);
  const path = firstString(obj, PATH_KEYS);
  const pattern = firstString(obj, PATTERN_KEYS);
  const command = firstString(obj, COMMAND_KEYS);
  const url = firstString(obj, URL_KEYS);

  if (kind === "read") {
    return "Read " + (path ? shortPath(path) + lineRange(obj) : "file");
  }
  if (kind === "write") {
    return "Wrote " + (path ? shortPath(path) : "file");
  }
  if (kind === "edit") {
    return "Edited " + (path ? shortPath(path) : "file");
  }
  if (kind === "grep") {
    const where = firstString(obj, GREP_PATH_KEYS) || path || ".";
    return "Grepped " + (pattern ? quoted(pattern) : "pattern") + " in " + shortPath(where);
  }
  if (kind === "glob") {
    return "Globbed " + quoted(pattern || path || "*");
  }
  if (kind === "list") {
    return "Listed " + shortPath(path || ".");
  }
  if (kind === "command") {
    return "Ran " + oneLine(command || path || "command", 72);
  }
  if (kind === "delete") {
    return "Deleted " + (path ? shortPath(path) : "file");
  }
  if (kind === "fetch") {
    return "Fetched " + shortPath(url || path || "url");
  }
  if (kind === "search") {
    return "Searched " + quoted(pattern || path || "query");
  }
  const fallback = path || command || pattern || url;
  const label = String(name || "Tool").replace(/[_-]+/g, " ");
  const titled = label.charAt(0).toUpperCase() + label.slice(1);
  return fallback ? titled + " " + oneLine(fallback, 64) : titled;
}

export function summarizeTool(
  name: string,
  args: unknown,
  _result?: unknown,
  _status?: string,
): ToolRowSummary {
  return {
    kind: kindOf(name),
    line: formatToolLine(name, args),
  };
}

export function summarizeToolGroup(
  items: { name: string; args?: unknown; result?: unknown }[],
): string {
  const counts: Record<string, number> = {};
  let grepFiles = 0;
  for (const item of items) {
    const kind = kindOf(item.name);
    counts[kind] = (counts[kind] || 0) + 1;
    if (kind === "grep") grepFiles += grepFileCount(item.result);
  }
  const reads = counts.read || 0;
  const greps = counts.grep || 0;
  const writes = counts.write || 0;
  const edits = counts.edit || 0;
  const commands = counts.command || 0;
  const globs = counts.glob || 0;
  const lists = counts.list || 0;
  const fetches = counts.fetch || 0;
  const searches = counts.search || 0;
  const mixed = [reads, greps, writes, edits, commands, globs, lists, fetches, searches].filter((n) => n > 0).length > 1;

  const parts: string[] = [];
  if (reads) parts.push(mixed || greps ? "Read" : reads === 1 ? "Read" : "Read " + reads + " files");
  if (writes) parts.push(writes === 1 ? "wrote 1 file" : "wrote " + writes + " files");
  if (edits) parts.push(edits === 1 ? "edited 1 file" : "edited " + edits + " files");
  if (greps) {
    const files = grepFiles || reads || greps;
    parts.push("grepped " + files + (files === 1 ? " file" : " files"));
    parts.push(greps + " grep");
  }
  if (globs) parts.push(globs === 1 ? "1 glob" : globs + " globs");
  if (lists) parts.push(lists === 1 ? "listed 1 path" : "listed " + lists + " paths");
  if (commands) parts.push(commands === 1 ? "ran 1 command" : "ran " + commands + " commands");
  if (fetches) parts.push(fetches === 1 ? "1 fetch" : fetches + " fetches");
  if (searches) parts.push(searches === 1 ? "1 search" : searches + " searches");
  if (!parts.length) return items.length === 1 ? "1 tool" : items.length + " tools";
  const text = parts.join(", ");
  return text.charAt(0).toUpperCase() + text.slice(1);
}

export function thinkPreview(content: unknown): string {
  const text = typeof content === "string" ? content : "";
  const compact = text.replace(/\s+/g, " ").trim();
  if (!compact) return "";
  if (compact.length <= 180) return compact;
  return "…" + compact.slice(-179);
}
