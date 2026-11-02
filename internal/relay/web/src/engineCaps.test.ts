import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { capsAllowDiff, capsAllowFollowups, engineCaps } from "./engineCaps.ts";

describe("engineCaps", () => {
  it("defaults to all-on when missing", () => {
    assert.deepEqual(engineCaps([], "n1", "claude"), {
      ask: true,
      tools: true,
      diff: true,
      followups: true,
    });
  });

  it("reads caps from the matching engine row", () => {
    const caps = engineCaps(
      [{ name: "pi", node_id: "n1", available: true, caps: { tools: true, diff: false } }],
      "n1",
      "pi",
    );
    assert.equal(capsAllowDiff(caps), false);
    assert.equal(capsAllowFollowups(caps), true);
  });
});
