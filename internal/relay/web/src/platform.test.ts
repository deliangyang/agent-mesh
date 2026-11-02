import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { platformLabel } from "./platform.ts";

describe("platformLabel", () => {
  it("returns empty when os is missing", () => {
    assert.equal(platformLabel(undefined), "");
    assert.equal(platformLabel({}), "");
    assert.equal(platformLabel({ arch: "amd64" }), "");
  });

  it("maps known GOOS names", () => {
    assert.equal(platformLabel({ os: "darwin", arch: "arm64" }), "macOS/arm64");
    assert.equal(platformLabel({ os: "windows", arch: "amd64" }), "Windows/amd64");
    assert.equal(platformLabel({ os: "linux" }), "Linux");
  });

  it("passes through unknown os", () => {
    assert.equal(platformLabel({ os: "plan9", arch: "amd64" }), "plan9/amd64");
  });
});
