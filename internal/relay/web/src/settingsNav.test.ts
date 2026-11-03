import assert from "node:assert/strict";
import { afterEach, describe, it } from "node:test";
import {
  SETTINGS_NAV,
  loadLastSettingsSection,
  resolveSettingsSection,
  saveLastSettingsSection,
} from "./settingsNav.ts";

afterEach(() => {
  try {
    localStorage.removeItem("mesh.settings.section");
  } catch {
    /* ignore */
  }
});

describe("settingsNav", () => {
  it("lists the four settings modules in order", () => {
    assert.deepEqual(
      SETTINGS_NAV.map((n) => n.id),
      ["appearance", "relay", "health", "account"],
    );
  });

  it("resolves known sections and defaults unknown to appearance", () => {
    assert.equal(resolveSettingsSection("relay"), "relay");
    assert.equal(resolveSettingsSection("Health"), "health");
    assert.equal(resolveSettingsSection(""), "appearance");
    assert.equal(resolveSettingsSection("nope"), "appearance");
  });

  it("remembers the last opened section", () => {
    assert.equal(loadLastSettingsSection(), "appearance");
    saveLastSettingsSection("health");
    assert.equal(loadLastSettingsSection(), "health");
  });
});
