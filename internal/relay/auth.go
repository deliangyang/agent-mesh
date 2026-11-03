package relay

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"agent-mesh/internal/i18n"
	"agent-mesh/internal/relay/web"
)

const cookieName = "mesh_session"

func (s *Server) initAuth() {
	if s.Password == "" {
		return
	}
	sum := sha256.Sum256([]byte("agent-mesh|" + s.Password))
	s.secret = sum[:]
}

func (s *Server) sessionToken() string {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte("v1"))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) authorized(r *http.Request) bool {
	if s.Password == "" {
		return true
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		got := strings.TrimPrefix(auth, "Bearer ")
		return subtle.ConstantTimeCompare([]byte(got), []byte(s.Password)) == 1
	}
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.sessionToken())) == 1
}

func (s *Server) publicPath(path string) bool {
	switch path {
	case "/healthz", "/login", "/v1/register", "/v1/node/tunnel", "/favicon.ico", "/favicon.svg":
		return true
	}
	if strings.HasPrefix(path, "/assets/") {
		return true
	}
	return false
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || s.publicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if s.authorized(r) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			i18n.Error(w, r, http.StatusUnauthorized, "http.unauthorized")
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if s.authorized(r) {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		s.serveWeb(w, r, "index.html")
		return
	}
	if r.Method != http.MethodPost {
		i18n.Error(w, r, http.StatusMethodNotAllowed, "http.method")
		return
	}
	password := r.FormValue("password")
	if password == "" && strings.Contains(r.Header.Get("Content-Type"), "json") {
		var body struct {
			Password string `json:"password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		password = body.Password
	}
	if s.Password == "" || subtle.ConstantTimeCompare([]byte(password), []byte(s.Password)) != 1 {
		i18n.Error(w, r, http.StatusUnauthorized, "http.wrongPassword")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    s.sessionToken(),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 7,
	})
	if strings.Contains(r.Header.Get("Content-Type"), "json") {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if !s.authorized(r) {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	s.serveWeb(w, r, "index.html")
}

func (s *Server) serveWeb(w http.ResponseWriter, r *http.Request, name string) {
	sub, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		i18n.Error(w, r, http.StatusInternalServerError, "http.webMissing")
		return
	}
	http.ServeFileFS(w, r, sub, name)
}

func (s *Server) mountWeb(mux *http.ServeMux) {
	if !s.Web {
		return
	}
	sub, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		s.Log.Error("embed web", "err", err)
		return
	}
	fileServer := http.FileServer(http.FS(sub))
	mux.HandleFunc("GET /{$}", s.handleRoot)
	mux.HandleFunc("GET /login", s.handleLogin)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.Handle("GET /assets/", fileServer)
	mux.Handle("GET /favicon.svg", fileServer)
}
