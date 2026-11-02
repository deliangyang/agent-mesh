package web

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
)

// NewHandler serves the embedded Vue UI and optionally reverse-proxies API calls.
func NewHandler(relayURL, password string) (http.Handler, error) {
	assets, err := fs.Sub(Dist, "dist")
	if err != nil {
		return nil, fmt.Errorf("web.NewHandler: dist: %w", err)
	}
	return newHandler(relayURL, password, assets)
}

// Attach serves the Vue UI and routes node API paths to api.
func Attach(api http.Handler) (http.Handler, error) {
	if api == nil {
		return nil, fmt.Errorf("web.Attach: api is nil")
	}
	ui, err := NewHandler("", "")
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if nodeAPI(r) {
			api.ServeHTTP(w, r)
			return
		}
		ui.ServeHTTP(w, r)
	}), nil
}

func nodeAPI(r *http.Request) bool {
	p := r.URL.Path
	if strings.HasPrefix(p, "/v1/") || p == "/healthz" {
		return true
	}
	if p == "/login" || p == "/logout" {
		return r.Method != http.MethodGet
	}
	return false
}

type handler struct {
	assets   fs.FS
	relay    *url.URL
	password string
	proxy    *httputil.ReverseProxy
}

func newHandler(relayURL, password string, assets fs.FS) (http.Handler, error) {
	h := &handler{
		assets:   assets,
		password: strings.TrimSpace(password),
	}
	relayURL = strings.TrimRight(strings.TrimSpace(relayURL), "/")
	if relayURL == "" {
		return h, nil
	}
	u, err := url.Parse(relayURL)
	if err != nil {
		return nil, fmt.Errorf("web.NewHandler: relay url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("web.NewHandler: relay url must be http or https")
	}
	h.relay = u
	proxy := httputil.NewSingleHostReverseProxy(u)
	proxy.FlushInterval = -1
	orig := proxy.Director
	proxy.Director = func(r *http.Request) {
		orig(r)
		r.Host = u.Host
		if h.password != "" && r.Header.Get("Authorization") == "" {
			r.Header.Set("Authorization", "Bearer "+h.password)
		}
	}
	h.proxy = proxy
	return h, nil
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.apiPath(r) {
		if h.proxy == nil {
			http.Error(w, "relay url is not set", http.StatusBadGateway)
			return
		}
		h.proxy.ServeHTTP(w, r)
		return
	}
	h.serveUI(w, r)
}

func (h *handler) apiPath(r *http.Request) bool {
	p := r.URL.Path
	if strings.HasPrefix(p, "/v1/") {
		return true
	}
	if p == "/login" || p == "/logout" {
		return r.Method != http.MethodGet
	}
	return false
}

func (h *handler) serveUI(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" || name == "." || name == "login" {
		name = "index.html"
	}
	if _, err := fs.Stat(h.assets, name); err != nil {
		name = "index.html"
	}
	http.ServeFileFS(w, r, h.assets, name)
}
