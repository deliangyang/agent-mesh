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
  gen: number;
};

export class E2EClient {
  ident: Identity;
  relay = "";
  private byNode = new Map<string, NodeSession>();
  /** In-flight handshakes coalesced per node so parallel catalog/watch calls share one session. */
  private handshakes = new Map<string, Promise<NodeSession>>();
  /** Serialize sealed outbound work per node so seal/handshake cannot interleave badly. */
  private tails = new Map<string, Promise<unknown>>();
  private reqSeq = 0;
  private genSeq = 0;
  /** Optional hook: sync.ts closes/reopens catalog sockets when a session is dropped. */
  onSessionDrop: ((nodeID: string) => void) | null = null;

  constructor(ident: Identity, relay = "") {
    this.ident = ident;
    this.relay = relay.replace(/\/$/, "");
  }

  private nextID(): string {
    this.reqSeq += 1;
    return `w${this.reqSeq}-${Date.now()}`;
  }

  private async withNode<T>(nodeID: string, fn: () => Promise<T>): Promise<T> {
    const key = nodeID || "*";
    const prev = this.tails.get(key) || Promise.resolve();
    let release!: () => void;
    const gate = new Promise<void>((r) => {
      release = r;
    });
    this.tails.set(
      key,
      prev.then(() => gate).catch(() => gate),
    );
    await prev.catch(() => {});
    try {
      return await fn();
    } finally {
      release();
    }
  }

  private dropSession(nodeID: string): void {
    const cur = this.byNode.get(nodeID);
    if (cur) this.byNode.delete(cur.peer.id);
    this.byNode.delete(nodeID);
    try {
      this.onSessionDrop?.(cur?.peer.id || nodeID);
    } catch {
      /* ignore */
    }
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
    this.genSeq += 1;
    const held: NodeSession = { peer, sess, base, gen: this.genSeq };
    this.byNode.set(peer.id, held);
    return held;
  }

  async call(nodeID: string, method: string, params?: unknown): Promise<RPCResponse> {
    return this.withNode(nodeID, async () => {
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
          if (attempt === 0 && /\bhttp 401\b/.test(msg)) {
            this.dropSession(ns.peer.id);
            continue;
          }
          throw err;
        }
      }
      throw lastErr instanceof Error ? lastErr : new Error(String(lastErr));
    });
  }

  async *stream(nodeID: string, method: string, params?: unknown, signal?: AbortSignal): AsyncGenerator<MeshEvent> {
    const started = await this.withNode(nodeID, async () => {
      const openOnce = async (): Promise<{ ns: NodeSession; resp: Response }> => {
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
        return { ns, resp };
      };
      let { ns, resp } = await openOnce();
      if (!resp.ok) {
        const t = await resp.text();
        if (resp.status === 401) {
          this.dropSession(ns.peer.id);
          ({ ns, resp } = await openOnce());
          if (!resp.ok) {
            throw new Error(`stream http ${resp.status}: ${await resp.text()}`);
          }
          return { ns, resp };
        }
        throw new Error(`stream http ${resp.status}: ${t}`);
      }
      return { ns, resp };
    });
    yield* this.readStream(started.ns, started.resp);
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
    return this.withNode(nodeID, async () => {
      const ns = await this.ensureSession(nodeID);
      const heldGen = ns.gen;
      const req: RPCRequest = { id: this.nextID(), method: "catalog", stream: true };
      const env = await seal(ns.sess, utf8(JSON.stringify(req)), "stream");
      const raw = marshalEnvelope(env);
      const base = this.relay || (typeof location !== "undefined" ? location.origin : "");
      const proto =
        base.startsWith("https") || (typeof location !== "undefined" && location.protocol === "https:")
          ? "wss:"
          : "ws:";
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
      // Fail fast if the node rejects the sealed subscribe (401 decrypt/no session).
      await new Promise<void>((resolve, reject) => {
        const t = window.setTimeout(() => reject(new Error("catalog ws no data")), 12000);
        sock.onmessage = (ev) => {
          window.clearTimeout(t);
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
              resolve();
              sock.onmessage = (later) => {
                if (closed) return;
                void (async () => {
                  try {
                    const cur = this.byNode.get(ns.peer.id);
                    if (!cur || cur.gen !== heldGen) return;
                    const b =
                      later.data instanceof ArrayBuffer
                        ? new Uint8Array(later.data)
                        : later.data instanceof Blob
                          ? new Uint8Array(await later.data.arrayBuffer())
                          : null;
                    if (!b) return;
                    const frame = unmarshalEnvelope(b);
                    const pt = await open(ns.sess, frame);
                    onPart(JSON.parse(fromUtf8(pt)) as Record<string, unknown>);
                  } catch {
                    /* drop */
                  }
                })();
              };
            } catch (err) {
              reject(err instanceof Error ? err : new Error(String(err)));
            }
          })();
        };
        sock.onclose = () => {
          window.clearTimeout(t);
          closed = true;
          reject(new Error("catalog ws closed before first frame"));
        };
        sock.send(raw);
      });
      sock.onclose = () => {
        closed = true;
      };
      return { close, peerID: ns.peer.id };
    });
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
