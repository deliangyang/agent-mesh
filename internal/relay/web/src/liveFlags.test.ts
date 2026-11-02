import assert from "node:assert/strict";
import { test } from "node:test";
import { flagsAfterBusy, flagsAfterChunk, flagsAfterEvents, liveHintMode } from "./liveFlags.ts";

test("思考 chunk 点亮 streaming 和 thinking", () => {
  const next = flagsAfterChunk({ streaming: false, thinking: false }, "thinking", false, false);
  assert.deepEqual(next, { streaming: true, thinking: true });
});

test("正文 chunk 保持 streaming，关掉 thinking", () => {
  const next = flagsAfterChunk({ streaming: true, thinking: true }, "assistant", false, false);
  assert.deepEqual(next, { streaming: true, thinking: false });
});

test("结束 chunk 在本地 run 结束后熄灯", () => {
  const next = flagsAfterChunk({ streaming: true, thinking: true }, "done", true, false);
  assert.deepEqual(next, { streaming: false, thinking: false });
});

test("带结束标记的正文 chunk 也会熄灯", () => {
  const next = flagsAfterChunk({ streaming: true, thinking: false }, "assistant", true, false);
  assert.deepEqual(next, { streaming: false, thinking: false });
});

test("本地 run 进行中，结束 chunk 不抢先熄灯", () => {
  const next = flagsAfterChunk({ streaming: true, thinking: false }, "done", true, true);
  assert.deepEqual(next, { streaming: true, thinking: false });
});

test("标题 chunk 不单独改变灯态", () => {
  const prev = { streaming: false, thinking: false };
  assert.deepEqual(flagsAfterChunk(prev, "title", false, false), prev);
});

test("会话 busy 时点亮，busy 落下后熄灯", () => {
  const on = flagsAfterBusy({ streaming: false, thinking: false }, true, false, false, true);
  assert.deepEqual(on, { streaming: true, thinking: false });
  const off = flagsAfterBusy(on, false, true, false, true);
  assert.deepEqual(off, { streaming: false, thinking: false });
});

test("换到空闲会话时立即熄灯", () => {
  const next = flagsAfterBusy({ streaming: true, thinking: true }, false, false, false, false);
  assert.deepEqual(next, { streaming: false, thinking: false });
});

test("本地 run 时 catalog 的 idle 不能把灯掐灭", () => {
  const prev = { streaming: true, thinking: true };
  assert.deepEqual(flagsAfterBusy(prev, false, true, true, true), prev);
});

test("增量对话事件在刷新后重新点亮", () => {
  const next = flagsAfterEvents({ streaming: false, thinking: false }, ["assistant"], true, false);
  assert.deepEqual(next, { streaming: true, thinking: false });
});

test("完整历史快照若落在思考上，刷新后仍显示思考中", () => {
  const next = flagsAfterEvents({ streaming: false, thinking: false }, ["user", "thinking"], false, false);
  assert.deepEqual(next, { streaming: true, thinking: true });
});

test("完整历史快照若已是助手回复，不把已结束的对话点亮", () => {
  const prev = { streaming: false, thinking: false };
  const next = flagsAfterEvents(prev, ["user", "assistant"], false, false);
  assert.deepEqual(next, prev);
});

test("空闲时不显示思考也不显示呼吸灯", () => {
  assert.equal(liveHintMode({ streaming: false, thinking: false }), "off");
  assert.equal(liveHintMode({ streaming: false, thinking: true }), "off");
});

test("思考中只显示思考，不显示呼吸灯", () => {
  assert.equal(liveHintMode({ streaming: true, thinking: true }), "think");
});

test("仍在输出但已离开思考时只显示呼吸灯", () => {
  assert.equal(liveHintMode({ streaming: true, thinking: false }), "live");
});
