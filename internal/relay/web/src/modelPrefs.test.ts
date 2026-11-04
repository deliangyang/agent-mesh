import assert from "node:assert/strict";
import { afterEach, before, describe, it } from "node:test";
import { modelName, modelsForEngine, resolveModelPref, saveModelPref } from "./modelPrefs.ts";
import type { ModelCard } from "./types.ts";

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

const models: ModelCard[] = [
  { id: "n1/claude/opus", owned_by: "claude", thinking: ["low", "high"] },
  { id: "n1/claude/sonnet", owned_by: "claude", thinking: ["low", "high"] },
  { id: "n1/cursor/auto", owned_by: "cursor" },
  { id: "n2/claude/opus", owned_by: "claude", thinking: ["low", "high"] },
];

describe("modelPrefs", () => {
  it("strips node and engine prefixes", () => {
    assert.equal(modelName("n1/claude/opus", "claude"), "opus");
    assert.equal(modelName("claude/sonnet", "claude"), "sonnet");
    assert.equal(modelName("plain", "claude"), "plain");
  });

  it("filters models by engine and node", () => {
    assert.deepEqual(
      modelsForEngine(models, "claude", "n1").map((m) => m.id),
      ["n1/claude/opus", "n1/claude/sonnet"],
    );
    assert.equal(modelsForEngine(models, "cursor").length, 1);
  });

  it("uses the stored model and drops an unsupported thinking level", () => {
    saveModelPref("claude", { model: "sonnet", thinking: "high" });
    const hit = resolveModelPref("claude", models, "n1");
    assert.equal(hit.model, "n1/claude/sonnet");
    assert.equal(hit.thinking, "high");
    saveModelPref("claude", { model: "sonnet", thinking: "max" });
    assert.equal(resolveModelPref("claude", models, "n1").thinking, "");
  });

  it("falls back to the first model when the stored one is gone", () => {
    saveModelPref("claude", { model: "retired" });
    assert.equal(resolveModelPref("claude", models, "n2").model, "n2/claude/opus");
  });
});
