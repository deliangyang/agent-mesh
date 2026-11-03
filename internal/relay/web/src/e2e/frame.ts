export const CONTENT_TYPE_ENVELOPE = "application/vnd.agent-mesh.envelope";
export const CONTENT_TYPE_ENVELOPE_STREAM = "application/vnd.agent-mesh.envelope-stream";

const MAGIC0 = 0x41; // A
const MAGIC1 = 0x4d; // M
const FLAG_EPH = 1;

export type Envelope = {
  version: number;
  from: string;
  to: string;
  kind: "handshake" | "rpc" | "stream" | string;
  ephPub?: Uint8Array;
  nonce: Uint8Array;
  ciphertext: Uint8Array;
};

function kindByte(kind: string): number {
  switch (kind) {
    case "handshake":
      return 1;
    case "rpc":
      return 2;
    case "stream":
      return 3;
    default:
      return 0;
  }
}

function kindString(k: number): string {
  switch (k) {
    case 1:
      return "handshake";
    case 2:
      return "rpc";
    case 3:
      return "stream";
    default:
      return "";
  }
}

export function marshalEnvelope(e: Envelope): Uint8Array {
  const from = new TextEncoder().encode(e.from);
  const to = new TextEncoder().encode(e.to);
  const eph = e.ephPub || new Uint8Array(0);
  const flags = eph.length > 0 ? FLAG_EPH : 0;
  const ver = e.version || 1;
  let n = 2 + 1 + 1 + 1 + 2 + from.length + 2 + to.length + 1 + e.nonce.length + 4 + e.ciphertext.length;
  if (flags & FLAG_EPH) n += 1 + eph.length;
  const buf = new Uint8Array(n);
  let off = 0;
  buf[off++] = MAGIC0;
  buf[off++] = MAGIC1;
  buf[off++] = ver;
  buf[off++] = kindByte(e.kind);
  buf[off++] = flags;
  buf[off++] = (from.length >> 8) & 0xff;
  buf[off++] = from.length & 0xff;
  buf.set(from, off);
  off += from.length;
  buf[off++] = (to.length >> 8) & 0xff;
  buf[off++] = to.length & 0xff;
  buf.set(to, off);
  off += to.length;
  if (flags & FLAG_EPH) {
    buf[off++] = eph.length;
    buf.set(eph, off);
    off += eph.length;
  }
  buf[off++] = e.nonce.length;
  buf.set(e.nonce, off);
  off += e.nonce.length;
  const cLen = e.ciphertext.length;
  buf[off++] = (cLen >>> 24) & 0xff;
  buf[off++] = (cLen >>> 16) & 0xff;
  buf[off++] = (cLen >>> 8) & 0xff;
  buf[off++] = cLen & 0xff;
  buf.set(e.ciphertext, off);
  return buf;
}

export function unmarshalEnvelope(data: Uint8Array): Envelope {
  if (data.length < 2 + 1 + 1 + 1 + 2 + 2 + 1 + 4) throw new Error("envelope too short");
  if (data[0] !== MAGIC0 || data[1] !== MAGIC1) throw new Error("bad envelope magic");
  let off = 2;
  const version = data[off++]!;
  const kind = kindString(data[off++]!);
  const flags = data[off++]!;
  const fromLen = (data[off]! << 8) | data[off + 1]!;
  off += 2;
  const from = new TextDecoder().decode(data.subarray(off, off + fromLen));
  off += fromLen;
  const toLen = (data[off]! << 8) | data[off + 1]!;
  off += 2;
  const to = new TextDecoder().decode(data.subarray(off, off + toLen));
  off += toLen;
  let ephPub: Uint8Array | undefined;
  if (flags & FLAG_EPH) {
    const n = data[off++]!;
    ephPub = data.slice(off, off + n);
    off += n;
  }
  const nLen = data[off++]!;
  const nonce = data.slice(off, off + nLen);
  off += nLen;
  const cLen = ((data[off]! << 24) | (data[off + 1]! << 16) | (data[off + 2]! << 8) | data[off + 3]!) >>> 0;
  off += 4;
  if (off + cLen !== data.length) throw new Error(off + cLen > data.length ? "truncated ciphertext" : "trailing envelope bytes");
  const ciphertext = data.slice(off, off + cLen);
  return { version, from, to, kind, ephPub, nonce, ciphertext };
}

export function isBinaryPayload(raw: Uint8Array): boolean {
  return raw.length >= 2 && raw[0] === MAGIC0 && raw[1] === MAGIC1;
}

export function contentTypeIsEnvelope(ct: string | null): boolean {
  return (ct || "").toLowerCase().includes("vnd.agent-mesh.envelope");
}

/** JSON wire form (Go json.Marshal of Envelope with []byte as base64). */
export function envelopeToJSON(e: Envelope): string {
  return JSON.stringify({
    v: e.version,
    from: e.from,
    to: e.to,
    kind: e.kind,
    eph: e.ephPub && e.ephPub.length ? b64(e.ephPub) : undefined,
    nonce: b64(e.nonce),
    ct: b64(e.ciphertext),
  });
}

function b64(u8: Uint8Array): string {
  let s = "";
  for (let i = 0; i < u8.length; i++) s += String.fromCharCode(u8[i]!);
  return btoa(s);
}

function unb64(s: string): Uint8Array {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

export function envelopeFromJSON(raw: string): Envelope {
  const o = JSON.parse(raw) as {
    v?: number;
    from: string;
    to: string;
    kind: string;
    eph?: string;
    nonce: string;
    ct: string;
  };
  return {
    version: o.v || 1,
    from: o.from,
    to: o.to,
    kind: o.kind,
    ephPub: o.eph ? unb64(o.eph) : undefined,
    nonce: unb64(o.nonce),
    ciphertext: unb64(o.ct),
  };
}

export function decodeEnvelopeBody(ct: string | null, body: Uint8Array): Envelope {
  if (contentTypeIsEnvelope(ct) || isBinaryPayload(body)) return unmarshalEnvelope(body);
  return envelopeFromJSON(new TextDecoder().decode(body));
}

export function marshalStreamFrame(env: Envelope): Uint8Array {
  const raw = marshalEnvelope(env);
  const out = new Uint8Array(4 + raw.length);
  const n = raw.length;
  out[0] = (n >>> 24) & 0xff;
  out[1] = (n >>> 16) & 0xff;
  out[2] = (n >>> 8) & 0xff;
  out[3] = n & 0xff;
  out.set(raw, 4);
  return out;
}

export class StreamFrameReader {
  private buf = new Uint8Array(0);

  push(chunk: Uint8Array): Envelope[] {
    const merged = new Uint8Array(this.buf.length + chunk.length);
    merged.set(this.buf);
    merged.set(chunk, this.buf.length);
    this.buf = merged;
    const out: Envelope[] = [];
    while (this.buf.length >= 4) {
      const n = ((this.buf[0]! << 24) | (this.buf[1]! << 16) | (this.buf[2]! << 8) | this.buf[3]!) >>> 0;
      if (n === 0 || n > 16 << 20) throw new Error(`bad frame size ${n}`);
      if (this.buf.length < 4 + n) break;
      const raw = this.buf.slice(4, 4 + n);
      this.buf = this.buf.slice(4 + n);
      out.push(unmarshalEnvelope(raw));
    }
    return out;
  }
}
