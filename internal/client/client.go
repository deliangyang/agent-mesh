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

// Client 可直连节点或经中继转发，应用层始终 E2E。
type Client struct {
	Ident  *e2e.Identity
	Relay  string
	Direct string
	HTTP   *http.Client

	peer *e2e.PublicIdentity
	sess *e2e.Session
	base string
}

// New 创建客户端。
func New(ident *e2e.Identity, relay, direct string) *Client {
	return &Client{
		Ident:  ident,
		Relay:  strings.TrimRight(relay, "/"),
		Direct: strings.TrimRight(direct, "/"),
		HTTP:   &http.Client{Timeout: 120 * time.Second},
	}
}

// Nodes 从中继拉取节点目录。
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

// Handshake 与目标节点建立 E2E 会话。
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
	raw, err := json.Marshal(env)
	if err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(raw))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("http %d: %s", resp.StatusCode, b)
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return out, fmt.Errorf("decode envelope: %w", err)
	}
	return out, nil
}

// Call 同步 RPC。
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

// Stream 加密 SSE。
func (c *Client) Stream(ctx context.Context, req protocol.Request) (<-chan protocol.Event, error) {
	if c.sess == nil {
		return nil, fmt.Errorf("handshake required")
	}
	plain, _ := json.Marshal(req)
	env, err := c.sess.Seal(plain)
	if err != nil {
		return nil, err
	}
	env.Kind = "stream"
	raw, _ := json.Marshal(env)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/stream", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpCli := c.HTTP
	if httpCli.Timeout != 0 {
		cp := *httpCli
		cp.Timeout = 0
		httpCli = &cp
	}
	resp, err := httpCli.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("stream http %d: %s", resp.StatusCode, b)
	}
	out := make(chan protocol.Event, 32)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
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
