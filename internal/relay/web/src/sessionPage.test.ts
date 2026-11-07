import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { applySessionIncrement, applySessionPage } from "./sessionPage.ts";
import type { Session } from "./types.ts";

function row(id: string, updated: number, extra: Partial<Session> = {}): Session {
  return { id, engine: "claude", node_id: "n1", title: id, updated_at: updated, ...extra };
}

describe("session pages", () => {
  it("appends an older page without dropping or truncating the first", () => {
    const first = applySessionPage([], [row("a", 40), row("b", 30)], false, "n1", "claude");
    const both = applySessionPage(first, [row("c", 20), row("d", 10)], true, "n1", "claude");
    assert.deepEqual(
      both.map((s) => s.id),
      ["a", "b", "c", "d"],
    );
  });

  it("keeps appended rows when a later increment touches a newer session", () => {
    const loaded = applySessionPage(
      [],
      [row("a", 40), row("b", 30), row("c", 20), row("d", 10)],
      false,
      "n1",
      "claude",
    );
    const next = applySessionIncrement(loaded, [row("a", 50, { title: "renamed" })], []);
    assert.deepEqual(
      next.map((s) => s.id),
      ["a", "b", "c", "d"],
    );
    assert.equal(next[0].title, "renamed");
  });

  it("does not let a reset-shaped single row erase older pages", () => {
    const loaded = applySessionPage([], [row("a", 40), row("b", 10)], false, "n1", "claude");
    const next = applySessionIncrement(loaded, [row("a", 40, { title: "still" })], []);
    assert.deepEqual(
      next.map((s) => s.id),
      ["a", "b"],
    );
  });

  it("clears git only when the increment says false", () => {
    const loaded = applySessionIncrement([], [row("a", 40, { git: true })], []);
    const kept = applySessionIncrement(loaded, [row("a", 41, { title: "next" })], []);
    assert.equal(kept[0].git, true);
    const cleared = applySessionIncrement(kept, [{ ...row("a", 42), git: false }], []);
    assert.equal(cleared[0].git, false);
  });
});
