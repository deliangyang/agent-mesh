import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { SETTINGS_NAV, resolveSettingsSection } from "./settingsNav.ts";

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
});
