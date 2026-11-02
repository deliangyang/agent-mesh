import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { describe, it } from "node:test";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");

describe("PWA manifest", () => {
  it("is valid JSON with standalone display", () => {
    const raw = readFileSync(join(root, "public/manifest.webmanifest"), "utf8");
    const m = JSON.parse(raw) as { name?: string; display?: string; start_url?: string; icons?: unknown[] };
    assert.equal(m.name, "Agent Mesh");
    assert.equal(m.display, "standalone");
    assert.equal(m.start_url, "/");
    assert.ok(Array.isArray(m.icons) && m.icons.length > 0);
  });

  it("is linked from index.html", () => {
    const html = readFileSync(join(root, "index.html"), "utf8");
    assert.match(html, /rel="manifest"/);
    assert.match(html, /manifest\.webmanifest/);
  });
});
