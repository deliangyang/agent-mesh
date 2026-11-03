import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { describe, it } from "node:test";
import { fileURLToPath } from "node:url";
import { b64Decode, timingSafeEqual } from "./bytes.ts";
import { aesGcmSeal, hkdfKey } from "./crypto.ts";
import { identityFromFile } from "./identity.ts";
import { marshalEnvelope, unmarshalEnvelope } from "./frame.ts";
import {
  ACK_LABEL,
  C2S_LABEL,
  HELLO_LABEL,
  S2C_LABEL,
  beginHandshakeFixed,
  finishHandshakeFixed,
  open,
  seal,
} from "./session.ts";

const root = dirname(fileURLToPath(import.meta.url));
const vectors = JSON.parse(readFileSync(join(root, "vectors.json"), "utf8")) as Record<string, unknown>;

function b64(key: string): Uint8Array {
  return b64Decode(String(vectors[key]));
}

describe("e2e vectors vs Go", () => {
  it("derives hello key and seals hello body", async () => {
    const client = await identityFromFile(vectors.client as { id: string; ed25519_seed: string; x25519_private: string });
    const node = await identityFromFile(vectors.node as { id: string; ed25519_seed: string; x25519_private: string });
    assert.equal(client.id, (vectors.client as { id: string }).id);
    assert.equal(node.id, (vectors.node as { id: string }).id);
    assert.ok(timingSafeEqual(client.x25519Pub, b64Decode((vectors.client as { x25519_pub: string }).x25519_pub)));
    assert.ok(timingSafeEqual(node.x25519Pub, b64Decode((vectors.node as { x25519_pub: string }).x25519_pub)));

    const { env, ss } = await beginHandshakeFixed(
      client,
      { id: node.id, signPub: node.signPub, x25519Pub: node.x25519Pub },
      b64("client_eph_private"),
      new TextEncoder().encode(String(vectors.hello_body)),
      b64("hello_nonce"),
    );
    assert.ok(timingSafeEqual(ss, b64("ss")));
    const helloKey = await hkdfKey(ss, HELLO_LABEL, 32);
    assert.ok(timingSafeEqual(helloKey, b64("hello_key")));
    assert.ok(timingSafeEqual(env.ciphertext, b64("hello_ct")));
    assert.ok(timingSafeEqual(env.ephPub!, b64("client_eph_public")));
    const bin = marshalEnvelope(env);
    assert.ok(timingSafeEqual(bin, b64("hello_env_bin")));
  });

  it("finishes handshake and seals rpc", async () => {
    const client = await identityFromFile(vectors.client as { id: string; ed25519_seed: string; x25519_private: string });
    const node = await identityFromFile(vectors.node as { id: string; ed25519_seed: string; x25519_private: string });
    const ackEnv = unmarshalEnvelope(b64("ack_env_bin"));
    const fin = await finishHandshakeFixed(
      client,
      { id: node.id, signPub: node.signPub, x25519Pub: node.x25519Pub },
      b64("client_eph_private"),
      ackEnv,
    );
    assert.ok(timingSafeEqual(fin.ss, b64("ss")));
    assert.ok(timingSafeEqual(fin.ss2, b64("ss2")));
    assert.ok(timingSafeEqual(fin.ackKey, b64("ack_key")));
    assert.ok(timingSafeEqual(fin.c2s, b64("c2s")));
    assert.ok(timingSafeEqual(fin.s2c, b64("s2c")));
    assert.ok(timingSafeEqual(await hkdfKey(concat(fin.ss, fin.ss2), C2S_LABEL, 32), b64("c2s")));
    assert.ok(timingSafeEqual(await hkdfKey(concat(fin.ss, fin.ss2), S2C_LABEL, 32), b64("s2c")));
    assert.ok(timingSafeEqual(await hkdfKey(concat(fin.ss, fin.ss2), ACK_LABEL, 32), b64("ack_key")));

    const env = await seal(fin.session, new TextEncoder().encode(String(vectors.rpc_plain)), "rpc");
    assert.ok(timingSafeEqual(env.nonce, b64("rpc_nonce")));
    assert.ok(timingSafeEqual(env.ciphertext, b64("rpc_ct")));
    assert.ok(timingSafeEqual(marshalEnvelope(env), b64("rpc_env_bin")));

    const nodeSess = {
      ...fin.session,
      clientSend: false,
      localID: node.id,
      peerID: client.id,
      sendN: 0,
    };
    const opened = await open(nodeSess, env);
    assert.equal(new TextDecoder().decode(opened), String(vectors.rpc_plain));
  });

  it("round-trips binary frame codec", () => {
    const raw = b64("frame_bin");
    const env = unmarshalEnvelope(raw);
    assert.equal(env.kind, "stream");
    assert.equal(new TextDecoder().decode(env.ciphertext), "hi");
    assert.ok(timingSafeEqual(marshalEnvelope(env), raw));
  });

  it("aes-gcm matches hello_ct with known key", async () => {
    const ct = await aesGcmSeal(b64("hello_key"), b64("hello_nonce"), new TextEncoder().encode(String(vectors.hello_body)));
    assert.ok(timingSafeEqual(ct, b64("hello_ct")));
  });
});

function concat(a: Uint8Array, b: Uint8Array): Uint8Array {
  const o = new Uint8Array(a.length + b.length);
  o.set(a);
  o.set(b, a.length);
  return o;
}
