import assert from "node:assert/strict";
import { afterEach, before, describe, it } from "node:test";
import { clearNodePeers, listNodePeers, peerByID, removeNodePeer, saveNodePeers } from "./peers.ts";

const mem = new Map<string, string>();

before(() => {
  const store = {
    getItem(key: string) {
      return mem.has(key) ? mem.get(key)! : null;
    },
    setItem(key: string, value: string) {
      mem.set(key, String(value));
    },
    removeItem(key: string) {
      mem.delete(key);
    },
    clear() {
      mem.clear();
    },
    key() {
      return null;
    },
    get length() {
      return mem.size;
    },
  };
  Object.defineProperty(globalThis, "localStorage", { value: store, configurable: true });
});

afterEach(() => {
  mem.clear();
  clearNodePeers();
});

describe("peers", () => {
  it("caches plaintext node pubs for handshake", () => {
    saveNodePeers([
      {
        id: "n1",
        name: "box",
        sign_pub: "AQID",
        x25519_pub: "BAUG",
      },
      {
        id: "n2",
        name: "other",
        sign_pub: "AAAA",
        x25519_pub: "BBBB",
      },
    ]);
    assert.equal(peerByID("n1")?.name, "box");
    assert.equal(listNodePeers().length, 2);
    removeNodePeer("n1");
    assert.equal(peerByID("n1"), undefined);
    assert.equal(listNodePeers().length, 1);
    clearNodePeers();
    assert.equal(listNodePeers().length, 0);
  });
});
