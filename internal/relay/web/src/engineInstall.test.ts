import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { applyLocalePref } from "./locale.ts";
import { formatEngineInstall, osBucket } from "./engineInstall.ts";

applyLocalePref("en");

describe("osBucket", () => {
  it("maps GOOS and aliases", () => {
    assert.equal(osBucket("windows"), "windows");
    assert.equal(osBucket("win32"), "windows");
    assert.equal(osBucket("darwin"), "darwin");
    assert.equal(osBucket("macos"), "darwin");
    assert.equal(osBucket("linux"), "linux");
    assert.equal(osBucket(""), "default");
  });
});

describe("formatEngineInstall", () => {
  it("includes env override and Windows search paths", () => {
    const s = formatEngineInstall({ name: "claude", title: "Claude Code" }, "windows");
    assert.match(s, /Claude Code/);
    assert.match(s, /CLAUDE_BIN/);
    assert.match(s, /USERPROFILE/);
  });

  it("uses darwin paths for macOS nodes", () => {
    const s = formatEngineInstall({ name: "pi" }, "darwin");
    assert.match(s, /PI_BIN/);
    assert.match(s, /\.npm-global/);
  });
});
