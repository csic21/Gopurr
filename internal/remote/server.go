package remote

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"gopurr/internal/brand"
	"gopurr/internal/buildinfo"
	"gopurr/internal/localize"
)

//go:embed webui
var assetFiles embed.FS

// Server owns the HTTP listener and browser sessions. It has no reference to
// the kernel manager or the app's UI state; Host is the only bridge.
type Server struct {
	cfg  Config
	host Host
	auth *authState

	mu   sync.Mutex
	ln   net.Listener
	http *http.Server

	// ErrorHandler receives an unexpected Serve error after Start returned.
	ErrorHandler func(error)
}

func New(cfg Config, host Host) *Server {
	return &Server{cfg: cfg.Normalize(), host: host, auth: newAuthState()}
}

func (s *Server) Config() Config { return s.cfg }

func (s *Server) Start() error {
	if len(s.cfg.Token) < 16 {
		return errors.New("remote token must be at least 16 characters")
	}
	if err := s.cfg.Validate(); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", s.cfg.Address())
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

// Serve is used by tests and by Start after binding. It never blocks.
func (s *Server) Serve(ln net.Listener) error {
	s.mu.Lock()
	if s.ln != nil {
		s.mu.Unlock()
		ln.Close()
		return errors.New("remote server already started")
	}
	s.ln = ln
	s.mu.Unlock()

	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      3 * time.Minute,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	s.mu.Lock()
	s.http = srv
	s.mu.Unlock()
	go func() {
		err := srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			if s.ErrorHandler != nil {
				s.ErrorHandler(err)
			} else {
				log.Printf("remote web: %v", err)
			}
		}
	}()
	return nil
}

func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		return s.ln.Addr().String()
	}
	return s.cfg.Address()
}

func (s *Server) Stop(ctx context.Context) error {
	s.auth.clear()
	s.mu.Lock()
	srv := s.http
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/i18n", s.handleI18n)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("GET /api/session", s.withSession(s.handleSession))
	mux.HandleFunc("POST /api/logout", s.withMutation(s.handleLogout))
	mux.HandleFunc("GET /api/status", s.withSession(s.handleStatus))
	mux.HandleFunc("GET /api/groups", s.withSession(s.handleGroups))
	mux.HandleFunc("POST /api/select", s.withMutation(s.handleSelect))
	mux.HandleFunc("POST /api/mode", s.withMutation(s.handleMode))
	mux.HandleFunc("POST /api/system-proxy", s.withMutation(s.handleSystemProxy))
	mux.HandleFunc("POST /api/tun", s.withMutation(s.handleTun))
	mux.HandleFunc("GET /api/connections", s.withSession(s.handleConnections))
	mux.HandleFunc("DELETE /api/connections/{id}", s.withMutation(s.handleCloseConnection))
	mux.HandleFunc("DELETE /api/connections", s.withMutation(s.handleCloseAll))
	mux.HandleFunc("GET /api/logs", s.withSession(s.handleLogs))
	mux.HandleFunc("DELETE /api/logs", s.withMutation(s.handleClearLogs))
	mux.HandleFunc("POST /api/delay", s.withMutation(s.handleDelay))
	mux.HandleFunc("GET /favicon.ico", s.handleFavicon)
	mux.HandleFunc("GET /assets/", s.handleAsset)
	mux.HandleFunc("GET /", s.handleIndex)
	return s.securityHeaders(mux)
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Robots-Tag", "noindex, nofollow")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/" {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

type sessionContextKey struct{}

func (s *Server) withSession(next func(http.ResponseWriter, *http.Request, session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		sess, ok := s.auth.get(cookie.Value)
		if !ok {
			clearSessionCookie(w, r)
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), sessionContextKey{}, sess)), sess)
	}
}

func (s *Server) withMutation(next func(http.ResponseWriter, *http.Request, session)) http.HandlerFunc {
	return s.withSession(func(w http.ResponseWriter, r *http.Request, sess session) {
		if !sameOrigin(r) {
			writeError(w, http.StatusForbidden, "cross-origin request rejected")
			return
		}
		if !csrfMatches(sess.csrf, r.Header.Get("X-CSRF-Token")) {
			writeError(w, http.StatusForbidden, "invalid csrf token")
			return
		}
		next(w, r, sess)
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "cross-origin request rejected")
		return
	}
	ip := requestIP(r)
	if ok, wait := s.auth.loginAllowed(ip); !ok {
		w.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds()+0.5)))
		writeError(w, http.StatusTooManyRequests, "too many attempts")
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !tokenMatches(s.cfg.Token, strings.TrimSpace(req.Token)) {
		delay := s.auth.loginFailed(ip)
		s.audit(r, "login failed")
		if delay > 0 {
			w.Header().Set("Retry-After", fmt.Sprint(int(delay.Seconds()+0.5)))
			writeError(w, http.StatusTooManyRequests, "too many attempts")
			return
		}
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}
	s.auth.loginSucceeded(ip)
	token, csrf, err := s.auth.create(ip)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
		MaxAge:   int(sessionTTL / time.Second),
	})
	s.audit(r, "login")
	writeJSON(w, http.StatusOK, s.sessionResponse(csrf))
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request, sess session) {
	writeJSON(w, http.StatusOK, s.sessionResponse(sess.csrf))
}

func (s *Server) sessionResponse(csrf string) map[string]any {
	return map[string]any{
		"authenticated": true,
		"csrf":          csrf,
		"lang":          s.host.Language(),
		"version":       buildinfo.Version,
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request, _ session) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		s.auth.delete(cookie.Value)
	}
	clearSessionCookie(w, r)
	s.audit(r, "logout")
	w.WriteHeader(http.StatusNoContent)
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
		MaxAge:   -1,
	})
}

func (s *Server) handleI18n(w http.ResponseWriter, _ *http.Request) {
	lang := s.host.Language()
	out := make(map[string]string, len(webKeys))
	for _, key := range webKeys {
		out[key] = localize.Translate(lang, key)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	b, err := assetFiles.ReadFile("webui/index.html")
	if err != nil {
		http.Error(w, "console unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}

func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/assets/logo.svg", http.StatusTemporaryRedirect)
}

func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/assets/")
	clean := path.Clean("/" + name)
	if clean == "/" || strings.Contains(clean, "..") {
		http.NotFound(w, r)
		return
	}
	clean = strings.TrimPrefix(clean, "/")
	var data []byte
	var err error
	if clean == "logo.svg" {
		data = brand.LogoSVG
	} else {
		data, err = fs.ReadFile(assetFiles, "webui/assets/"+clean)
	}
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ext := path.Ext(clean)
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if (strings.HasPrefix(contentType, "text/") || contentType == "image/svg+xml" || contentType == "application/javascript") && !strings.Contains(contentType, "charset=") {
		contentType += "; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

func (s *Server) audit(r *http.Request, action string) {
	log.Printf("remote web: %s from %s", action, requestIP(r))
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) error {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		return errors.New("content type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("invalid request: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
