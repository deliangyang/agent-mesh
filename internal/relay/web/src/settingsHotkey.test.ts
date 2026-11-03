import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { isOpenSettingsHotkey } from "./settingsHotkey.ts";

function chord(partial: Partial<{ key: string; metaKey: boolean; ctrlKey: boolean; altKey: boolean; shiftKey: boolean }>) {
  return {
    key: ",",
    metaKey: false,
    ctrlKey: false,
    altKey: false,
    shiftKey: false,
    ...partial,
  };
}

describe("isOpenSettingsHotkey", () => {
  it("accepts Ctrl+, and Meta+,", () => {
    assert.equal(isOpenSettingsHotkey(chord({ ctrlKey: true })), true);
    assert.equal(isOpenSettingsHotkey(chord({ metaKey: true })), true);
  });

  it("rejects bare comma and modified chords", () => {
    assert.equal(isOpenSettingsHotkey(chord({})), false);
    assert.equal(isOpenSettingsHotkey(chord({ ctrlKey: true, shiftKey: true })), false);
    assert.equal(isOpenSettingsHotkey(chord({ ctrlKey: true, key: "s" })), false);
  });
});
