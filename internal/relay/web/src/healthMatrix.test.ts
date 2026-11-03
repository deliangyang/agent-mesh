import assert from "node:assert/strict";
import { describe, it } from "node:test";
import {
  buildHealthMatrix,
  filterHealthMatrix,
  listHealthOsOptions,
  resolveHealthOsFilter,
  resolveHealthPick,
} from "./healthMatrix.ts";

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
    assert.equal(got.rows[0].osKey, "windows");
    assert.deepEqual(got.rows[0].cells, ["ok", "miss"]);
    assert.deepEqual(got.rows[1].cells, ["ok", "none"]);
    assert.equal(got.rows[1].osKey, "darwin");
  });

  it("returns empty when catalog is empty", () => {
    const got = buildHealthMatrix([], []);
    assert.deepEqual(got.engineNames, []);
    assert.deepEqual(got.rows, []);
  });
});

describe("health OS filter", () => {
  const full = buildHealthMatrix(
    [
      { id: "n1", name: "win", os: "windows", arch: "amd64" },
      { id: "n2", name: "mac", os: "darwin", arch: "arm64" },
      { id: "n3", name: "box", os: "linux", arch: "amd64" },
    ],
    [
      { name: "claude", node_id: "n1", available: true },
      { name: "claude", node_id: "n2", available: true },
      { name: "claude", node_id: "n3", available: false },
    ],
  );

  it("resolves query values", () => {
    assert.equal(resolveHealthOsFilter(""), "");
    assert.equal(resolveHealthOsFilter("all"), "");
    assert.equal(resolveHealthOsFilter("Linux"), "linux");
  });

  it("lists OS options in stable order", () => {
    assert.deepEqual(listHealthOsOptions(full), [
      { osKey: "linux", count: 1 },
      { osKey: "darwin", count: 1 },
      { osKey: "windows", count: 1 },
    ]);
  });

  it("filters rows by OS and keeps columns", () => {
    const filtered = filterHealthMatrix(full, "darwin");
    assert.deepEqual(filtered.engineNames, full.engineNames);
    assert.equal(filtered.rows.length, 1);
    assert.equal(filtered.rows[0].nodeId, "n2");
    assert.equal(filterHealthMatrix(full, "").rows.length, 3);
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
