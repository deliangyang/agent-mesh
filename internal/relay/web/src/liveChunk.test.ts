import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { historyBehindLive, liveChunkToEvent } from "./liveChunk.ts";

describe("liveChunkToEvent", () => {
  it("skips ping and title", () => {
    assert.equal(liveChunkToEvent({ agent_mesh: { type: "ping" } }), null);
    assert.equal(liveChunkToEvent({ agent_mesh: { type: "title" }, choices: [{ delta: { content: "x" } }] }), null);
  });

  it("keeps assistant deltas", () => {
    const ev = liveChunkToEvent({
      choices: [{ delta: { content: "hello" } }],
      agent_mesh: { type: "delta" },
    });
    assert.deepEqual(ev, { type: "delta", role: undefined, tool: undefined, text: "hello", preview: undefined, done: undefined });
  });

  it("skips bare done markers", () => {
    assert.equal(
      liveChunkToEvent({
        choices: [{ delta: { content: "" }, finish_reason: "stop" }],
        agent_mesh: { type: "log" },
      }),
      null,
    );
  });
});

describe("historyBehindLive", () => {
  it("treats empty history as behind when live has messages", () => {
    assert.equal(historyBehindLive([], 2, 10), true);
  });

  it("allows equal or richer history", () => {
    assert.equal(historyBehindLive([{ type: "user", text: "hi" }, { type: "assistant", text: "yo" }], 2, 4), false);
  });

  it("flags fewer events or much less text as behind", () => {
    assert.equal(historyBehindLive([{ type: "user", text: "hi" }], 2, 20), true);
    assert.equal(historyBehindLive([{ type: "user", text: "a" }, { type: "assistant", text: "b" }], 2, 500), true);
  });
});
