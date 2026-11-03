import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { applyLocalePref } from "./locale.ts";
import {
  capsAllowAsk,
  capsAllowDiff,
  capsAllowFollowups,
  capsBadgeLabel,
  capsBadgeTitle,
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

describe("capsBadgeLabel", () => {
  it("localizes known badges", () => {
    applyLocalePref("en");
    assert.equal(capsBadgeLabel("tools"), "Tools");
    assert.equal(capsBadgeTitle("diff"), "View workspace git diff");
    applyLocalePref("zh-CN");
    assert.equal(capsBadgeLabel("ask"), "询问");
  });
});
