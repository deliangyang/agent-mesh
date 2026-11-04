import assert from "node:assert/strict";
import { test } from "node:test";
import { cleanUserText, ensureTurnThink, eventsToMessages, mergeEvents, sameEvents, sameMessages, sessionIdFromChunk, sessionIdFromText } from "./events.ts";

test("mergeEvents keeps ask", () => {
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

test("adjacent assistant markdown paragraphs stay apart", () => {
  const rows = mergeEvents([
    { type: "assistant", text: "## Heading" },
    { type: "assistant", text: "- **pickup code**: 02001754" },
  ]);
  assert.equal(rows.length, 1);
  assert.equal(rows[0].text, "## Heading\n\n- **pickup code**: 02001754");
  const msgs = eventsToMessages(rows);
  const asst = msgs.find((m) => m.role === "assistant") as { content?: string };
  assert.equal(asst?.content, "## Heading\n\n- **pickup code**: 02001754");
});

test("streaming deltas still concatenate", () => {
  const rows = mergeEvents([
    { type: "delta", text: "Hel" },
    { type: "delta", text: "lo" },
  ]);
  assert.equal(rows.length, 1);
  assert.equal(rows[0].text, "Hello");
});

test("tools in one turn collapse; thinking stays out of the bubble", () => {
  const msgs = eventsToMessages([
    { type: "user", role: "user", text: "ls" },
    { type: "thinking", text: "look at the directory first" },
    { type: "tool_call", tool: "Glob", text: '{"glob_pattern":"**/*.go"}' },
    { type: "thinking", text: "then read the Makefile" },
    { type: "tool_call", tool: "Read", text: '{"path":"/tmp/Makefile"}' },
    { type: "assistant", text: "done" },
  ]);
  const roles = msgs.map((m) => m.role);
  assert.deepEqual(roles, ["user", "assistant", "assistant"]);
  const tools = msgs[1] as { toolCalls?: { function: { name: string } }[] };
  assert.equal(tools.toolCalls?.length, 2);
  assert.equal(tools.toolCalls?.[0]?.function.name, "Glob");
  assert.equal(tools.toolCalls?.[1]?.function.name, "Read");
  assert.equal((msgs[2] as { content?: string }).content, "done");
});

test("this turn's thinking is not inserted as reasoning", () => {
  const msgs = eventsToMessages([
    { type: "user", role: "user", text: "ls" },
    { type: "tool_call", tool: "Glob", text: '{"glob_pattern":"*"}' },
    { type: "assistant", text: "done" },
  ]);
  const got = ensureTurnThink(msgs, "look at the directory first");
  assert.deepEqual(
    got.map((m) => m.role),
    ["user", "assistant", "assistant"],
  );
  assert.equal(got, msgs);
});

test("two different Globs in a row are not merged", () => {
  const msgs = eventsToMessages([
    { type: "user", role: "user", text: "find" },
    { type: "tool_call", tool: "Glob", text: '{"glob_pattern":"*.go"}' },
    { type: "tool_call", tool: "Glob", text: '{"glob_pattern":"*.ts"}' },
  ]);
  const tools = msgs.find((m) => m.role === "assistant") as { toolCalls?: unknown[] };
  assert.equal(tools.toolCalls?.length, 2);
});

test("tools and assistant text stay in order instead of stacking at the top", () => {
  const msgs = eventsToMessages([
    { type: "user", role: "user", text: "take a look" },
    { type: "assistant", text: "read the file first" },
    { type: "tool_call", tool: "Read", text: '{"path":"/tmp/a.ts"}' },
    { type: "tool_result", tool: "Read", text: "export const a = 1" },
    { type: "assistant", text: "search again" },
    { type: "tool_call", tool: "Grep", text: '{"pattern":"a"}' },
    { type: "tool_result", tool: "Grep", text: "a.ts:1:export" },
    { type: "assistant", text: "done" },
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
    "text:read the file first",
    "tools:Read",
    "tool",
    "text:search again",
    "tools:Grep",
    "tool",
    "text:done",
  ]);
});

test("sequential Reads stay grouped even with results in between", () => {
  const msgs = eventsToMessages([
    { type: "user", role: "user", text: "read" },
    { type: "tool_call", tool: "Read", text: '{"path":"a.go"}' },
    { type: "tool_result", tool: "Read", text: "a" },
    { type: "tool_call", tool: "Read", text: '{"path":"b.go"}' },
    { type: "tool_result", tool: "Read", text: "b" },
    { type: "tool_call", tool: "Read", text: '{"path":"c.go"}' },
    { type: "tool_result", tool: "Read", text: "c" },
  ]);
  const tools = msgs.find((m) => m.role === "assistant") as { toolCalls?: unknown[] };
  assert.equal(tools.toolCalls?.length, 3);
});

test("later tools in the same turn stay grouped until assistant text", () => {
  const msgs = eventsToMessages([
    { type: "user", role: "user", text: "find" },
    { type: "tool_call", tool: "Glob", text: '{"glob_pattern":"*.go"}' },
    { type: "tool_result", tool: "Glob", text: "a.go" },
    { type: "tool_call", tool: "Read", text: '{"path":"a.go"}' },
    { type: "tool_result", tool: "Read", text: "package main" },
    { type: "assistant", text: "done" },
  ]);
  const kinds = msgs.map((m) => {
    if (m.role === "user") return "user";
    if (m.role === "tool") return "tool";
    const asst = m as { content?: string; toolCalls?: { function: { name: string } }[] };
    if (asst.toolCalls?.length) return "tools:" + asst.toolCalls.map((t) => t.function.name).join(",");
    return "text:" + String(asst.content || "");
  });
  assert.deepEqual(kinds, ["user", "tools:Glob,Read", "tool", "tool", "text:done"]);
});

test("sameEvents compares type and body; identical snapshots do not rerender", () => {
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

test("sameMessages compares content and ignores new objects", () => {
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

test("extract session id from stream JSON", () => {
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

test("system-reminder hook blocks stay out of the user bubble", () => {
  const raw = "<system-reminder>\nUserPromptSubmit hook context\n</system-reminder>\n\nfix the bug";
  const rows = mergeEvents([{ type: "user", role: "user", text: raw }]);
  assert.equal(rows.length, 1);
  assert.equal(rows[0].text, "fix the bug");
});

test("a user turn with only system-reminder blocks is dropped", () => {
  const rows = mergeEvents([
    { type: "user", role: "user", text: "<system-reminder>hook only</system-reminder>" },
  ]);
  assert.equal(rows.length, 0);
});

test("an unclosed system-reminder does not leak its content", () => {
  assert.equal(cleanUserText("hello\n<system-reminder>ctx to eof"), "hello");
});

test("slash-command scaffolding does not render as a user bubble", () => {
  const rows = mergeEvents([
    { type: "user", role: "user", text: "<command-message>mesh-deploy</command-message>\n<command-name>/mesh-deploy</command-name>" },
    { type: "user", role: "user", text: "Base directory for this skill: /repo/.claude/skills/mesh-deploy\n\n# Deploy\nrun it" },
  ]);
  assert.equal(rows.length, 0);
});
