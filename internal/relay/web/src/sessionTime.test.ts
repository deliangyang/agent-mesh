import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { formatSessionUpdated } from "./sessionTime.ts";

describe("formatSessionUpdated", () => {
  const now = new Date(2026, 10, 7, 21, 30, 0); // Nov 7 2026 local
  const nowMs = now.getTime();

  it("returns empty for missing stamp", () => {
    assert.equal(formatSessionUpdated(0, { nowMs }), "");
  });

  it("shows clock time within the past week", () => {
    const stamp = Math.floor(new Date(2026, 10, 7, 14, 5, 0).getTime() / 1000);
    const out = formatSessionUpdated(stamp, { nowMs, locale: "en" });
    assert.match(out, /14:05/);
  });

  it("shows month/day for older same-year sessions", () => {
    const stamp = Math.floor(new Date(2026, 8, 1, 9, 0, 0).getTime() / 1000);
    const out = formatSessionUpdated(stamp, { nowMs, locale: "en" });
    assert.match(out, /9/);
    assert.match(out, /1/);
    assert.doesNotMatch(out, /:/);
  });

  it("includes year for older cross-year sessions", () => {
    const stamp = Math.floor(new Date(2025, 0, 15, 12, 0, 0).getTime() / 1000);
    const out = formatSessionUpdated(stamp, { nowMs, locale: "en" });
    assert.match(out, /2025/);
  });
});
