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
import { listNodePeers, peerByID, removeNodePeer, saveNodePeers } from "./peers.ts";
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
    const rows = (await resp.json()) as NodeRow[];
    saveNodePeers(rows);
    return rows;
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

  private async handshake(nodeID: string, refreshed = false): Promise<NodeSession> {
    const cached = nodeID ? this.byNode.get(nodeID) : undefined;
    if (cached && !sessionExpired(cached.sess)) return cached;

    // Prefer plaintext pubs cached from /v1/nodes (settings Nodes panel); refresh if missing.
    let row = nodeID ? peerByID(nodeID) : listNodePeers()[0];
    if (!row || row.sign_pub == null || row.x25519_pub == null || refreshed) {
      const rows = await this.nodes();
      row = rows.find((n) => n.id === nodeID) || (!nodeID ? rows[0] : undefined);
    }
    if (!row) throw new Error(nodeID ? `node ${nodeID} not found` : "no nodes registered");
    // Another waiter may have finished while we fetched the directory.
    const raced = this.byNode.get(row.id);
    if (raced && !sessionExpired(raced.sess)) return raced;

    try {
      const peer = parseNodePublic(row);
      const base = `${this.relay}/v1/to/${peer.id}`;
      const { env, state } = await beginHandshake(this.ident, peer);
      const ack = await this.postEnvelope(base + "/handshake", env);
      const sess = await finishHandshake(this.ident, peer, state, ack);
      this.genSeq += 1;
      const held: NodeSession = { peer, sess, base, gen: this.genSeq };
      this.byNode.set(peer.id, held);
      return held;
    } catch (err) {
      // Stale pubs (node rekeyed) — drop cache and retry once from /v1/nodes.
      if (!refreshed) {
        removeNodePeer(row.id);
        return this.handshake(nodeID, true);
      }
      throw err;
    }
  }

  async call(nodeID: string, method: string, params?: unknown, signal?: AbortSignal): Promise<RPCResponse> {
    return this.withNode(nodeID, async () => {
      let lastErr: unknown;
      for (let attempt = 0; attempt < 2; attempt++) {
        if (signal?.aborted) throw new DOMException("Aborted", "AbortError");
        const ns = await this.ensureSession(nodeID);
        try {
          const req: RPCRequest = { id: this.nextID(), method, params };
          const env = await seal(ns.sess, utf8(JSON.stringify(req)), "rpc");
          const ack = await this.postEnvelope(ns.base + "/rpc", env, signal);
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
   * Only handshake/seal hold the per-node lock so history/send are not blocked.
   */
  async openCatalogWS(
    nodeID: string,
    onPart: (msg: Record<string, unknown>) => void,
    onClose?: () => void,
  ): Promise<{ close: () => void; peerID: string }> {
    const primed = await this.withNode(nodeID, async () => {
      const ns = await this.ensureSession(nodeID);
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
      return { ns, heldGen: ns.gen, raw, url };
    });

    const { ns, heldGen, raw, url } = primed;
    const sock = new WebSocket(url);
    sock.binaryType = "arraybuffer";
    let closed = false;
    let intentional = false;
    const close = () => {
      intentional = true;
      closed = true;
      try {
        sock.close();
      } catch {
        /* ignore */
      }
    };
    const notifyClose = () => {
      if (intentional) return;
      try {
        onClose?.();
      } catch {
        /* ignore */
      }
    };
    const failOpen = (err: unknown): never => {
      close();
      throw err instanceof Error ? err : new Error(String(err));
    };

    try {
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
      // Catalog streams are increments-only: a quiet node may send nothing for a long
      // time. Do not wait for a first frame (that used to drop the E2E session and
      // 401 every parallel engines/models call). Subscribe and listen for diffs.
      sock.onmessage = (ev) => {
        if (closed) return;
        void (async () => {
          try {
            const cur = this.byNode.get(ns.peer.id);
            if (!cur || cur.gen !== heldGen) return;
            const buf =
              ev.data instanceof ArrayBuffer
                ? new Uint8Array(ev.data)
                : ev.data instanceof Blob
                  ? new Uint8Array(await ev.data.arrayBuffer())
                  : null;
            if (!buf) return;
            const frame = unmarshalEnvelope(buf);
            const plain = await open(ns.sess, frame);
            onPart(JSON.parse(fromUtf8(plain)) as Record<string, unknown>);
          } catch {
            /* drop */
          }
        })();
      };
      sock.onclose = () => {
        closed = true;
        notifyClose();
      };
      sock.send(raw);
    } catch (err) {
      failOpen(err);
    }
    return { close, peerID: ns.peer.id };
  }

  private async postEnvelope(url: string, env: Envelope, signal?: AbortSignal): Promise<Envelope> {
    const raw = marshalEnvelope(env);
    const resp = await fetch(url, {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": CONTENT_TYPE_ENVELOPE,
        Accept: CONTENT_TYPE_ENVELOPE,
      },
      body: raw,
      signal,
    });
    const buf = new Uint8Array(await resp.arrayBuffer());
    if (resp.status >= 400) throw new Error(`http ${resp.status}: ${fromUtf8(buf)}`);
    return decodeEnvelopeBody(resp.headers.get("Content-Type"), buf);
  }
}

export type { HandshakeState, Session };
