import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { describe, it } from "node:test";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");

describe("service worker", () => {
  it("ships in public/ and refuses API caching", () => {
    const raw = readFileSync(join(root, "public/sw.js"), "utf8");
    assert.match(raw, /mesh-static-v\d+/);
    assert.match(raw, /\/v1\//);
    assert.match(raw, /\/login/);
    assert.match(raw, /\/logout/);
    assert.doesNotMatch(raw, /cache\.put\([^)]*\/v1/);
  });
});
