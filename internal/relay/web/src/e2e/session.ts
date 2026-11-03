import { concatBytes, fromUtf8, utf8 } from "./bytes.ts";
import {
  aesGcmOpen,
  aesGcmSeal,
  generateX25519Pair,
  hkdfKey,
  importX25519Private,
  importX25519Public,
  randomSeal,
  x25519ECDH,
} from "./crypto.ts";
import type { Identity, PublicIdentity } from "./identity.ts";
import type { Envelope } from "./frame.ts";

const VERSION = 1;
const HELLO_LABEL = "mesh/e2e/v1/hello";
const ACK_LABEL = "mesh/e2e/v1/ack";
const C2S_LABEL = "mesh/e2e/v1/c2s";
const S2C_LABEL = "mesh/e2e/v1/s2c";

export type Session = {
  id: string;
  peerID: string;
  localID: string;
  c2s: Uint8Array;
  s2c: Uint8Array;
  sendN: number;
  expiry: number; // unix ms
  clientSend: boolean;
};

export type HandshakeState = {
  ephPrivate: Uint8Array;
  ephPublic: Uint8Array;
};

function counterNonce(n: number): Uint8Array {
  const nonce = new Uint8Array(12);
  const view = new DataView(nonce.buffer);
  // Match Go binary.BigEndian.PutUint64(nonce[4:], n).
  view.setUint32(4, Math.floor(n / 0x100000000) >>> 0);
  view.setUint32(8, n >>> 0);
  return nonce;
}

function sendKey(s: Session): Uint8Array {
  return s.clientSend ? s.c2s : s.s2c;
}

function recvKey(s: Session): Uint8Array {
  return s.clientSend ? s.s2c : s.c2s;
}

export async function seal(s: Session, plaintext: Uint8Array, kind: "rpc" | "stream" = "rpc"): Promise<Envelope> {
  s.sendN += 1;
  const nonce = counterNonce(s.sendN);
  const ct = await aesGcmSeal(sendKey(s), nonce, plaintext);
  return {
    version: VERSION,
    from: s.localID,
    to: s.peerID,
    kind,
    nonce,
    ciphertext: ct,
  };
}

export async function open(s: Session, env: Envelope): Promise<Uint8Array> {
  if (env.version !== VERSION) throw new Error(`unsupported envelope version ${env.version}`);
  return aesGcmOpen(recvKey(s), env.nonce, env.ciphertext);
}

export function sessionExpired(s: Session): boolean {
  return Date.now() > s.expiry;
}

export async function beginHandshake(
  local: Identity,
  peer: PublicIdentity,
): Promise<{ env: Envelope; state: HandshakeState }> {
  const eph = await generateX25519Pair();
  const peerPub = await importX25519Public(peer.x25519Pub);
  const ss = await x25519ECDH(eph.privateKey, peerPub);
  const helloKey = await hkdfKey(ss, HELLO_LABEL, 32);
  const rnd = new Uint8Array(16);
  globalThis.crypto.getRandomValues(rnd);
  const body = utf8(
    JSON.stringify({
      client_id: local.id,
      unix: Math.floor(Date.now() / 1000),
      random: b64(rnd),
    }),
  );
  const { nonce, ct } = await randomSeal(helloKey, body);
  return {
    env: {
      version: VERSION,
      from: local.id,
      to: peer.id,
      kind: "handshake",
      ephPub: eph.publicRaw,
      nonce,
      ciphertext: ct,
    },
    state: { ephPrivate: eph.privateRaw, ephPublic: eph.publicRaw },
  };
}

export async function finishHandshake(
  local: Identity,
  peer: PublicIdentity,
  state: HandshakeState,
  env: Envelope,
): Promise<Session> {
  if (env.kind !== "handshake") throw new Error("not a handshake envelope");
  if (!env.ephPub || env.ephPub.length === 0) throw new Error("missing node eph");
  const clientEph = await importX25519Private(state.ephPrivate);
  const peerStatic = await importX25519Public(peer.x25519Pub);
  const ss = await x25519ECDH(clientEph, peerStatic);
  const nodeEph = await importX25519Public(env.ephPub);
  const ss2 = await x25519ECDH(clientEph, nodeEph);
  const ikm = concatBytes(ss, ss2);
  const ackKey = await hkdfKey(ikm, ACK_LABEL, 32);
  const plain = await aesGcmOpen(ackKey, env.nonce, env.ciphertext);
  const ack = JSON.parse(fromUtf8(plain)) as { node_id: string; session_id: string; expiry: number };
  const c2s = await hkdfKey(ikm, C2S_LABEL, 32);
  const s2c = await hkdfKey(ikm, S2C_LABEL, 32);
  return {
    id: ack.session_id,
    peerID: peer.id,
    localID: local.id,
    c2s,
    s2c,
    sendN: 0,
    expiry: (ack.expiry || 0) * 1000,
    clientSend: true,
  };
}

/** Deterministic helpers for tests (fixed eph / nonce). */
export async function beginHandshakeFixed(
  local: Identity,
  peer: PublicIdentity,
  ephPrivate: Uint8Array,
  helloBody: Uint8Array,
  helloNonce: Uint8Array,
): Promise<{ env: Envelope; ss: Uint8Array }> {
  const ephKey = await importX25519Private(ephPrivate);
  const peerPub = await importX25519Public(peer.x25519Pub);
  const ss = await x25519ECDH(ephKey, peerPub);
  const helloKey = await hkdfKey(ss, HELLO_LABEL, 32);
  const ct = await aesGcmSeal(helloKey, helloNonce, helloBody);
  const ephPub = await x25519PublicFromPriv(ephPrivate);
  return {
    ss,
    env: {
      version: VERSION,
      from: local.id,
      to: peer.id,
      kind: "handshake",
      ephPub,
      nonce: helloNonce,
      ciphertext: ct,
    },
  };
}

export async function finishHandshakeFixed(
  local: Identity,
  peer: PublicIdentity,
  ephPrivate: Uint8Array,
  env: Envelope,
): Promise<{ session: Session; ss: Uint8Array; ss2: Uint8Array; c2s: Uint8Array; s2c: Uint8Array; ackKey: Uint8Array }> {
  const clientEph = await importX25519Private(ephPrivate);
  const peerStatic = await importX25519Public(peer.x25519Pub);
  const ss = await x25519ECDH(clientEph, peerStatic);
  const nodeEph = await importX25519Public(env.ephPub!);
  const ss2 = await x25519ECDH(clientEph, nodeEph);
  const ikm = concatBytes(ss, ss2);
  const ackKey = await hkdfKey(ikm, ACK_LABEL, 32);
  const plain = await aesGcmOpen(ackKey, env.nonce, env.ciphertext);
  const ack = JSON.parse(fromUtf8(plain)) as { session_id: string; expiry: number };
  const c2s = await hkdfKey(ikm, C2S_LABEL, 32);
  const s2c = await hkdfKey(ikm, S2C_LABEL, 32);
  return {
    ss,
    ss2,
    c2s,
    s2c,
    ackKey,
    session: {
      id: ack.session_id,
      peerID: peer.id,
      localID: local.id,
      c2s,
      s2c,
      sendN: 0,
      expiry: ack.expiry * 1000,
      clientSend: true,
    },
  };
}

async function x25519PublicFromPriv(privRaw: Uint8Array): Promise<Uint8Array> {
  const key = await importX25519Private(privRaw);
  const base = new Uint8Array(32);
  base[0] = 9;
  return x25519ECDH(key, await importX25519Public(base));
}

function b64(u8: Uint8Array): string {
  let s = "";
  for (let i = 0; i < u8.length; i++) s += String.fromCharCode(u8[i]!);
  return btoa(s);
}

export { counterNonce, HELLO_LABEL, ACK_LABEL, C2S_LABEL, S2C_LABEL, hkdfKey };
