import assert from "node:assert/strict";
import { test } from "node:test";
import { ensureTurnThink, eventsToMessages, mergeEvents, sessionIdFromChunk, sessionIdFromText } from "./events.ts";

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

test("同一轮工具收成一条，thinking 放在工具之后", () => {
  const msgs = eventsToMessages([
    { type: "user", role: "user", text: "ls" },
    { type: "thinking", text: "先看目录" },
    { type: "tool_call", tool: "Glob", text: '{"glob_pattern":"**/*.go"}' },
    { type: "thinking", text: "再读 Makefile" },
    { type: "tool_call", tool: "Read", text: '{"path":"/tmp/Makefile"}' },
    { type: "assistant", text: "好了" },
  ]);
  const roles = msgs.map((m) => m.role);
  assert.deepEqual(roles, ["user", "assistant", "reasoning", "assistant"]);
  const tools = msgs[1] as { toolCalls?: { function: { name: string } }[] };
  assert.equal(tools.toolCalls?.length, 2);
  assert.equal(tools.toolCalls?.[0]?.function.name, "Glob");
  assert.equal(tools.toolCalls?.[1]?.function.name, "Read");
  assert.equal((msgs[2] as { content?: string }).content, "先看目录再读 Makefile");
  assert.equal((msgs[3] as { content?: string }).content, "好了");
});

test("历史没有 thinking 时仍把本轮思考补进对话", () => {
  const msgs = eventsToMessages([
    { type: "user", role: "user", text: "ls" },
    { type: "tool_call", tool: "Glob", text: '{"glob_pattern":"*"}' },
    { type: "assistant", text: "好了" },
  ]);
  const got = ensureTurnThink(msgs, "先看目录");
  assert.deepEqual(
    got.map((m) => m.role),
    ["user", "assistant", "reasoning", "assistant"],
  );
  assert.equal((got[2] as { content?: string }).content, "先看目录");
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
