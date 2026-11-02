package workbuddy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
)

type acpClient struct {
	gw             gateway
	connectionID   string
	sessionToken   string
	nextID         atomic.Int64
}

func newACP(ctx context.Context, gw gateway) (*acpClient, error) {
	c := &acpClient{gw: gw}
	raw, err := c.post(ctx, "/api/v1/acp/connect", map[string]any{}, false)
	if err != nil {
		return nil, fmt.Errorf("acp connect: %w", err)
	}
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return nil, fmt.Errorf("acp connect response is not JSON")
	}
	if data, ok := payload["data"].(map[string]any); ok {
		payload = data
	}
	c.connectionID, _ = payload["connectionId"].(string)
	c.sessionToken, _ = payload["sessionToken"].(string)
	if c.connectionID == "" {
		return nil, fmt.Errorf("acp connect did not return connectionId")
	}
	return c, nil
}

func (c *acpClient) call(ctx context.Context, method string, params any) (map[string]any, error) {
	id := c.nextID.Add(1)
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	raw, err := c.post(ctx, "/api/v1/acp", body, true)
	if err != nil {
		return nil, err
	}
	payload, err := decodeRPC(raw, id)
	if err != nil {
		return nil, err
	}
	if errObj, ok := payload["error"]; ok && errObj != nil {
		if m, ok := errObj.(map[string]any); ok {
			if msg, _ := m["message"].(string); msg != "" {
				return nil, fmt.Errorf("%s", msg)
			}
		}
		return nil, fmt.Errorf("%v", errObj)
	}
	res, _ := payload["result"].(map[string]any)
	if res == nil {
		res = map[string]any{}
	}
	return res, nil
}

func (c *acpClient) post(ctx context.Context, path string, body any, streamOK bool) ([]byte, error) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.gw.URL+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.gw.Password)
	req.Header.Set("acp-connection-id", c.connectionID)
	if c.sessionToken != "" {
		req.Header.Set("acp-session-token", c.sessionToken)
	}
	if streamOK {
		req.Header.Set("Accept", "application/json, text/event-stream")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, b)
	}
	ctype := resp.Header.Get("Content-Type")
	if streamOK && strings.Contains(ctype, "event-stream") {
		return readSSEResult(resp.Body)
	}
	return io.ReadAll(resp.Body)
}

func readSSEResult(r io.Reader) ([]byte, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var data []string
	flush := func() []byte {
		if len(data) == 0 {
			return nil
		}
		raw := []byte(strings.Join(data, "\n"))
		data = nil
		return raw
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if raw := flush(); len(raw) > 0 {
				var obj map[string]any
				if json.Unmarshal(raw, &obj) == nil {
					if _, ok := obj["result"]; ok {
						return raw, nil
					}
					if _, ok := obj["error"]; ok {
						return raw, nil
					}
				}
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if raw := flush(); len(raw) > 0 {
		return raw, sc.Err()
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("acp stream returned no result")
}

func decodeRPC(raw []byte, id int64) (map[string]any, error) {
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return nil, fmt.Errorf("acp response is not JSON")
	}
	if data, ok := payload["data"].(map[string]any); ok {
		payload = data
	}
	if payload["id"] != nil && fmt.Sprint(payload["id"]) != strconv.FormatInt(id, 10) {
		// some gateways use string ids; a result is enough.
		if _, ok := payload["result"]; !ok {
			if _, ok := payload["error"]; !ok {
				return nil, fmt.Errorf("acp response id mismatch")
			}
		}
	}
	return payload, nil
}
