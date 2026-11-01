import assert from "node:assert/strict";
import { test } from "node:test";
import { formatToolLine, thinkPreview } from "./toolSummary.ts";

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
