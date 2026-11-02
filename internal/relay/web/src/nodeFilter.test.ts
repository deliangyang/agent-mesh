import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { filterNodesByOs, nodePlatforms } from "./nodeFilter.ts";

describe("nodePlatforms", () => {
  it("returns stable unique platforms", () => {
    assert.deepEqual(
      nodePlatforms([
        { id: "a", os: "windows" },
        { id: "b", os: "darwin" },
        { id: "c", os: "windows" },
        { id: "d" },
      ]),
      ["darwin", "windows"],
    );
  });
});

describe("filterNodesByOs", () => {
  it("filters or returns all", () => {
    const nodes = [
      { id: "a", os: "linux" },
      { id: "b", os: "windows" },
    ];
    assert.equal(filterNodesByOs(nodes, "").length, 2);
    assert.deepEqual(
      filterNodesByOs(nodes, "windows").map((n) => n.id),
      ["b"],
    );
  });
});
