import assert from "node:assert/strict";
import { afterEach, before, describe, it } from "node:test";
import {
  SETTINGS_NAV,
  adjacentSettingsSection,
  loadLastSettingsSection,
  resolveSettingsSection,
  saveLastSettingsSection,
} from "./settingsNav.ts";

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

afterEach(() => {
  mem.clear();
});

describe("settingsNav", () => {
  it("lists the settings modules in order", () => {
    assert.deepEqual(
      SETTINGS_NAV.map((n) => n.id),
      ["appearance", "connection", "relay", "health", "account"],
    );
  });

  it("resolves known sections and defaults unknown to appearance", () => {
    assert.equal(resolveSettingsSection("relay"), "relay");
    assert.equal(resolveSettingsSection("connection"), "connection");
    assert.equal(resolveSettingsSection("Health"), "health");
    assert.equal(resolveSettingsSection(""), "appearance");
    assert.equal(resolveSettingsSection("nope"), "appearance");
  });

  it("remembers the last opened section", () => {
    assert.equal(loadLastSettingsSection(), "appearance");
    saveLastSettingsSection("health");
    assert.equal(loadLastSettingsSection(), "health");
  });

  it("moves to the adjacent settings module and wraps", () => {
    assert.equal(adjacentSettingsSection("appearance", 1), "connection");
    assert.equal(adjacentSettingsSection("account", 1), "appearance");
    assert.equal(adjacentSettingsSection("appearance", -1), "account");
  });
});
