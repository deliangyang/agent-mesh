import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { keepLiveAfterStream } from "./liveHandoff.ts";

describe("keepLiveAfterStream", () => {
  it("stops after a clean done", () => {
    assert.equal(
      keepLiveAfterStream({ ended: true, sseDropped: false, queued: false, runFailed: false, aborted: false }),
      false,
    );
  });

  it("keeps listening after a dropped SSE even if done already fired", () => {
    assert.equal(
      keepLiveAfterStream({ ended: true, sseDropped: true, queued: false, runFailed: false, aborted: false }),
      true,
    );
  });

  it("keeps listening while the primary stream never marked done", () => {
    assert.equal(
      keepLiveAfterStream({ ended: false, sseDropped: true, queued: false, runFailed: false, aborted: false }),
      true,
    );
  });

  it("stops on failure or abort", () => {
    assert.equal(
      keepLiveAfterStream({ ended: false, sseDropped: true, queued: false, runFailed: true, aborted: false }),
      false,
    );
    assert.equal(
      keepLiveAfterStream({ ended: false, sseDropped: true, queued: false, runFailed: false, aborted: true }),
      false,
    );
  });
});
