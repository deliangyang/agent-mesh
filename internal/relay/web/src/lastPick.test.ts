import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { pickToQuery } from "./lastPick.ts";

describe("pickToQuery", () => {
  it("omits empty fields", () => {
    assert.deepEqual(pickToQuery({}), {});
  });

  it("includes os platform filter", () => {
    assert.deepEqual(pickToQuery({ node: "n1", os: "windows" }), {
      node: "n1",
      os: "windows",
    });
  });
});
