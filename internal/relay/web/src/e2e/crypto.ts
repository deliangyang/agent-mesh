import { utf8 } from "./bytes.ts";

const subtle = globalThis.crypto.subtle;

/** PKCS8 wrapper for a 32-byte Ed25519 seed (RFC 8410). */
export function ed25519PKCS8FromSeed(seed: Uint8Array): Uint8Array {
  if (seed.length !== 32) throw new Error("ed25519 seed must be 32 bytes");
  const out = new Uint8Array(48);
  out.set([0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x04, 0x22, 0x04, 0x20], 0);
  out.set(seed, 16);
  return out;
}

export function ed25519SeedFromPKCS8(pkcs8: Uint8Array): Uint8Array {
  if (pkcs8.length < 48) throw new Error("bad ed25519 pkcs8");
  return pkcs8.slice(pkcs8.length - 32);
}

export function ed25519SPKIFromPub(pub: Uint8Array): Uint8Array {
  if (pub.length !== 32) throw new Error("ed25519 pub must be 32 bytes");
  const out = new Uint8Array(44);
  out.set([0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x03, 0x21, 0x00], 0);
  out.set(pub, 12);
  return out;
}

export async function importEd25519Private(seed: Uint8Array): Promise<CryptoKey> {
  return subtle.importKey("pkcs8", ed25519PKCS8FromSeed(seed), "Ed25519", true, ["sign"]);
}

export async function importEd25519Public(pub: Uint8Array): Promise<CryptoKey> {
  return subtle.importKey("spki", ed25519SPKIFromPub(pub), "Ed25519", true, ["verify"]);
}

/** Public key from 32-byte seed (Go ed25519.NewKeyFromSeed). */
export async function ed25519PublicFromSeed(seed: Uint8Array): Promise<Uint8Array> {
  const { getPublicKeyAsync } = await import("./vendor/ed25519.mjs");
  return getPublicKeyAsync(seed);
}

/** PKCS8 wrapper for a 32-byte X25519 private key (RFC 8410). */
export function x25519PKCS8FromPrivate(raw: Uint8Array): Uint8Array {
  if (raw.length !== 32) throw new Error("x25519 private must be 32 bytes");
  const out = new Uint8Array(48);
  out.set([0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x6e, 0x04, 0x22, 0x04, 0x20], 0);
  out.set(raw, 16);
  return out;
}

export function x25519SPKIFromPub(pub: Uint8Array): Uint8Array {
  if (pub.length !== 32) throw new Error("x25519 pub must be 32 bytes");
  const out = new Uint8Array(44);
  out.set([0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x6e, 0x03, 0x21, 0x00], 0);
  out.set(pub, 12);
  return out;
}

export async function importX25519Private(raw: Uint8Array, extractable = false): Promise<CryptoKey> {
  return subtle.importKey("pkcs8", x25519PKCS8FromPrivate(raw), { name: "X25519" }, extractable, ["deriveBits"]);
}

export async function importX25519Public(raw: Uint8Array): Promise<CryptoKey> {
  return subtle.importKey("spki", x25519SPKIFromPub(raw), { name: "X25519" }, true, []);
}

export async function x25519ECDH(priv: CryptoKey, pub: CryptoKey): Promise<Uint8Array> {
  const bits = await subtle.deriveBits({ name: "X25519", public: pub }, priv, 256);
  return new Uint8Array(bits);
}

export async function generateX25519Pair(): Promise<{ privateRaw: Uint8Array; publicRaw: Uint8Array; privateKey: CryptoKey }> {
  const pair = (await subtle.generateKey({ name: "X25519" }, true, ["deriveBits"])) as CryptoKeyPair;
  const pkcs8 = new Uint8Array(await subtle.exportKey("pkcs8", pair.privateKey));
  const spki = new Uint8Array(await subtle.exportKey("spki", pair.publicKey));
  return { privateRaw: pkcs8.slice(-32), publicRaw: spki.slice(-32), privateKey: pair.privateKey };
}

/** HKDF-SHA256 matching Go crypto/hkdf.Key(..., salt="agent-mesh", info, n). */
export async function hkdfKey(ikm: Uint8Array, info: string, n: number): Promise<Uint8Array> {
  const base = await subtle.importKey("raw", ikm, "HKDF", false, ["deriveBits"]);
  const bits = await subtle.deriveBits(
    {
      name: "HKDF",
      hash: "SHA-256",
      salt: utf8("agent-mesh"),
      info: utf8(info),
    },
    base,
    n * 8,
  );
  return new Uint8Array(bits);
}

export async function aesGcmSeal(key: Uint8Array, nonce: Uint8Array, plaintext: Uint8Array): Promise<Uint8Array> {
  const k = await subtle.importKey("raw", key, { name: "AES-GCM" }, false, ["encrypt"]);
  const ct = await subtle.encrypt({ name: "AES-GCM", iv: nonce }, k, plaintext);
  return new Uint8Array(ct);
}

export async function aesGcmOpen(key: Uint8Array, nonce: Uint8Array, ciphertext: Uint8Array): Promise<Uint8Array> {
  const k = await subtle.importKey("raw", key, { name: "AES-GCM" }, false, ["decrypt"]);
  const pt = await subtle.decrypt({ name: "AES-GCM", iv: nonce }, k, ciphertext);
  return new Uint8Array(pt);
}

export async function randomSeal(key: Uint8Array, plaintext: Uint8Array): Promise<{ nonce: Uint8Array; ct: Uint8Array }> {
  const nonce = new Uint8Array(12);
  globalThis.crypto.getRandomValues(nonce);
  const ct = await aesGcmSeal(key, nonce, plaintext);
  return { nonce, ct };
}
