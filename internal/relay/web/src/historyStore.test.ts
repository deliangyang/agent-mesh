import assert from "node:assert/strict";
import { afterEach, before, describe, it } from "node:test";
import { loadStoredHistory, saveStoredHistory } from "./historyStore.ts";
import type { MeshEvent } from "./types.ts";

const mem = new Map<string, string>();

before(() => {
  const store = {
    getItem(key: string) {
      return mem.has(key) ? mem.get(key)! : null;
    },
    setItem(key: string, value: string) {
      mem.set(key, String(value));
    },
    removeItem(key: string) {
      mem.delete(key);
    },
    clear() {
      mem.clear();
    },
    key() {
      return null;
    },
    get length() {
      return mem.size;
    },
  };
  Object.defineProperty(globalThis, "localStorage", { value: store, configurable: true });
});

afterEach(() => mem.clear());

describe("historyStore", () => {
  it("round-trips a transcript tail", () => {
    const evs: MeshEvent[] = [
      { type: "user", text: "hi" },
      { type: "assistant", text: "hello" },
    ];
    saveStoredHistory("n|claude|s1", evs);
    assert.deepEqual(loadStoredHistory("n|claude|s1"), evs);
  });

  it("caps stored events to the newest 300", () => {
    const evs: MeshEvent[] = Array.from({ length: 350 }, (_, i) => ({ type: "assistant", text: String(i) }));
    saveStoredHistory("n|claude|big", evs);
    const got = loadStoredHistory("n|claude|big") || [];
    assert.equal(got.length, 300);
    assert.equal(got[0].text, "50");
  });

  it("keeps only the newest 12 threads", () => {
    for (let i = 0; i < 15; i++) {
      saveStoredHistory("n|claude|t" + i, [{ type: "user", text: "m" + i }]);
    }
    assert.equal(loadStoredHistory("n|claude|t0"), null);
    assert.ok(loadStoredHistory("n|claude|t14"));
  });
});
