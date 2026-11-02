import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { chunkFingerprint, nextPiece, rememberChunk } from "./liveDedup.ts";

describe("liveDedup", () => {
  it("fingerprints distinguish content and collapse exact repeats", () => {
    const a = {
      id: "chatcmpl-1",
      choices: [{ delta: { content: "Hi" } }],
      agent_mesh: { type: "delta", session: "s1" },
    };
    const b = {
      id: "chatcmpl-1",
      choices: [{ delta: { content: "Hi" } }],
      agent_mesh: { type: "delta", session: "s1" },
    };
    const c = {
      id: "chatcmpl-1",
      choices: [{ delta: { content: "Hi!" } }],
      agent_mesh: { type: "delta", session: "s1" },
    };
    const seen = new Set<string>();
    assert.equal(rememberChunk(seen, chunkFingerprint(a)), false);
    assert.equal(rememberChunk(seen, chunkFingerprint(b)), true);
    assert.equal(rememberChunk(seen, chunkFingerprint(c)), false);
  });

  it("nextPiece strips cumulative prefixes", () => {
    assert.equal(nextPiece("Hel", "Hello"), "lo");
    assert.equal(nextPiece("Hello", "Hello"), "");
    assert.equal(nextPiece("", "Hi"), "Hi");
  });
});
