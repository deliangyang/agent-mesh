import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { buildPlatformGroups, missingEnginesOnOs } from "./platformGroups.ts";

describe("buildPlatformGroups", () => {
  it("returns empty when there are no nodes", () => {
    assert.deepEqual(buildPlatformGroups([], []), []);
  });

  it("groups nodes by OS and tallies engine coverage", () => {
    const groups = buildPlatformGroups(
      [
        { id: "n1", name: "desk", os: "linux", arch: "amd64" },
        { id: "n2", name: "lap", os: "darwin", arch: "arm64" },
        { id: "n3", name: "win", os: "windows", arch: "amd64" },
        { id: "n4", name: "mystery" },
      ],
      [
        { name: "claude", node_id: "n1", available: true },
        { name: "cursor", node_id: "n1", available: false },
        { name: "claude", node_id: "n2", available: true },
        { name: "cursor", node_id: "n3", available: true },
      ],
    );
    assert.deepEqual(
      groups.map((g) => g.osKey),
      ["linux", "darwin", "windows", "unknown"],
    );
    assert.equal(groups[0].label, "Linux");
    assert.equal(groups[0].nodeCount, 1);
    assert.equal(groups[0].ok, 1);
    assert.equal(groups[0].miss, 1);
    assert.equal(groups[0].nodes[0].platform, "Linux/amd64");
    assert.deepEqual(groups[0].missingEngines, [{ name: "cursor", title: "cursor" }]);
    assert.equal(groups[1].label, "macOS");
    assert.equal(groups[1].ok, 1);
    assert.equal(groups[1].miss, 0);
    assert.deepEqual(groups[1].missingEngines, []);
    assert.equal(groups[3].label, "Unknown");
    assert.equal(groups[3].ok, 0);
  });

  it("dedupes engine names per node when tallied", () => {
    const groups = buildPlatformGroups(
      [{ id: "n1", name: "a", os: "linux", arch: "amd64" }],
      [
        { name: "claude", node_id: "n1", available: true },
        { name: "claude", node_id: "n1", available: true },
      ],
    );
    assert.equal(groups[0].ok, 1);
    assert.equal(groups[0].miss, 0);
  });

  it("lists distinct missing engines for an OS across nodes", () => {
    const nodes = [
      { id: "a", name: "a", os: "linux" },
      { id: "b", name: "b", os: "linux" },
      { id: "c", name: "c", os: "darwin" },
    ];
    const engines = [
      { name: "claude", title: "Claude", node_id: "a", available: false },
      { name: "claude", title: "Claude", node_id: "b", available: false },
      { name: "cursor", node_id: "a", available: false },
      { name: "pi", node_id: "c", available: false },
      { name: "codex", node_id: "a", available: true },
    ];
    assert.deepEqual(missingEnginesOnOs(nodes, engines, "linux"), [
      { name: "claude", title: "Claude" },
      { name: "cursor", title: "cursor" },
    ]);
    assert.deepEqual(missingEnginesOnOs(nodes, engines, "darwin"), [{ name: "pi", title: "pi" }]);
    assert.deepEqual(missingEnginesOnOs(nodes, engines, "windows"), []);
  });
});
