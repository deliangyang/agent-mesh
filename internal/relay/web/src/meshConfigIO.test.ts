import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { buildMeshConfigBundle, meshConfigFilename, parseMeshConfigBundle } from "./meshConfigIO.ts";

describe("meshConfigIO", () => {
  it("builds a versioned bundle", () => {
    assert.deepEqual(buildMeshConfigBundle({ relay: " https://r ", password: "x", advertise: " http://a " }), {
      version: 1,
      relay: "https://r",
      password: "x",
      advertise: "http://a",
    });
  });

  it("parses export JSON and relays arrays", () => {
    const a = parseMeshConfigBundle(JSON.stringify({ version: 1, relay: "https://a", password: "", advertise: "" }));
    assert.equal(a?.relay, "https://a");
    const b = parseMeshConfigBundle(JSON.stringify({ relays: ["https://b", "https://c"], password: "p" }));
    assert.equal(b?.relay, "https://b, https://c");
    assert.equal(b?.password, "p");
  });

  it("parses E2E client key fields", () => {
    const raw = JSON.stringify({
      version: 1,
      vault_passphrase: "xyz",
      client_key: { id: "abc", ed25519_seed: "x=", x25519_private: "y=" },
    });
    const b = parseMeshConfigBundle(raw);
    assert.equal(b?.vault_passphrase, "xyz");
    assert.match(b?.client_key || "", /"id":"abc"/);
  });

  it("rejects empty or invalid payloads", () => {
    assert.equal(parseMeshConfigBundle("{"), null);
    assert.equal(parseMeshConfigBundle("{}"), null);
    assert.equal(parseMeshConfigBundle(JSON.stringify({ version: 1 })), null);
  });

  it("names download files with a date stamp", () => {
    assert.equal(meshConfigFilename(new Date("2026-11-03T12:00:00Z")), "mesh-config-20261103.json");
  });
});
