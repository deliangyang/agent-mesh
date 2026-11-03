import assert from "node:assert/strict";
import { describe, it } from "node:test";
import {
  SETTINGS_SHORTCUTS,
  chordTokens,
  modKeyLabel,
  prefersMetaMod,
  shortcutsByScope,
} from "./settingsShortcuts.ts";

describe("settingsShortcuts", () => {
  it("lists the known shortcut ids", () => {
    assert.deepEqual(
      SETTINGS_SHORTCUTS.map((s) => s.id),
      ["open-settings", "leave-settings", "settings-next", "settings-prev", "dismiss-ask"],
    );
  });

  it("detects Apple platforms for the meta modifier", () => {
    assert.equal(prefersMetaMod("Mozilla/5.0 (Macintosh; Intel Mac OS X)", "MacIntel"), true);
    assert.equal(prefersMetaMod("Mozilla/5.0 (iPhone; CPU iPhone OS)", "iPhone"), true);
    assert.equal(prefersMetaMod("Mozilla/5.0 (X11; Linux x86_64)", "Linux x86_64"), false);
    assert.equal(prefersMetaMod("Mozilla/5.0 (Windows NT 10.0)", "Win32"), false);
  });

  it("labels the modifier key for the platform", () => {
    assert.equal(modKeyLabel("Macintosh", "MacIntel"), "⌘");
    assert.equal(modKeyLabel("Linux", "Linux x86_64"), "Ctrl");
  });

  it("flattens chords into display tokens", () => {
    assert.deepEqual(chordTokens({ kind: "mod", key: "," }, "Ctrl"), ["Ctrl", ","]);
    assert.deepEqual(chordTokens({ kind: "keys", keys: ["↓", "j"] }, "Ctrl"), ["↓", "j"]);
  });

  it("filters shortcuts by scope", () => {
    assert.deepEqual(
      shortcutsByScope("settings").map((s) => s.id),
      ["leave-settings", "settings-next", "settings-prev"],
    );
  });
});
