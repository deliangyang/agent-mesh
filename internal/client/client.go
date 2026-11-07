package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"agent-mesh/internal/e2e"
	"agent-mesh/internal/protocol"
)

// Client can talk to a node directly or through the relay; application encryption is always E2E.
type Client struct {
	Ident  *e2e.Identity
	Relay  string
	Direct string
	HTTP   *http.Client

	peer *e2e.PublicIdentity
	sess *e2e.Session
	base string
}

// New creates a client.
func New(ident *e2e.Identity, relay, direct string) *Client {
	return &Client{
		Ident:  ident,
		Relay:  strings.TrimRight(relay, "/"),
		Direct: strings.TrimRight(direct, "/"),
		HTTP:   &http.Client{Timeout: 120 * time.Second},
	}
}

// Nodes loads the node directory from the relay.
func (c *Client) Nodes(ctx context.Context) ([]protocol.NodeInfo, error) {
	if c.Relay == "" {
		return nil, fmt.Errorf("relay url required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Relay+"/v1/nodes", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("nodes http %d: %s", resp.StatusCode, b)
	}
	var rows []protocol.NodeInfo
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (c *Client) target(nodeID string) (base string, peer e2e.PublicIdentity, err error) {
	if c.Direct != "" {
		info, err := c.fetchInfo(context.Background(), c.Direct+"/v1/info")
		if err != nil {
			return "", peer, err
		}
		return c.Direct + "/v1", e2e.PublicIdentity{ID: info.ID, SignPub: info.SignPub, X25519Pub: info.X25519}, nil
	}
	if c.Relay == "" {
		return "", peer, fmt.Errorf("need --relay or --direct")
	}
	rows, err := c.Nodes(context.Background())
	if err != nil {
		return "", peer, err
	}
	for _, n := range rows {
		if nodeID == "" || n.ID == nodeID {
			return c.Relay + "/v1/to/" + n.ID, e2e.PublicIdentity{ID: n.ID, SignPub: n.SignPub, X25519Pub: n.X25519}, nil
		}
	}
	return "", peer, fmt.Errorf("node %s not found", nodeID)
}

func (c *Client) fetchInfo(ctx context.Context, u string) (protocol.NodeInfo, error) {
	var info protocol.NodeInfo
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return info, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return info, err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return info, err
	}
	return info, nil
}

// Info returns public node info (including OS/Arch when the node advertises them).
// Prefers --direct /v1/info; otherwise looks up the node in the relay directory.
func (c *Client) Info(ctx context.Context, nodeID string) (protocol.NodeInfo, error) {
	if c.Direct != "" {
		return c.fetchInfo(ctx, c.Direct+"/v1/info")
	}
	rows, err := c.Nodes(ctx)
	if err != nil {
		return protocol.NodeInfo{}, err
	}
	for _, n := range rows {
		if nodeID == "" || n.ID == nodeID {
			return n, nil
		}
	}
	if nodeID == "" {
		return protocol.NodeInfo{}, fmt.Errorf("no nodes registered")
	}
	return protocol.NodeInfo{}, fmt.Errorf("node %s not found", nodeID)
}

// Handshake opens an E2E session with the target node.
func (c *Client) Handshake(ctx context.Context, nodeID string) error {
	base, peer, err := c.target(nodeID)
	if err != nil {
		return err
	}
	hello, eph, err := e2e.BeginHandshake(c.Ident, peer)
	if err != nil {
		return err
	}
	ack, err := c.postEnvelope(ctx, base+"/handshake", hello)
	if err != nil {
		return err
	}
	sess, err := e2e.FinishHandshake(c.Ident, peer, eph, ack)
	if err != nil {
		return err
	}
	c.peer = &peer
	c.sess = sess
	c.base = base
	return nil
}

func (c *Client) postEnvelope(ctx context.Context, u string, env e2e.Envelope) (e2e.Envelope, error) {
	var out e2e.Envelope
	raw, ct, err := e2e.EncodeEnvelope(env, true)
	if err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(raw))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Accept", e2e.ContentTypeEnvelope)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("http %d: %s", resp.StatusCode, b)
	}
	if e2e.ContentTypeIsEnvelope(resp.Header.Get("Content-Type")) || e2e.IsBinaryPayload(b) {
		if err := out.UnmarshalBinary(b); err != nil {
			return out, fmt.Errorf("decode envelope: %w", err)
		}
		return out, nil
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return out, fmt.Errorf("decode envelope: %w", err)
	}
	return out, nil
}

// Call is a synchronous RPC.
func (c *Client) Call(ctx context.Context, req protocol.Request) (protocol.Response, error) {
	var resp protocol.Response
	if c.sess == nil {
		return resp, fmt.Errorf("handshake required")
	}
	plain, _ := json.Marshal(req)
	env, err := c.sess.Seal(plain)
	if err != nil {
		return resp, err
	}
	env.Kind = "rpc"
	ack, err := c.postEnvelope(ctx, c.base+"/rpc", env)
	if err != nil {
		return resp, err
	}
	body, err := c.sess.Open(ack)
	if err != nil {
		return resp, fmt.Errorf("decrypt: %w", err)
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return resp, err
	}
	if !resp.OK && resp.Error != "" {
		return resp, fmt.Errorf("%s", resp.Error)
	}
	return resp, nil
}

// Stream is encrypted streaming (binary frames by default, SSE fallback).
func (c *Client) Stream(ctx context.Context, req protocol.Request) (<-chan protocol.Event, error) {
	ctx, cancel := context.WithCancel(ctx)
	raw, err := c.StreamJSON(ctx, req)
	if err != nil {
		cancel()
		return nil, err
	}
	out := make(chan protocol.Event, 32)
	go func() {
		defer close(out)
		defer cancel()
		for plain := range raw {
			var ev protocol.Event
			if err := json.Unmarshal(plain, &ev); err != nil {
				continue
			}
			out <- ev
			if ev.Done {
				return
			}
		}
	}()
	return out, nil
}

// StreamJSON is encrypted streaming that yields each decrypted JSON payload.
func (c *Client) StreamJSON(ctx context.Context, req protocol.Request) (<-chan json.RawMessage, error) {
	resp, binStream, err := c.openStream(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make(chan json.RawMessage, 32)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		if binStream {
			c.readBinaryJSON(resp.Body, out)
			return
		}
		c.readSSEJSON(resp.Body, out)
	}()
	return out, nil
}

func (c *Client) openStream(ctx context.Context, req protocol.Request) (*http.Response, bool, error) {
	if c.sess == nil {
		return nil, false, fmt.Errorf("handshake required")
	}
	plain, _ := json.Marshal(req)
	env, err := c.sess.Seal(plain)
	if err != nil {
		return nil, false, err
	}
	env.Kind = "stream"
	raw, ct, err := e2e.EncodeEnvelope(env, true)
	if err != nil {
		return nil, false, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/stream", bytes.NewReader(raw))
	if err != nil {
		return nil, false, err
	}
	httpReq.Header.Set("Content-Type", ct)
	httpReq.Header.Set("Accept", e2e.ContentTypeEnvelopeStream)
	httpCli := c.HTTP
	if httpCli.Timeout != 0 {
		cp := *httpCli
		cp.Timeout = 0
		httpCli = &cp
	}
	resp, err := httpCli.Do(httpReq)
	if err != nil {
		return nil, false, err
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, false, fmt.Errorf("stream http %d: %s", resp.StatusCode, b)
	}
	binStream := e2e.ContentTypeIsEnvelope(resp.Header.Get("Content-Type")) ||
		strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "envelope-stream")
	return resp, binStream, nil
}

func (c *Client) readBinaryJSON(r io.Reader, out chan<- json.RawMessage) {
	for {
		env, err := e2e.ReadStreamFrame(r, true)
		if err != nil {
			return
		}
		plain, err := c.sess.Open(env)
		if err != nil {
			continue
		}
		out <- append(json.RawMessage(nil), plain...)
	}
}

func (c *Client) readSSEJSON(r io.Reader, out chan<- json.RawMessage) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var env e2e.Envelope
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &env); err != nil {
			continue
		}
		plain, err := c.sess.Open(env)
		if err != nil {
			continue
		}
		out <- append(json.RawMessage(nil), plain...)
	}
}
