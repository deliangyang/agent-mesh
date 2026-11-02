import assert from "node:assert/strict";
import { test } from "node:test";
import { ensureTurnThink, eventsToMessages, mergeEvents, sameEvents, sameMessages, sessionIdFromChunk, sessionIdFromText } from "./events.ts";

test("mergeEvents 保留 ask", () => {
  const rows = mergeEvents([
    { type: "user", role: "user", text: "hi" },
    { type: "ask", tool: "AskUserQuestion", text: "{\"questions\":[]}" },
    { type: "assistant", text: "ok" },
  ]);
  assert.equal(rows.some((ev) => ev.type === "ask"), true);
  const msgs = eventsToMessages(rows);
  assert.equal(msgs.some((m) => m.role === "user"), true);
  assert.equal(msgs.some((m) => (m as { content?: string }).content === "ok"), true);
});

test("相邻助手 Markdown 段落不会黏在一起", () => {
  const rows = mergeEvents([
    { type: "assistant", text: "## 标题" },
    { type: "assistant", text: "- **取件码**：02001754" },
  ]);
  assert.equal(rows.length, 1);
  assert.equal(rows[0].text, "## 标题\n\n- **取件码**：02001754");
  const msgs = eventsToMessages(rows);
  const asst = msgs.find((m) => m.role === "assistant") as { content?: string };
  assert.equal(asst?.content, "## 标题\n\n- **取件码**：02001754");
});

test("流式 delta 仍直接拼接", () => {
  const rows = mergeEvents([
    { type: "delta", text: "显示" },
    { type: "delta", text: "完整" },
  ]);
  assert.equal(rows.length, 1);
  assert.equal(rows[0].text, "显示完整");
});

test("同一轮工具收成一条，thinking 不进入对话气泡", () => {
  const msgs = eventsToMessages([
    { type: "user", role: "user", text: "ls" },
    { type: "thinking", text: "先看目录" },
    { type: "tool_call", tool: "Glob", text: '{"glob_pattern":"**/*.go"}' },
    { type: "thinking", text: "再读 Makefile" },
    { type: "tool_call", tool: "Read", text: '{"path":"/tmp/Makefile"}' },
    { type: "assistant", text: "好了" },
  ]);
  const roles = msgs.map((m) => m.role);
  assert.deepEqual(roles, ["user", "assistant", "assistant"]);
  const tools = msgs[1] as { toolCalls?: { function: { name: string } }[] };
  assert.equal(tools.toolCalls?.length, 2);
  assert.equal(tools.toolCalls?.[0]?.function.name, "Glob");
  assert.equal(tools.toolCalls?.[1]?.function.name, "Read");
  assert.equal((msgs[2] as { content?: string }).content, "好了");
});

test("本轮思考不再插入 reasoning 消息", () => {
  const msgs = eventsToMessages([
    { type: "user", role: "user", text: "ls" },
    { type: "tool_call", tool: "Glob", text: '{"glob_pattern":"*"}' },
    { type: "assistant", text: "好了" },
  ]);
  const got = ensureTurnThink(msgs, "先看目录");
  assert.deepEqual(
    got.map((m) => m.role),
    ["user", "assistant", "assistant"],
  );
  assert.equal(got, msgs);
});

test("相邻两次不同 Glob 不会被合成一条", () => {
  const msgs = eventsToMessages([
    { type: "user", role: "user", text: "find" },
    { type: "tool_call", tool: "Glob", text: '{"glob_pattern":"*.go"}' },
    { type: "tool_call", tool: "Glob", text: '{"glob_pattern":"*.ts"}' },
  ]);
  const tools = msgs.find((m) => m.role === "assistant") as { toolCalls?: unknown[] };
  assert.equal(tools.toolCalls?.length, 2);
});

test("工具和助手文本按出现顺序交错，不堆在顶部", () => {
  const msgs = eventsToMessages([
    { type: "user", role: "user", text: "看一下" },
    { type: "assistant", text: "先读文件" },
    { type: "tool_call", tool: "Read", text: '{"path":"/tmp/a.ts"}' },
    { type: "tool_result", tool: "Read", text: "export const a = 1" },
    { type: "assistant", text: "再搜一下" },
    { type: "tool_call", tool: "Grep", text: '{"pattern":"a"}' },
    { type: "tool_result", tool: "Grep", text: "a.ts:1:export" },
    { type: "assistant", text: "好了" },
  ]);
  const kinds = msgs.map((m) => {
    if (m.role === "user") return "user";
    if (m.role === "tool") return "tool";
    const asst = m as { content?: string; toolCalls?: { function: { name: string } }[] };
    if (asst.toolCalls?.length) return "tools:" + asst.toolCalls.map((t) => t.function.name).join(",");
    return "text:" + String(asst.content || "");
  });
  assert.deepEqual(kinds, [
    "user",
    "text:先读文件",
    "tools:Read",
    "tool",
    "text:再搜一下",
    "tools:Grep",
    "tool",
    "text:好了",
  ]);
});

test("有结果后再来的工具另起一条，不和前面的工具堆在一起", () => {
  const msgs = eventsToMessages([
    { type: "user", role: "user", text: "find" },
    { type: "tool_call", tool: "Glob", text: '{"glob_pattern":"*.go"}' },
    { type: "tool_result", tool: "Glob", text: "a.go" },
    { type: "tool_call", tool: "Read", text: '{"path":"a.go"}' },
    { type: "tool_result", tool: "Read", text: "package main" },
    { type: "assistant", text: "好了" },
  ]);
  const kinds = msgs.map((m) => {
    if (m.role === "user") return "user";
    if (m.role === "tool") return "tool";
    const asst = m as { content?: string; toolCalls?: { function: { name: string } }[] };
    if (asst.toolCalls?.length) return "tools:" + asst.toolCalls.map((t) => t.function.name).join(",");
    return "text:" + String(asst.content || "");
  });
  assert.deepEqual(kinds, ["user", "tools:Glob", "tool", "tools:Read", "tool", "text:好了"]);
});

test("sameEvents 只比类型和正文，相同快照不再重绘", () => {
  const rows = [
    { type: "user", role: "user", text: "hi" },
    { type: "assistant", text: "ok" },
  ];
  assert.equal(
    sameEvents(rows, [
      { type: "user", role: "user", text: "hi" },
      { type: "assistant", text: "ok" },
    ]),
    true,
  );
  assert.equal(
    sameEvents(rows, [
      { type: "user", role: "user", text: "hi" },
      { type: "assistant", text: "no" },
    ]),
    false,
  );
});

test("sameMessages 按内容比较，忽略新对象", () => {
  const evs = [
    { type: "user", role: "user", text: "hi" },
    { type: "assistant", text: "ok" },
  ];
  const a = eventsToMessages(evs);
  const b = eventsToMessages(evs);
  assert.equal(a === b, false);
  assert.equal(sameMessages(a, b), true);
  assert.equal(sameMessages(a, eventsToMessages([{ type: "user", role: "user", text: "hi" }])), false);
});

test("从 stream JSON 提取 session id", () => {
  assert.equal(
    sessionIdFromChunk({
      agent_mesh: { session: "s-mesh" },
      session_id: "s-top",
    }),
    "s-mesh",
  );
  assert.equal(sessionIdFromChunk({ session_id: "abc-1" }), "abc-1");
  assert.equal(
    sessionIdFromChunk({
      choices: [{ delta: { content: '{"type":"thinking","session_id":"from-dump"}' } }],
    }),
    "from-dump",
  );
  assert.equal(sessionIdFromText('{"type":"system","session_id":"sys-9"}'), "sys-9");
  assert.equal(sessionIdFromText('{"type":"session","id":"pi-1","cwd":"/tmp"}'), "pi-1");
  assert.equal(sessionIdFromChunk({ choices: [{ delta: { content: "hello" } }] }), "");
});
