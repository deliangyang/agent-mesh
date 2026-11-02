import assert from "node:assert/strict";
import { test } from "node:test";
import { flagsAfterBusy, flagsAfterChunk, flagsAfterEvents, liveHintMode } from "./liveFlags.ts";

test("thinking chunks light streaming and thinking", () => {
  const next = flagsAfterChunk({ streaming: false, thinking: false }, "thinking", false, false);
  assert.deepEqual(next, { streaming: true, thinking: true });
});

test("body chunks keep streaming and clear thinking", () => {
  const next = flagsAfterChunk({ streaming: true, thinking: true }, "assistant", false, false);
  assert.deepEqual(next, { streaming: true, thinking: false });
});

test("done chunks turn the lamp off after the local run ends", () => {
  const next = flagsAfterChunk({ streaming: true, thinking: true }, "done", true, false);
  assert.deepEqual(next, { streaming: false, thinking: false });
});

test("body chunks with a done marker also turn the lamp off", () => {
  const next = flagsAfterChunk({ streaming: true, thinking: false }, "assistant", true, false);
  assert.deepEqual(next, { streaming: false, thinking: false });
});

test("while a local run is live, a done chunk must not turn the lamp off early", () => {
  const next = flagsAfterChunk({ streaming: true, thinking: false }, "done", true, true);
  assert.deepEqual(next, { streaming: true, thinking: false });
});

test("title chunks do not change the lamp on their own", () => {
  const prev = { streaming: false, thinking: false };
  assert.deepEqual(flagsAfterChunk(prev, "title", false, false), prev);
});

test("busy sessions light the lamp; idle clears it", () => {
  const on = flagsAfterBusy({ streaming: false, thinking: false }, true, false, false, true);
  assert.deepEqual(on, { streaming: true, thinking: false });
  const off = flagsAfterBusy(on, false, true, false, true);
  assert.deepEqual(off, { streaming: false, thinking: false });
});

test("switching to an idle session turns the lamp off immediately", () => {
  const next = flagsAfterBusy({ streaming: true, thinking: true }, false, false, false, false);
  assert.deepEqual(next, { streaming: false, thinking: false });
});

test("catalog idle must not kill the lamp during a local run", () => {
  const prev = { streaming: true, thinking: true };
  assert.deepEqual(flagsAfterBusy(prev, false, true, true, true), prev);
});

test("incremental chat events relight after refresh", () => {
  const next = flagsAfterEvents({ streaming: false, thinking: false }, ["assistant"], true, false);
  assert.deepEqual(next, { streaming: true, thinking: false });
});

test("a full history snapshot that ends on thinking still shows thinking after refresh", () => {
  const next = flagsAfterEvents({ streaming: false, thinking: false }, ["user", "thinking"], false, false);
  assert.deepEqual(next, { streaming: true, thinking: true });
});

test("a full history snapshot that already has an assistant reply does not relight a finished chat", () => {
  const prev = { streaming: false, thinking: false };
  const next = flagsAfterEvents(prev, ["user", "assistant"], false, false);
  assert.deepEqual(next, prev);
});

test("idle shows neither thinking nor the lamp", () => {
  assert.equal(liveHintMode({ streaming: false, thinking: false }), "off");
  assert.equal(liveHintMode({ streaming: false, thinking: true }), "off");
});

test("thinking shows thinking only, not the lamp", () => {
  assert.equal(liveHintMode({ streaming: true, thinking: true }), "think");
});

test("still streaming but past thinking shows only the lamp", () => {
  assert.equal(liveHintMode({ streaming: true, thinking: false }), "live");
});
