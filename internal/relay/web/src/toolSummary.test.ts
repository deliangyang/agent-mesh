import assert from "node:assert/strict";
import { test } from "node:test";
import { applyLocalePref } from "./locale.ts";
import { formatToolLine, formatToolResult, summarizeToolGroup, thinkPreview } from "./toolSummary.ts";

applyLocalePref("en");

test("Glob uses glob_pattern", () => {
  assert.equal(formatToolLine("Glob", { glob_pattern: "**/*.go" }), 'Globbed "**/*.go"');
  assert.equal(
    formatToolLine("Glob", { glob_pattern: "*", target_directory: "/tmp/proj" }),
    'Globbed "*" in /tmp/proj',
  );
});

test("Read treats a bare path as a file", () => {
  assert.equal(formatToolLine("Read", "/tmp/Makefile"), "Read /tmp/Makefile");
  assert.equal(formatToolLine("Read", { path: "/tmp/go.mod" }), "Read /tmp/go.mod");
});

test("Read still shows only the path when args are partial JSON", () => {
  assert.equal(
    formatToolLine("Read", '{"path":"/tmp/Chat.vue"} extra'),
    "Read /tmp/Chat.vue",
  );
  assert.equal(formatToolLine("Read", '/tmp/Chat.vue"}'), "Read /tmp/Chat.vue");
});

test("a single Read is the title", () => {
  assert.equal(
    summarizeToolGroup([{ name: "Read", args: { path: "/tmp/a.ts" } }]),
    "Read /tmp/a.ts",
  );
});

test("multi-tool titles follow Cursor: verb first, count last", () => {
  const title = summarizeToolGroup([
    { name: "Read", args: { path: "a.ts" } },
    { name: "Read", args: { path: "b.ts" } },
    { name: "Glob", args: { glob_pattern: "*.vue" } },
    { name: "Glob", args: { glob_pattern: "*.ts" } },
    { name: "Grep", args: { pattern: "foo" } },
    { name: "Grep", args: { pattern: "bar" } },
    { name: "Grep", args: { pattern: "baz" } },
  ]);
  assert.equal(title, "Read, globbed, grepped 2 files, 2 globs, 3 greps");
});

test("two Reads become Read 2 files", () => {
  assert.equal(
    summarizeToolGroup([
      { name: "Read", args: { path: "a.ts" } },
      { name: "Read", args: { path: "b.ts" } },
    ]),
    "Read 2 files",
  );
});

test("Grep result lines show Found N matches", () => {
  assert.equal(
    formatToolResult("Grep", "a.ts:1:foo\nb.ts:2:bar\n"),
    "Found 2 matches",
  );
  assert.equal(formatToolResult("Grep", "Found 133 matches\n"), "Found 133 matches");
  assert.equal(formatToolResult("Read", "file contents"), "");
});

test("MCP tools show Explored available MCP tools", () => {
  assert.equal(
    formatToolLine("GetDynamicTools", { namespace: "cursor", toolName: "TodoWrite" }),
    "Explored available MCP tools cursor · TodoWrite",
  );
});

test("TodoWrite inside CallDynamicTool shows as todos", () => {
  assert.equal(
    formatToolLine("CallDynamicTool", {
      namespace: "cursor",
      toolName: "TodoWrite",
      arguments: { merge: false, todos: [{ id: "1", content: "change the UI", status: "pending" }] },
    }),
    "Updated todos",
  );
  assert.equal(formatToolLine("TodoWrite", { todos: [{ id: "1", content: "change the UI", status: "pending" }] }), "Updated todos");
});

test("in-progress Read uses the present participle", () => {
  assert.equal(formatToolLine("Read", { path: "/tmp/a.ts" }, "executing"), "Reading /tmp/a.ts");
});

test("thinkPreview does not split Chinese", () => {
  const short = "请用 markdown 显示完整内容";
  assert.equal(thinkPreview(short), short);

  const long = "显示".repeat(120);
  const got = thinkPreview(long);
  assert.equal(got.startsWith("…"), true);
  assert.equal(Array.from(got).length, 180);
  assert.equal(got.includes("\uFFFD"), false);
  assert.equal(got.endsWith("显示"), true);
});
