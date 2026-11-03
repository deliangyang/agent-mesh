import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { buildConnectionDiag, connectionNodeRows } from "./connectionDiag.ts";

describe("buildConnectionDiag", () => {
  it("counts catalog rows and mirrors link state", () => {
    const d = buildConnectionDiag(
      { state: "live", attempt: 2 },
      {
        nodes: [{ id: "a", name: "A", os: "linux", arch: "amd64" }],
        engines: [
          { name: "claude", node_id: "a", available: true },
          { name: "cursor", node_id: "a", available: true },
        ],
        sessions: [{ id: "1", engine: "claude", node_id: "a" }],
      },
    );
    assert.equal(d.linkState, "live");
    assert.equal(d.attempt, 2);
    assert.equal(d.nodeCount, 1);
    assert.equal(d.engineCount, 2);
    assert.equal(d.sessionCount, 1);
    assert.equal(d.nodes[0]?.platform, "Linux/amd64");
    assert.equal(d.nodes[0]?.engineCount, 2);
  });

  it("tolerates empty catalog", () => {
    const d = buildConnectionDiag({ state: "down", attempt: 0 }, { nodes: [], engines: [], sessions: [] });
    assert.equal(d.linkState, "down");
    assert.equal(d.nodeCount, 0);
    assert.deepEqual(d.nodes, []);
  });
});

describe("connectionNodeRows", () => {
  it("skips unavailable engines when counting", () => {
    const rows = connectionNodeRows(
      [{ id: "n1", name: "studio", os: "darwin", arch: "arm64" }],
      [
        { name: "claude", node_id: "n1", available: true },
        { name: "codex", node_id: "n1", available: false },
      ],
    );
    assert.equal(rows[0]?.platform, "macOS/arm64");
    assert.equal(rows[0]?.engineCount, 1);
  });
});
