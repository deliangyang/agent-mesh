import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { buildConnectionDiag } from "./connectionDiag.ts";

describe("buildConnectionDiag", () => {
  it("counts catalog rows and mirrors link state", () => {
    const d = buildConnectionDiag(
      { state: "live", attempt: 2 },
      {
        nodes: [{ id: "a", name: "A" }],
        engines: [
          { name: "claude", node_id: "a" },
          { name: "cursor", node_id: "a" },
        ],
        sessions: [{ id: "1", engine: "claude", node_id: "a" }],
      },
    );
    assert.equal(d.linkState, "live");
    assert.equal(d.attempt, 2);
    assert.equal(d.nodeCount, 1);
    assert.equal(d.engineCount, 2);
    assert.equal(d.sessionCount, 1);
  });

  it("tolerates empty catalog", () => {
    const d = buildConnectionDiag({ state: "down", attempt: 0 }, { nodes: [], engines: [], sessions: [] });
    assert.equal(d.linkState, "down");
    assert.equal(d.nodeCount, 0);
  });
});
