import assert from "node:assert/strict";
import { test } from "node:test";
import { engineEmptyHint, nodeEngines } from "./engineCards.ts";
import type { EngineRow } from "./types.ts";

const rows: EngineRow[] = [
  { name: "claude", title: "Claude", node_id: "n1", available: true },
  { name: "cursor", title: "Cursor", node_id: "n1", available: false },
  { name: "codex", title: "Codex", node_id: "n2", available: true },
  { name: "claude", title: "Claude", node_id: "n2", available: true },
];

test("nodeEngines is empty until a node is picked", () => {
  assert.deepEqual(nodeEngines(rows, ""), []);
});

test("nodeEngines hides engines the node does not have", () => {
  const got = nodeEngines(rows, "n1");
  assert.equal(got.length, 1);
  assert.equal(got[0].name, "claude");
});

test("nodeEngines does not leak another node's engines", () => {
  const got = nodeEngines(rows, "n2");
  assert.deepEqual(
    got.map((e) => e.name),
    ["codex", "claude"],
  );
});

test("nodeEngines stays empty when the node has nothing installed", () => {
  assert.deepEqual(nodeEngines([{ name: "pi", node_id: "n1", available: false }], "n1"), []);
});

test("engineEmptyHint tells the user to pick a node or install", () => {
  assert.equal(engineEmptyHint(""), "Pick a node first");
  assert.equal(
    engineEmptyHint("n1"),
    "No engines on this node. Install an agent CLI on the machine, then refresh.",
  );
});
