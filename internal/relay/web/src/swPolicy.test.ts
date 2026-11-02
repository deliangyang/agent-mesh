import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { isMeshAPIRequest, isMeshStaticAsset } from "./swPolicy.ts";

describe("swPolicy", () => {
  it("blocks API and auth paths", () => {
    for (const p of ["/v1", "/v1/models", "/v1/openai/sessions", "/login", "/logout", "/api/x"]) {
      assert.equal(isMeshAPIRequest(p), true, p);
      assert.equal(isMeshStaticAsset(p), false, p);
    }
  });

  it("allows shell and hashed assets", () => {
    for (const p of ["/", "/index.html", "/manifest.webmanifest", "/favicon.svg", "/assets/index-abc.js", "/assets/x.css"]) {
      assert.equal(isMeshAPIRequest(p), false, p);
      assert.equal(isMeshStaticAsset(p), true, p);
    }
  });

  it("does not treat arbitrary app routes as static", () => {
    assert.equal(isMeshStaticAsset("/chat"), false);
    assert.equal(isMeshStaticAsset("/settings"), false);
  });
});
