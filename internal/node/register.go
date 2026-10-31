package node

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"agent-mesh/internal/protocol"
)

// AdvertiseURL 节点对外可达地址（中继用来回连）。
func (s *Server) AdvertiseURL(advertise string) string {
	if advertise != "" {
		return advertise
	}
	return "http://" + s.Listen
}

// GuessAdvertise 用非回环 IPv4 拼对外地址。
func GuessAdvertise(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://" + listen
	}
	if host != "" && host != "0.0.0.0" && host != "::" && host != "127.0.0.1" && host != "localhost" {
		return "http://" + listen
	}
	if ip := lanIPv4(); ip != "" {
		return "http://" + net.JoinHostPort(ip, port)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func lanIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP == nil {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || ip.IsLoopback() {
				continue
			}
			s := ip.String()
			if strings.HasPrefix(s, "169.254.") {
				continue
			}
			return s
		}
	}
	return ""
}

// RegisterLoop 周期性向中继登记，实现失效摘除与多中继。
func (s *Server) RegisterLoop(ctx context.Context, relays []string, advertise, password string) {
	s.Hub.SetRelays(relays)
	s.Hub.SetPassword(password)
	if len(relays) == 0 {
		return
	}
	go s.tunnelLoop(ctx, relays, advertise)
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	_ = s.registerOnce(ctx, relays, advertise)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = s.registerOnce(ctx, relays, advertise)
		}
	}
}

// RegisterOnce 立即向中继登记一次，供桌面「接入」按钮使用。
func (s *Server) RegisterOnce(ctx context.Context, relays []string, advertise, password string) error {
	s.Hub.SetRelays(relays)
	s.Hub.SetPassword(password)
	if len(relays) == 0 {
		return fmt.Errorf("未配置中继")
	}
	return s.registerOnce(ctx, relays, advertise)
}

func (s *Server) registerOnce(ctx context.Context, relays []string, advertise string) error {
	info := s.Info()
	info.Listen = s.AdvertiseURL(advertise)
	unix := time.Now().Unix()
	msg := []byte(fmt.Sprintf("%s|%s|%d", info.ID, info.Listen, unix))
	req := protocol.RegisterRequest{
		Info:      info,
		Unix:      unix,
		Signature: s.Ident.SignBytes(msg),
		Snapshot:  s.cachedSnapshot(ctx),
	}
	raw, _ := json.Marshal(req)
	cli := &http.Client{Timeout: 8 * time.Second}
	var last error
	ok := false
	for _, relay := range relays {
		u := relay + "/v1/register"
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(raw))
		if err != nil {
			s.Log.Warn("register build", "relay", relay, "err", err)
			last = err
			continue
		}
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := cli.Do(httpReq)
		if err != nil {
			s.Log.Warn("register", "relay", relay, "err", err)
			last = err
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			last = fmt.Errorf("register http %d", resp.StatusCode)
			s.Log.Warn("register status", "relay", relay, "code", resp.StatusCode)
			continue
		}
		ok = true
	}
	if !ok {
		if last == nil {
			return fmt.Errorf("未配置中继")
		}
		return last
	}
	return nil
}
