import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { explainError, unwrapFailText } from "./toast.ts";

describe("unwrapFailText", () => {
  it("keeps plain error bodies", () => {
    assert.equal(unwrapFailText("no live workbuddy gateway"), "no live workbuddy gateway");
  });

  it("unwraps JSON error strings", () => {
    assert.equal(unwrapFailText('{"error":"pick a working directory"}'), "pick a working directory");
  });

  it("unwraps nested OpenAI-style errors", () => {
    assert.equal(
      unwrapFailText('{"error":{"message":"model not found","type":"invalid_request"}}'),
      "model not found",
    );
  });

  it("uses status when body is empty", () => {
    assert.equal(unwrapFailText("", 502), "Request failed (502)");
  });
});

describe("explainError", () => {
  it("skips abort errors", () => {
    const err = new Error("aborted");
    err.name = "AbortError";
    assert.equal(explainError(err), "");
  });

  it("maps network failures", () => {
    assert.equal(explainError(new Error("Failed to fetch")), "network error; check the connection and try again");
  });

  it("unwraps JSON messages before toasting", () => {
    assert.equal(explainError(new Error('{"error":"engine offline"}')), "engine offline");
  });
});
