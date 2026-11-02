export type TodoStatus = "pending" | "in_progress" | "completed" | "cancelled";

export type TodoItem = {
  id: string;
  content: string;
  activeForm?: string;
  status: TodoStatus;
};

export type TodoPatch = {
  items: TodoItem[];
  merge: boolean;
};

type Toolish = {
  id?: string;
  role?: string;
  toolCalls?: { function?: { name?: string; arguments?: unknown } }[];
};

function asObject(v: unknown): Record<string, unknown> {
  if (v && typeof v === "object" && !Array.isArray(v)) return v as Record<string, unknown>;
  if (typeof v === "string") {
    const s = v.trim();
    if (!s) return {};
    try {
      const parsed = JSON.parse(s) as unknown;
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
        return parsed as Record<string, unknown>;
      }
    } catch {
      return {};
    }
  }
  return {};
}

function str(v: unknown): string {
  return typeof v === "string" ? v.trim() : "";
}

function toolKey(name: string): string {
  return String(name || "")
    .trim()
    .toLowerCase()
    .replace(/[\s_-]+/g, "");
}

function todoName(name: string): boolean {
  const n = toolKey(name);
  if (!n) return false;
  return (
    n === "todowrite" ||
    n === "writetodos" ||
    n === "updatetodos" ||
    n === "todo" ||
    n === "todos" ||
    n.endsWith("todowrite")
  );
}

function isDynamicCall(name: string): boolean {
  return toolKey(name) === "calldynamictool";
}

export function isTodoTool(name: string, args?: unknown): boolean {
  if (todoName(name)) return true;
  if (!isDynamicCall(name)) return false;
  const obj = asObject(args);
  const inner = str(obj.toolName) || str(obj.tool_name) || str(obj.name);
  return todoName(inner);
}

function todoPayload(name: string, args: unknown): Record<string, unknown> {
  const obj = asObject(args);
  if (!isDynamicCall(name)) return obj;
  const inner = asObject(obj.arguments ?? obj.args ?? obj.input);
  if (Array.isArray(inner.todos) || Array.isArray(inner.items) || Array.isArray(inner.tasks)) {
    return inner;
  }
  if (Array.isArray(obj.todos) || Array.isArray(obj.items) || Array.isArray(obj.tasks)) {
    return obj;
  }
  return inner;
}

function normalizeStatus(v: unknown): TodoStatus {
  const n = toolKey(typeof v === "string" ? v : "");
  if (n === "inprogress" || n === "running" || n === "active" || n === "doing" || n === "started") {
    return "in_progress";
  }
  if (n === "completed" || n === "complete" || n === "done" || n === "success" || n === "finished") {
    return "completed";
  }
  if (n === "cancelled" || n === "canceled" || n === "skipped") return "cancelled";
  return "pending";
}

function parseItem(v: unknown): TodoItem | null {
  if (typeof v === "string" && v.trim()) {
    const content = v.trim();
    return { id: content, content, status: "pending" };
  }
  const m = asObject(v);
  const content = str(m.content) || str(m.title) || str(m.text) || str(m.task) || str(m.description);
  const id = str(m.id) || content;
  if (!id) return null;
  const activeForm = str(m.activeForm) || str(m.active_form);
  return {
    id,
    content,
    activeForm: activeForm || undefined,
    status: normalizeStatus(m.status ?? m.state),
  };
}

export function parseTodoCall(name: string, args: unknown): TodoPatch | null {
  const payload = todoPayload(name, args);
  const rows = payload.todos ?? payload.items ?? payload.tasks;
  if (!Array.isArray(rows) || !rows.length) return null;
  if (!isTodoTool(name, args) && !todoName(name)) return null;
  const items: TodoItem[] = [];
  for (const row of rows) {
    const item = parseItem(row);
    if (item) items.push(item);
  }
  if (!items.length) return null;
  return {
    items,
    merge: payload.merge === true || payload.merge === "true",
  };
}

export function mergeTodos(prev: TodoItem[], patch: TodoPatch): TodoItem[] {
  if (!patch.merge) {
    return patch.items.filter((it) => it.content);
  }
  const byId = new Map(prev.map((it) => [it.id, it]));
  const order = prev.map((it) => it.id);
  for (const item of patch.items) {
    const old = byId.get(item.id);
    if (!old) {
      if (!item.content) continue;
      order.push(item.id);
      byId.set(item.id, item);
      continue;
    }
    byId.set(item.id, {
      ...old,
      ...item,
      content: item.content || old.content,
      activeForm: item.activeForm || old.activeForm,
    });
  }
  return order.map((id) => byId.get(id)).filter((it): it is TodoItem => !!it);
}

export function todosFromMessages(messages: Toolish[], untilId?: string): TodoItem[] {
  const rows = messages || [];
  const end = untilId ? rows.findIndex((msg) => msg.id === untilId) : -1;
  const slice = end >= 0 ? rows.slice(0, end + 1) : rows;
  let items: TodoItem[] = [];
  for (const msg of slice) {
    if (msg.role === "assistant" && Array.isArray(msg.toolCalls)) {
      for (const tc of msg.toolCalls) {
        const patch = parseTodoCall(tc?.function?.name || "", tc?.function?.arguments);
        if (patch) items = mergeTodos(items, patch);
      }
    }
  }
  return items;
}

export function todoProgress(items: TodoItem[]): { done: number; total: number } {
  const active = items.filter((it) => it.status !== "cancelled");
  const done = active.filter((it) => it.status === "completed").length;
  return { done, total: active.length };
}

export function formatTodoTitle(items: TodoItem[]): string {
  const { done, total } = todoProgress(items);
  if (!total) return "Todos";
  return "Todos " + done + "/" + total;
}

export function todoLabel(item: TodoItem): string {
  if (item.status === "in_progress" && item.activeForm) return item.activeForm.trim();
  return item.content.trim();
}
