import { fromUtf8, utf8 } from "./bytes.ts";
import {
  CONTENT_TYPE_ENVELOPE,
  CONTENT_TYPE_ENVELOPE_STREAM,
  StreamFrameReader,
  contentTypeIsEnvelope,
  decodeEnvelopeBody,
  marshalEnvelope,
  type Envelope,
} from "./frame.ts";
import { parseNodePublic, type Identity, type PublicIdentity } from "./identity.ts";
import { beginHandshake, finishHandshake, open, seal, sessionExpired, type HandshakeState, type Session } from "./session.ts";

export type NodeRow = {
  id: string;
  name?: string;
  listen?: string;
  os?: string;
  arch?: string;
  engines?: string[];
  sign_pub?: unknown;
  x25519_pub?: unknown;
  last_seen?: number;
};

export type RPCRequest = {
  id: string;
  method: string;
  stream?: boolean;
  params?: unknown;
};

export type RPCResponse = {
  id: string;
  ok: boolean;
  error?: string;
  body?: unknown;
};

export type MeshEvent = {
  type: string;
  role?: string;
  tool?: string;
  engine?: string;
  session?: string;
  text?: string;
  preview?: string;
  unix?: number;
  done?: boolean;
};

type NodeSession = {
  peer: PublicIdentity;
  sess: Session;
  base: string;
};

export class E2EClient {
  ident: Identity;
  relay = "";
  private byNode = new Map<string, NodeSession>();
  private reqSeq = 0;

  constructor(ident: Identity, relay = "") {
    this.ident = ident;
    this.relay = relay.replace(/\/$/, "");
  }

  private nextID(): string {
    this.reqSeq += 1;
    return `w${this.reqSeq}-${Date.now()}`;
  }

  async nodes(): Promise<NodeRow[]> {
    const resp = await fetch(this.relay + "/v1/nodes", { credentials: "same-origin" });
    if (!resp.ok) throw new Error(`nodes http ${resp.status}`);
    return (await resp.json()) as NodeRow[];
  }

  async ensureSession(nodeID: string): Promise<NodeSession> {
    const cur = this.byNode.get(nodeID);
    if (cur && !sessionExpired(cur.sess)) return cur;
    const rows = await this.nodes();
    const row = rows.find((n) => n.id === nodeID) || (!nodeID ? rows[0] : undefined);
    if (!row) throw new Error(nodeID ? `node ${nodeID} not found` : "no nodes registered");
    const peer = parseNodePublic(row);
    const base = `${this.relay}/v1/to/${peer.id}`;
    const { env, state } = await beginHandshake(this.ident, peer);
    const ack = await this.postEnvelope(base + "/handshake", env);
    const sess = await finishHandshake(this.ident, peer, state, ack);
    const held: NodeSession = { peer, sess, base };
    this.byNode.set(peer.id, held);
    return held;
  }

  async call(nodeID: string, method: string, params?: unknown): Promise<RPCResponse> {
    const ns = await this.ensureSession(nodeID);
    const req: RPCRequest = { id: this.nextID(), method, params };
    const env = await seal(ns.sess, utf8(JSON.stringify(req)), "rpc");
    const ack = await this.postEnvelope(ns.base + "/rpc", env);
    const plain = await open(ns.sess, ack);
    const resp = JSON.parse(fromUtf8(plain)) as RPCResponse;
    if (!resp.ok && resp.error) throw new Error(resp.error);
    return resp;
  }

  async *stream(nodeID: string, method: string, params?: unknown, signal?: AbortSignal): AsyncGenerator<MeshEvent> {
    const ns = await this.ensureSession(nodeID);
    const req: RPCRequest = { id: this.nextID(), method, stream: true, params };
    const env = await seal(ns.sess, utf8(JSON.stringify(req)), "stream");
    const raw = marshalEnvelope(env);
    const resp = await fetch(ns.base + "/stream", {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": CONTENT_TYPE_ENVELOPE,
        Accept: CONTENT_TYPE_ENVELOPE_STREAM,
      },
      body: raw,
      signal,
    });
    if (!resp.ok) {
      const t = await resp.text();
      throw new Error(`stream http ${resp.status}: ${t}`);
    }
    if (!resp.body) throw new Error("empty stream body");
    const ct = resp.headers.get("Content-Type") || "";
    const bin = contentTypeIsEnvelope(ct) || ct.toLowerCase().includes("envelope-stream");
    if (bin) {
      const reader = resp.body.getReader();
      const frames = new StreamFrameReader();
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        for (const fe of frames.push(value)) {
          const plain = await open(ns.sess, fe);
          const ev = JSON.parse(fromUtf8(plain)) as MeshEvent;
          yield ev;
          if (ev.done) return;
        }
      }
      return;
    }
    // SSE fallback: data: {json envelope}
    const text = await resp.text();
    for (const line of text.split("\n")) {
      if (!line.startsWith("data: ")) continue;
      const fe = decodeEnvelopeBody("application/json", utf8(line.slice(6)));
      const plain = await open(ns.sess, fe);
      const ev = JSON.parse(fromUtf8(plain)) as MeshEvent;
      yield ev;
      if (ev.done) return;
    }
  }

  private async postEnvelope(url: string, env: Envelope): Promise<Envelope> {
    const raw = marshalEnvelope(env);
    const resp = await fetch(url, {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": CONTENT_TYPE_ENVELOPE,
        Accept: CONTENT_TYPE_ENVELOPE,
      },
      body: raw,
    });
    const buf = new Uint8Array(await resp.arrayBuffer());
    if (resp.status >= 400) throw new Error(`http ${resp.status}: ${fromUtf8(buf)}`);
    return decodeEnvelopeBody(resp.headers.get("Content-Type"), buf);
  }
}

export type { HandshakeState, Session };
