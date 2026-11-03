import { fromUtf8, utf8 } from "./bytes.ts";
import {
  CONTENT_TYPE_ENVELOPE,
  CONTENT_TYPE_ENVELOPE_STREAM,
  StreamFrameReader,
  contentTypeIsEnvelope,
  decodeEnvelopeBody,
  marshalEnvelope,
  unmarshalEnvelope,
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
  /** In-flight handshakes coalesced per node so parallel catalog/watch calls share one session. */
  private handshakes = new Map<string, Promise<NodeSession>>();
  private reqSeq = 0;

  constructor(ident: Identity, relay = "") {
    this.ident = ident;
    this.relay = relay.replace(/\/$/, "");
  }

  private nextID(): string {
    this.reqSeq += 1;
    return `w${this.reqSeq}-${Date.now()}`;
  }

  private dropSession(nodeID: string): void {
    const cur = this.byNode.get(nodeID);
    if (cur) this.byNode.delete(cur.peer.id);
    this.byNode.delete(nodeID);
  }

  async nodes(): Promise<NodeRow[]> {
    const resp = await fetch(this.relay + "/v1/nodes", { credentials: "same-origin" });
    if (!resp.ok) throw new Error(`nodes http ${resp.status}`);
    return (await resp.json()) as NodeRow[];
  }

  async ensureSession(nodeID: string): Promise<NodeSession> {
    const key = nodeID || "*";
    const cur = this.byNode.get(nodeID);
    if (cur && !sessionExpired(cur.sess)) return cur;
    if (cur) this.dropSession(nodeID);

    const inflight = this.handshakes.get(key);
    if (inflight) return inflight;

    const pending = this.handshake(nodeID).finally(() => {
      if (this.handshakes.get(key) === pending) this.handshakes.delete(key);
    });
    this.handshakes.set(key, pending);
    return pending;
  }

  private async handshake(nodeID: string): Promise<NodeSession> {
    const cached = nodeID ? this.byNode.get(nodeID) : undefined;
    if (cached && !sessionExpired(cached.sess)) return cached;

    const rows = await this.nodes();
    const row = rows.find((n) => n.id === nodeID) || (!nodeID ? rows[0] : undefined);
    if (!row) throw new Error(nodeID ? `node ${nodeID} not found` : "no nodes registered");
    // Another waiter may have finished while we fetched the directory.
    const raced = this.byNode.get(row.id);
    if (raced && !sessionExpired(raced.sess)) return raced;

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
    let lastErr: unknown;
    for (let attempt = 0; attempt < 2; attempt++) {
      const ns = await this.ensureSession(nodeID);
      try {
        const req: RPCRequest = { id: this.nextID(), method, params };
        const env = await seal(ns.sess, utf8(JSON.stringify(req)), "rpc");
        const ack = await this.postEnvelope(ns.base + "/rpc", env);
        const plain = await open(ns.sess, ack);
        const resp = JSON.parse(fromUtf8(plain)) as RPCResponse;
        if (!resp.ok && resp.error) throw new Error(resp.error);
        return resp;
      } catch (err) {
        lastErr = err;
        const msg = err instanceof Error ? err.message : String(err);
        // Stale session after a racing handshake (node keeps only the latest per client).
        if (attempt === 0 && /\bhttp 401\b/.test(msg)) {
          this.dropSession(ns.peer.id);
          continue;
        }
        throw err;
      }
    }
    throw lastErr instanceof Error ? lastErr : new Error(String(lastErr));
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
      // One retry after clearing a stale session (same race as call()).
      if (resp.status === 401) {
        this.dropSession(ns.peer.id);
        const ns2 = await this.ensureSession(nodeID);
        const env2 = await seal(ns2.sess, utf8(JSON.stringify({ ...req, id: this.nextID() })), "stream");
        const raw2 = marshalEnvelope(env2);
        const resp2 = await fetch(ns2.base + "/stream", {
          method: "POST",
          credentials: "same-origin",
          headers: {
            "Content-Type": CONTENT_TYPE_ENVELOPE,
            Accept: CONTENT_TYPE_ENVELOPE_STREAM,
          },
          body: raw2,
          signal,
        });
        if (!resp2.ok) {
          throw new Error(`stream http ${resp2.status}: ${await resp2.text()}`);
        }
        yield* this.readStream(ns2, resp2);
        return;
      }
      throw new Error(`stream http ${resp.status}: ${t}`);
    }
    yield* this.readStream(ns, resp);
  }

  private async *readStream(ns: NodeSession, resp: Response): AsyncGenerator<MeshEvent> {
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

  /**
   * Open an E2E catalog WebSocket for one node. Each WS binary message is a sealed
   * Envelope whose plaintext is a catalog busMsg (sessions/engines/…).
   * Returns a closer; onAuth401 clears the session so the next open re-handshakes.
   */
  async openCatalogWS(
    nodeID: string,
    onPart: (msg: Record<string, unknown>) => void,
  ): Promise<{ close: () => void; peerID: string }> {
    const ns = await this.ensureSession(nodeID);
    const req: RPCRequest = { id: this.nextID(), method: "catalog", stream: true };
    const env = await seal(ns.sess, utf8(JSON.stringify(req)), "stream");
    const raw = marshalEnvelope(env);
    const base = this.relay || (typeof location !== "undefined" ? location.origin : "");
    const proto = base.startsWith("https") || (typeof location !== "undefined" && location.protocol === "https:") ? "wss:" : "ws:";
    let host = "";
    try {
      host = base ? new URL(base).host : location.host;
    } catch {
      host = typeof location !== "undefined" ? location.host : "";
    }
    const url = `${proto}//${host}/v1/to/${ns.peer.id}/e2e/ws`;
    const sock = new WebSocket(url);
    sock.binaryType = "arraybuffer";
    let closed = false;
    const close = () => {
      closed = true;
      try {
        sock.close();
      } catch {
        /* ignore */
      }
    };
    await new Promise<void>((resolve, reject) => {
      const t = window.setTimeout(() => reject(new Error("catalog ws timeout")), 15000);
      sock.onopen = () => {
        window.clearTimeout(t);
        resolve();
      };
      sock.onerror = () => {
        window.clearTimeout(t);
        reject(new Error("catalog ws error"));
      };
    });
    sock.send(raw);
    sock.onmessage = (ev) => {
      if (closed) return;
      void (async () => {
        try {
          const buf =
            ev.data instanceof ArrayBuffer
              ? new Uint8Array(ev.data)
              : ev.data instanceof Blob
                ? new Uint8Array(await ev.data.arrayBuffer())
                : null;
          if (!buf) return;
          const fe = unmarshalEnvelope(buf);
          const plain = await open(ns.sess, fe);
          const msg = JSON.parse(fromUtf8(plain)) as Record<string, unknown>;
          onPart(msg);
        } catch {
          /* decrypt / parse — drop */
        }
      })();
    };
    sock.onclose = () => {
      closed = true;
    };
    return { close, peerID: ns.peer.id };
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
