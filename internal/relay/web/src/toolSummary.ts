import { isTodoTool } from "./todos.ts";

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
  shelltoolcall: "command",
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
  lstoolcall: "list",
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
  getdynamictools: "explore",
  calldynamictool: "explore",
  listdynamictools: "explore",
  todowrite: "todo",
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
const PATTERN_KEYS = ["glob_pattern", "globPattern", "pattern", "query", "regex", "search", "glob"];
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
  | "explore"
  | "todo"
  | "tool";

const VERB: Record<ToolKind, string> = {
  read: "Read",
  write: "wrote",
  edit: "edited",
  grep: "grepped",
  glob: "globbed",
  list: "listed",
  command: "ran",
  delete: "deleted",
  fetch: "fetched",
  search: "searched",
  explore: "Explored",
  todo: "updated todos",
  tool: "used tools",
};

const RUNNING: Record<string, string> = {
  Read: "Reading",
  Wrote: "Writing",
  Edited: "Editing",
  Grepped: "Grepping",
  Globbed: "Globbing",
  Listed: "Listing",
  Ran: "Running",
  Deleted: "Deleting",
  Fetched: "Fetching",
  Searched: "Searching",
  Explored: "Exploring",
};

export const TOOL_TAIL = 3;

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

export function kindOfCall(name: string, args?: unknown): ToolKind {
  if (isTodoTool(name, args)) return "todo";
  return kindOf(name);
}

function parseJsonObject(s: string): Record<string, unknown> | null {
  const tryParse = (raw: string): Record<string, unknown> | null => {
    try {
      const parsed = JSON.parse(raw) as unknown;
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
        return parsed as Record<string, unknown>;
      }
    } catch {
      /* 半截 JSON 再往下抽字段 */
    }
    return null;
  };
  const direct = tryParse(s);
  if (direct) return direct;
  const start = s.indexOf("{");
  const end = s.lastIndexOf("}");
  if (start >= 0 && end > start) return tryParse(s.slice(start, end + 1));
  return null;
}

function fieldsFromText(s: string): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  const keys = [...PATH_KEYS, ...GREP_PATH_KEYS, ...PATTERN_KEYS, ...COMMAND_KEYS, ...URL_KEYS];
  for (const k of keys) {
    const re = new RegExp('"' + k.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + '"\\s*:\\s*"((?:\\\\.|[^"\\\\])*)"');
    const m = s.match(re);
    if (m && !out[k]) out[k] = m[1].replace(/\\"/g, '"');
  }
  return out;
}

function stripJsonJunk(s: string): string {
  return s
    .replace(/^[^/A-Za-z]*?(?=\/|[A-Za-z]:[\\/]|\.{1,2}[\\/])/, "")
    .replace(/["'}]+$/, "")
    .trim();
}

function asObject(args: unknown): Record<string, unknown> {
  if (args && typeof args === "object" && !Array.isArray(args)) {
    return args as Record<string, unknown>;
  }
  if (typeof args === "string") {
    const s = args.trim();
    if (!s) return {};
    const fromJson = parseJsonObject(s);
    if (fromJson) return fromJson;
    const fromFields = fieldsFromText(s);
    if (Object.keys(fromFields).length) return fromFields;
    if (/^(\/|\.\/|\.\.\/|[A-Za-z]:[\\/])/.test(s) || s.includes("/") || s.includes("\\")) {
      return { path: stripJsonJunk(s) || s };
    }
    return { command: s };
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

function clipChars(s: string, n: number): string {
  const chars = Array.from(s);
  if (n <= 0) return "";
  if (chars.length <= n) return s;
  return chars.slice(0, n).join("");
}

function oneLine(s: string, n: number): string {
  const t = s.replace(/\s+/g, " ").trim();
  const chars = Array.from(t);
  if (chars.length <= n) return t;
  if (n < 2) return clipChars(t, n);
  return clipChars(t, n - 1) + "…";
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

function grepMatchCount(result: unknown): number {
  if (typeof result !== "string" || !result.trim()) return 0;
  const named = result.match(/Found\s+(\d+)\s+matches?/i);
  if (named) return Number(named[1]);
  let n = 0;
  for (const line of result.split(/\r?\n/)) {
    if (/^[^:\n]+:\d+/.test(line)) n += 1;
  }
  return n;
}

function exploreLine(obj: Record<string, unknown>): string {
  const ns = firstString(obj, ["namespace"]);
  const tool = firstString(obj, ["toolName", "tool_name", "name"]);
  const bits = [ns, tool].filter(Boolean);
  if (bits.length) return "Explored available MCP tools " + bits.join(" · ");
  const pat = firstString(obj, ["pattern", "query"]);
  if (pat) return "Explored available MCP tools " + oneLine(pat, 48);
  return "Explored available MCP tools";
}

function withRunning(line: string, running: boolean): string {
  if (!running) return line;
  const sp = line.indexOf(" ");
  const head = sp < 0 ? line : line.slice(0, sp);
  const rest = sp < 0 ? "" : line.slice(sp);
  return (RUNNING[head] || head) + rest;
}

export function formatToolLine(name: string, args: unknown, status?: string): string {
  const kind = kindOfCall(name, args);
  const obj = asObject(args);
  const path = firstString(obj, PATH_KEYS);
  const pattern = firstString(obj, PATTERN_KEYS);
  const command = firstString(obj, COMMAND_KEYS);
  const url = firstString(obj, URL_KEYS);
  const running = status === "inProgress" || status === "executing";

  if (kind === "read") {
    return withRunning("Read " + (path ? shortPath(path) + lineRange(obj) : "file"), running);
  }
  if (kind === "write") {
    return withRunning("Wrote " + (path ? shortPath(path) : "file"), running);
  }
  if (kind === "edit") {
    return withRunning("Edited " + (path ? shortPath(path) : "file"), running);
  }
  if (kind === "grep") {
    const where = firstString(obj, GREP_PATH_KEYS) || path || ".";
    return withRunning("Grepped " + (pattern ? quoted(pattern) : "pattern") + " in " + shortPath(where), running);
  }
  if (kind === "glob") {
    const where = firstString(obj, GREP_PATH_KEYS);
    const pat = pattern || path || "*";
    const line = where && where !== pat ? "Globbed " + quoted(pat) + " in " + shortPath(where) : "Globbed " + quoted(pat);
    return withRunning(line, running);
  }
  if (kind === "list") {
    return withRunning("Listed " + shortPath(path || "."), running);
  }
  if (kind === "command") {
    return withRunning("Ran " + oneLine(command || path || "command", 72), running);
  }
  if (kind === "delete") {
    return withRunning("Deleted " + (path ? shortPath(path) : "file"), running);
  }
  if (kind === "fetch") {
    return withRunning("Fetched " + shortPath(url || path || "url"), running);
  }
  if (kind === "search") {
    return withRunning("Searched " + quoted(pattern || path || "query"), running);
  }
  if (kind === "explore") {
    return withRunning(exploreLine(obj), running);
  }
  if (kind === "todo") {
    return withRunning("Updated todos", running);
  }
  const fallback = path || command || pattern || url;
  const label = String(name || "Tool").replace(/[_-]+/g, " ");
  const titled = label.charAt(0).toUpperCase() + label.slice(1);
  return fallback ? titled + " " + oneLine(fallback, 64) : titled;
}

export function formatToolResult(name: string, result: unknown): string {
  if (kindOf(name) !== "grep") return "";
  const n = grepMatchCount(result);
  if (!n) return "";
  return "Found " + n + (n === 1 ? " match" : " matches");
}

export function summarizeTool(
  name: string,
  args: unknown,
  _result?: unknown,
  _status?: string,
): ToolRowSummary {
  return {
    kind: kindOfCall(name, args),
    line: formatToolLine(name, args),
  };
}

export function summarizeToolGroup(
  items: { name: string; args?: unknown; result?: unknown }[],
): string {
  if (!items.length) return "";
  if (items.length === 1) return formatToolLine(items[0].name, items[0].args);

  const counts: Record<string, number> = {};
  const order: ToolKind[] = [];
  for (const item of items) {
    const kind = kindOfCall(item.name, item.args);
    if (!counts[kind]) order.push(kind);
    counts[kind] = (counts[kind] || 0) + 1;
  }

  const verbs = order
    .filter((k) => k !== "tool")
    .map((k, i) => {
      const v = VERB[k] || k;
      return i === 0 ? v.charAt(0).toUpperCase() + v.slice(1) : v.toLowerCase();
    });
  const files = (counts.read || 0) + (counts.write || 0) + (counts.edit || 0) + (counts.delete || 0);
  const extra: string[] = [];
  if (files) extra.push(files === 1 ? "1 file" : files + " files");
  if (counts.glob) extra.push(counts.glob === 1 ? "1 glob" : counts.glob + " globs");
  if (counts.grep) extra.push(counts.grep === 1 ? "1 grep" : counts.grep + " greps");
  if (counts.list) extra.push(counts.list === 1 ? "1 path" : counts.list + " paths");
  if (counts.command) extra.push(counts.command === 1 ? "1 command" : counts.command + " commands");
  if (counts.fetch) extra.push(counts.fetch === 1 ? "1 fetch" : counts.fetch + " fetches");
  if (counts.search) extra.push(counts.search === 1 ? "1 search" : counts.search + " searches");
  if (counts.explore) extra.push(counts.explore === 1 ? "1 explore" : counts.explore + " explores");
  if (counts.todo) extra.push(counts.todo === 1 ? "1 todo" : counts.todo + " todos");
  if (!verbs.length) return items.length + " tools";
  return extra.length ? verbs.join(", ") + " " + extra.join(", ") : verbs.join(", ");
}

export function thinkPreview(content: unknown): string {
  const text = typeof content === "string" ? content : "";
  const compact = text.replace(/\s+/g, " ").trim();
  if (!compact) return "";
  const chars = Array.from(compact);
  if (chars.length <= 180) return compact;
  return "…" + chars.slice(-179).join("");
}
