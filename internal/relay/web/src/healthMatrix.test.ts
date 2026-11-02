import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { buildHealthMatrix, resolveHealthPick } from "./healthMatrix.ts";

describe("buildHealthMatrix", () => {
  it("marks ok / miss / none per node", () => {
    const got = buildHealthMatrix(
      [
        { id: "n1", name: "win", os: "windows", arch: "amd64" },
        { id: "n2", name: "mac", os: "darwin", arch: "arm64" },
      ],
      [
        { name: "claude", title: "Claude", node_id: "n1", available: true },
        { name: "cursor", title: "Cursor", node_id: "n1", available: false },
        { name: "claude", title: "Claude", node_id: "n2", available: true },
      ],
    );
    assert.deepEqual(got.engineNames, ["claude", "cursor"]);
    assert.equal(got.rows[0].platform, "Windows/amd64");
    assert.deepEqual(got.rows[0].cells, ["ok", "miss"]);
    assert.deepEqual(got.rows[1].cells, ["ok", "none"]);
  });

  it("returns empty when catalog is empty", () => {
    const got = buildHealthMatrix([], []);
    assert.deepEqual(got.engineNames, []);
    assert.deepEqual(got.rows, []);
  });
});

describe("resolveHealthPick", () => {
  const engines = [
    { name: "claude", node_id: "n1", available: true },
    { name: "cursor", node_id: "n1", available: false },
    { name: "pi", node_id: "n1", available: true },
  ];

  it("keeps an available engine click", () => {
    assert.deepEqual(resolveHealthPick(engines, "n1", "pi"), { node: "n1", engine: "pi" });
  });

  it("falls back when engine is missing or unavailable", () => {
    assert.deepEqual(resolveHealthPick(engines, "n1", "cursor"), { node: "n1", engine: "claude" });
    assert.deepEqual(resolveHealthPick(engines, "n1"), { node: "n1", engine: "claude" });
  });
});
