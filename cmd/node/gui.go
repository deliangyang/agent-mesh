package main

import (
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

func nodeUIURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://127.0.0.1:7700"
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func waitHealth(uiURL string) {
	cli := &http.Client{Timeout: 300 * time.Millisecond}
	deadline := time.Now().Add(8 * time.Second)
	url := strings.TrimRight(uiURL, "/") + "/healthz"
	for time.Now().Before(deadline) {
		resp, err := cli.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(80 * time.Millisecond)
	}
}
