import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { clientOsKey, clientPlatformLabel } from "./clientPlatform.ts";

describe("clientPlatform", () => {
  it("maps common user agents", () => {
    assert.equal(clientOsKey("Mozilla/5.0 (Windows NT 10.0; Win64; x64)", "Win32"), "windows");
    assert.equal(clientOsKey("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)", "MacIntel"), "darwin");
    assert.equal(clientOsKey("Mozilla/5.0 (X11; Linux x86_64)", "Linux x86_64"), "linux");
    assert.equal(clientOsKey("Mozilla/5.0 (iPhone; CPU iPhone OS 17_0)", "iPhone"), "ios");
  });

  it("returns display labels", () => {
    assert.equal(clientPlatformLabel("Macintosh", "MacIntel"), "macOS");
    assert.equal(clientPlatformLabel("Linux", "Linux x86_64"), "Linux");
  });
});
