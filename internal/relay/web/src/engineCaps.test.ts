import assert from "node:assert/strict";
import { describe, it } from "node:test";
import {
  capsAllowAsk,
  capsAllowDiff,
  capsAllowFollowups,
  capsBadges,
  engineCaps,
  showAskChrome,
} from "./engineCaps.ts";

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
    assert.equal(capsAllowAsk(caps), false);
  });
});

describe("showAskChrome", () => {
  it("hides ask chrome when caps omit ask and no live prompt", () => {
    assert.equal(showAskChrome({ tools: true }, false), false);
  });

  it("keeps streaming asks visible even without caps.ask", () => {
    assert.equal(showAskChrome({ tools: true }, true), true);
  });

  it("shows chrome for ask-capable engines", () => {
    assert.equal(showAskChrome({ ask: true, tools: true }, false), true);
  });
});

describe("capsBadges", () => {
  it("omits ask when false", () => {
    assert.deepEqual(capsBadges({ tools: true, diff: true, followups: true }), [
      "tools",
      "diff",
      "followups",
    ]);
  });
});
