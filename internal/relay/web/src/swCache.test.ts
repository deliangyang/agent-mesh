import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { describe, it } from "node:test";
import { fileURLToPath } from "node:url";
import { MESH_SW_CACHE_VERSION, meshServiceWorkerURL, meshStaticCacheName } from "./swCache.ts";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");

describe("swCache", () => {
  it("names caches and registration URLs from the version", () => {
    assert.equal(meshStaticCacheName(2), "mesh-static-v2");
    assert.equal(meshServiceWorkerURL(2), "/sw.js?v=2");
    assert.match(meshStaticCacheName(), /^mesh-static-v\d+$/);
  });

  it("keeps public/sw.js CACHE aligned with MESH_SW_CACHE_VERSION", () => {
    const raw = readFileSync(join(root, "public/sw.js"), "utf8");
    const expected = meshStaticCacheName(MESH_SW_CACHE_VERSION);
    assert.match(raw, new RegExp(`const CACHE = "${expected}"`));
  });
});
