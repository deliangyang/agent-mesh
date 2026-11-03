package node

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agent-mesh/internal/driver"
	"agent-mesh/internal/i18n"
)

// MeshStatus is the local node ↔ relay settings shown in the desktop UI.
type MeshStatus struct {
	Relays    []string `json:"relays"`
	Password  string   `json:"password,omitempty"`
	Advertise string   `json:"advertise"`
	Linked    bool     `json:"linked"`
	LinkError string   `json:"link_error,omitempty"`
	Local     bool     `json:"local"`
}

type meshFile struct {
	Relays    []string `json:"relays"`
	Password  string   `json:"password,omitempty"`
	Advertise string   `json:"advertise,omitempty"`
}

func meshConfigPath() string {
	return filepath.Join(driver.MeshHome(), "mesh.json")
}

// LoadMeshFile reads ~/.agent-mesh/mesh.json when present.
func LoadMeshFile() meshFile {
	raw, err := os.ReadFile(meshConfigPath())
	if err != nil {
		return meshFile{}
	}
	var cfg meshFile
	_ = json.Unmarshal(raw, &cfg)
	cfg.Relays = normalizeRelays(cfg.Relays...)
	cfg.Advertise = strings.TrimRight(strings.TrimSpace(cfg.Advertise), "/")
	return cfg
}

func saveMeshFile(cfg meshFile) error {
	cfg.Relays = normalizeRelays(cfg.Relays...)
	cfg.Advertise = strings.TrimRight(strings.TrimSpace(cfg.Advertise), "/")
	if err := os.MkdirAll(filepath.Dir(meshConfigPath()), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(meshConfigPath(), raw, 0o600)
}

func normalizeRelays(parts ...string) []string {
	var out []string
	seen := map[string]bool{}
	for _, raw := range parts {
		for _, p := range strings.Split(raw, ",") {
			p = strings.TrimRight(strings.TrimSpace(p), "/")
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// MeshStatus returns current relay settings.
func (s *Server) MeshStatus() MeshStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return MeshStatus{
		Relays:    append([]string(nil), s.relays...),
		Password:  s.password,
		Advertise: s.advertise,
		Linked:    s.meshOK,
		LinkError: s.meshErr,
		Local:     true,
	}
}

// StartMesh begins the register / tunnel loop with the given settings.
func (s *Server) StartMesh(ctx context.Context, relays []string, advertise, password string) {
	s.applyMeshLocked(ctx, normalizeRelays(relays...), strings.TrimRight(strings.TrimSpace(advertise), "/"), password, false)
}

// ConfigureMesh updates relay settings, persists them, and reconnects.
func (s *Server) ConfigureMesh(ctx context.Context, relays []string, advertise, password string) error {
	relays = normalizeRelays(relays...)
	advertise = strings.TrimRight(strings.TrimSpace(advertise), "/")
	if advertise == "" {
		advertise = GuessAdvertise(s.Listen)
	}
	s.applyMeshLocked(ctx, relays, advertise, password, true)
	return s.ProbeMesh(ctx, relays, advertise, password)
}

func (s *Server) applyMeshLocked(ctx context.Context, relays []string, advertise, password string, persist bool) {
	s.mu.Lock()
	if s.regCancel != nil {
		s.regCancel()
		s.regCancel = nil
	}
	s.relays = relays
	s.password = password
	s.advertise = advertise
	if len(relays) == 0 {
		s.meshOK = false
		s.meshErr = ""
	}
	s.mu.Unlock()
	if persist {
		_ = saveMeshFile(meshFile{Relays: relays, Password: password, Advertise: advertise})
	}
	if ctx == nil {
		ctx = context.Background()
	}
	loopCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.regCancel = cancel
	s.mu.Unlock()
	go s.RegisterLoop(loopCtx, relays, advertise, password)
}

// ProbeMesh tries a one-shot register against the given relays (connectivity check).
func (s *Server) ProbeMesh(ctx context.Context, relays []string, advertise, password string) error {
	relays = normalizeRelays(relays...)
	advertise = strings.TrimRight(strings.TrimSpace(advertise), "/")
	if advertise == "" {
		advertise = GuessAdvertise(s.Listen)
	}
	if len(relays) == 0 {
		err := fmt.Errorf("relay URL is required")
		s.setMeshLink(false, err.Error())
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := probeRelayReachable(ctx, relays[0]); err != nil {
		s.setMeshLink(false, err.Error())
		return err
	}
	// Keep Hub credentials in sync so a successful probe can ingest immediately.
	s.Hub.SetRelays(relays)
	s.Hub.SetPassword(password)
	err := s.registerOnce(ctx, relays, advertise)
	if err != nil {
		s.setMeshLink(false, err.Error())
		return err
	}
	s.setMeshLink(true, "")
	return nil
}

func (s *Server) setMeshLink(ok bool, errText string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meshOK = ok
	s.meshErr = errText
}

type meshBody struct {
	Relay     string   `json:"relay"`
	Relays    []string `json:"relays"`
	Password  string   `json:"password"`
	Advertise string   `json:"advertise"`
}

func (b meshBody) relayList() []string {
	if len(b.Relays) > 0 {
		return normalizeRelays(b.Relays...)
	}
	return normalizeRelays(b.Relay)
}

func (s *Server) handleMesh(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.MeshStatus())
	case http.MethodPut:
		var body meshBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			i18n.Error(w, r, http.StatusBadRequest, "http.badJSON")
			return
		}
		if err := s.ConfigureMesh(r.Context(), body.relayList(), body.Advertise, body.Password); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"ok":         false,
				"error":      err.Error(),
				"mesh":       s.MeshStatus(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mesh": s.MeshStatus()})
	default:
		i18n.Error(w, r, http.StatusMethodNotAllowed, "http.methodNotAllowed")
	}
}

func (s *Server) handleMeshTest(w http.ResponseWriter, r *http.Request) {
	var body meshBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		i18n.Error(w, r, http.StatusBadRequest, "http.badJSON")
		return
	}
	relays := body.relayList()
	if len(relays) == 0 {
		st := s.MeshStatus()
		relays = st.Relays
		if body.Password == "" {
			body.Password = st.Password
		}
		if body.Advertise == "" {
			body.Advertise = st.Advertise
		}
	}
	if err := s.ProbeMesh(r.Context(), relays, body.Advertise, body.Password); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok":    false,
			"error": err.Error(),
			"mesh":  s.MeshStatus(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mesh": s.MeshStatus()})
}

func probeRelayReachable(ctx context.Context, relay string) error {
	cli := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, relay+"/healthz", nil)
	if err != nil {
		return fmt.Errorf("relay url: %w", err)
	}
	resp, err := cli.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach relay: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("relay healthz http %d", resp.StatusCode)
	}
	return nil
}
