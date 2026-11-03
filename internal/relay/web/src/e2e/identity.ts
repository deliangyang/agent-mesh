import { b64Decode, b64Encode, hexEncode } from "./bytes.ts";
import {
  ed25519PublicFromSeed,
  generateX25519Pair,
  importEd25519Private,
  importX25519Private,
  importX25519Public,
  x25519ECDH,
} from "./crypto.ts";

export type PublicIdentity = {
  id: string;
  signPub: Uint8Array;
  x25519Pub: Uint8Array;
};

export type Identity = {
  id: string;
  ed25519Seed: Uint8Array;
  signPub: Uint8Array;
  x25519Private: Uint8Array;
  x25519Pub: Uint8Array;
  signKey: CryptoKey;
  x25519Key: CryptoKey;
};

type IdentityFile = {
  id: string;
  ed25519_seed: string;
  x25519_private: string;
};

/** X25519 public = ECDH(priv, basepoint u=9). */
async function x25519PublicFromPrivate(privRaw: Uint8Array): Promise<{ pub: Uint8Array; key: CryptoKey }> {
  const key = await importX25519Private(privRaw, false);
  const base = new Uint8Array(32);
  base[0] = 9;
  const pub = await x25519ECDH(key, await importX25519Public(base));
  return { pub, key };
}

export async function generateIdentity(): Promise<Identity> {
  const edSeed = new Uint8Array(32);
  globalThis.crypto.getRandomValues(edSeed);
  const x = await generateX25519Pair();
  const signKey = await importEd25519Private(edSeed);
  const signPub = await ed25519PublicFromSeed(edSeed);
  const id = hexEncode(signPub.slice(0, 8));
  return {
    id,
    ed25519Seed: edSeed,
    signPub,
    x25519Private: x.privateRaw,
    x25519Pub: x.publicRaw,
    signKey,
    x25519Key: x.privateKey,
  };
}

export function publicOf(id: Identity): PublicIdentity {
  return { id: id.id, signPub: id.signPub.slice(), x25519Pub: id.x25519Pub.slice() };
}

export async function identityFromFile(doc: IdentityFile): Promise<Identity> {
  const edSeed = b64Decode(doc.ed25519_seed);
  const xPriv = b64Decode(doc.x25519_private);
  const signKey = await importEd25519Private(edSeed);
  const signPub = await ed25519PublicFromSeed(edSeed);
  const { pub: x25519Pub, key: x25519Key } = await x25519PublicFromPrivate(xPriv);
  const id = doc.id || hexEncode(signPub.slice(0, 8));
  return {
    id,
    ed25519Seed: edSeed,
    signPub,
    x25519Private: xPriv,
    x25519Pub,
    signKey,
    x25519Key,
  };
}

export function identityToFile(id: Identity): IdentityFile {
  return {
    id: id.id,
    ed25519_seed: b64Encode(id.ed25519Seed),
    x25519_private: b64Encode(id.x25519Private),
  };
}

export function identityToJSON(id: Identity): string {
  return JSON.stringify(identityToFile(id), null, 2);
}

export async function identityFromJSON(raw: string): Promise<Identity> {
  return identityFromFile(JSON.parse(raw) as IdentityFile);
}

export function parseNodePublic(row: {
  id: string;
  sign_pub?: unknown;
  x25519_pub?: unknown;
  x25519?: unknown;
}): PublicIdentity {
  const signRaw = row.sign_pub;
  const xRaw = row.x25519_pub ?? row.x25519;
  if (signRaw == null || xRaw == null) throw new Error("missing public keys");
  const signPub = decodeKey(signRaw);
  const x25519Pub = decodeKey(xRaw);
  return { id: row.id, signPub, x25519Pub };
}

function decodeKey(v: unknown): Uint8Array {
  if (typeof v === "string") return b64Decode(v);
  if (v instanceof Uint8Array) return v;
  if (Array.isArray(v)) return new Uint8Array(v);
  // JSON from Go often encodes []byte as base64 string already handled;
  // some proxies may pass {0:..} objects — reject.
  throw new Error("bad key encoding");
}
