import assert from "node:assert/strict";
import { test } from "node:test";
import { formatToolLine, formatToolResult, summarizeToolGroup, thinkPreview } from "./toolSummary.ts";

test("Glob 使用 glob_pattern", () => {
  assert.equal(formatToolLine("Glob", { glob_pattern: "**/*.go" }), 'Globbed "**/*.go"');
  assert.equal(
    formatToolLine("Glob", { glob_pattern: "*", target_directory: "/tmp/proj" }),
    'Globbed "*" in /tmp/proj',
  );
});

test("Read 把裸路径当文件", () => {
  assert.equal(formatToolLine("Read", "/tmp/Makefile"), "Read /tmp/Makefile");
  assert.equal(formatToolLine("Read", { path: "/tmp/go.mod" }), "Read /tmp/go.mod");
});

test("单条 Read 作为标题", () => {
  assert.equal(
    summarizeToolGroup([{ name: "Read", args: { path: "/tmp/a.ts" } }]),
    "Read /tmp/a.ts",
  );
});

test("多条工具标题按 Cursor 格式：动词在前，计数在后", () => {
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

test("两条 Read 汇总成 Read 2 files", () => {
  assert.equal(
    summarizeToolGroup([
      { name: "Read", args: { path: "a.ts" } },
      { name: "Read", args: { path: "b.ts" } },
    ]),
    "Read 2 files",
  );
});

test("Grep 结果行显示 Found N matches", () => {
  assert.equal(
    formatToolResult("Grep", "a.ts:1:foo\nb.ts:2:bar\n"),
    "Found 2 matches",
  );
  assert.equal(formatToolResult("Grep", "Found 133 matches\n"), "Found 133 matches");
  assert.equal(formatToolResult("Read", "file contents"), "");
});

test("MCP 工具显示 Explored available MCP tools", () => {
  assert.equal(
    formatToolLine("GetDynamicTools", { namespace: "cursor", toolName: "TodoWrite" }),
    "Explored available MCP tools cursor · TodoWrite",
  );
});

test("CallDynamicTool 里的 TodoWrite 按待办显示", () => {
  assert.equal(
    formatToolLine("CallDynamicTool", {
      namespace: "cursor",
      toolName: "TodoWrite",
      arguments: { merge: false, todos: [{ id: "1", content: "改界面", status: "pending" }] },
    }),
    "Updated todos",
  );
  assert.equal(formatToolLine("TodoWrite", { todos: [{ id: "1", content: "改界面", status: "pending" }] }), "Updated todos");
});

test("进行中的 Read 用现在分词", () => {
  assert.equal(formatToolLine("Read", { path: "/tmp/a.ts" }, "executing"), "Reading /tmp/a.ts");
});

test("thinkPreview 不切开中文", () => {
  const short = "请用 markdown 显示完整内容";
  assert.equal(thinkPreview(short), short);

  const long = "显示".repeat(120);
  const got = thinkPreview(long);
  assert.equal(got.startsWith("…"), true);
  assert.equal(Array.from(got).length, 180);
  assert.equal(got.includes("\uFFFD"), false);
  assert.equal(got.endsWith("显示"), true);
});
