package remote

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type stubHost struct {
	mu       sync.Mutex
	selected string
	mode     string
	proxyOn  bool
	tunOn    bool
	closed   []string
	closeAll int
	cleared  int
	delayed  []string
	language string
}

func (h *stubHost) Status(context.Context) (Status, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return Status{
		Version: "test", Kernel: true, Mode: h.mode,
		SystemProxy: h.proxyOn, SystemProxyAvailable: true,
		Tun: h.tunOn, TunAvailable: true, TunInstalled: true,
		Traffic:     Traffic{Down: 12, Up: 3, DownTotal: 100, UpTotal: 50, At: time.Now().UnixMilli()},
		Connections: 2, Rules: 3, Proxies: 4,
	}, nil
}

func (h *stubHost) Groups(context.Context) ([]Group, error) {
	return []Group{{Name: "Proxy", Type: "Selector", Now: "A", Members: []Member{{Name: "A", Type: "Shadowsocks", Delay: intPtr(42), DelayKnown: true}}}}, nil
}

func (h *stubHost) Select(_ context.Context, group, member string) error {
	h.mu.Lock()
	h.selected = group + "/" + member
	h.mu.Unlock()
	return nil
}

func (h *stubHost) SetMode(_ context.Context, mode string) error {
	h.mu.Lock()
	h.mode = mode
	h.mu.Unlock()
	return nil
}

func (h *stubHost) SetSystemProxy(_ context.Context, on bool) error {
	h.mu.Lock()
	h.proxyOn = on
	h.mu.Unlock()
	return nil
}

func (h *stubHost) SetTun(_ context.Context, on bool) error {
	h.mu.Lock()
	h.tunOn = on
	h.mu.Unlock()
	return nil
}

func (h *stubHost) Connections(context.Context) ([]Connection, error) {
	return []Connection{{ID: "c1", Host: "example.com", Chain: "Proxy » A", Start: time.Now()}}, nil
}

func (h *stubHost) CloseConnection(_ context.Context, id string) error {
	h.mu.Lock()
	h.closed = append(h.closed, id)
	h.mu.Unlock()
	return nil
}

func (h *stubHost) CloseAllConnections(context.Context) error {
	h.mu.Lock()
	h.closeAll++
	h.mu.Unlock()
	return nil
}

func (h *stubHost) Logs(context.Context) ([]LogEntry, error) {
	return []LogEntry{{At: time.Now(), Level: "info", Text: "hello"}}, nil
}

func (h *stubHost) ClearLogs(context.Context) error {
	h.mu.Lock()
	h.cleared++
	h.mu.Unlock()
	return nil
}

func (h *stubHost) Delay(_ context.Context, group string, names []string) (map[string]DelayResult, error) {
	h.mu.Lock()
	h.delayed = append(append([]string(nil), names...), group)
	h.mu.Unlock()
	return map[string]DelayResult{"A": {Delay: 42}}, nil
}

func (h *stubHost) Language() string {
	if h.language != "" {
		return h.language
	}
	return "en"
}

func intPtr(value int) *int { return &value }

func newTestServer(t *testing.T) (*Server, *stubHost, *httptest.Server, *http.Client) {
	t.Helper()
	host := &stubHost{mode: "rule", language: "en"}
	srv := New(Config{Enabled: true, Bind: "127.0.0.1", Port: 7899, Token: "secret-token-123456"}, host)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return srv, host, httpSrv, &http.Client{Jar: jar}
}

func postJSON(t *testing.T, client *http.Client, url, body string, csrf string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func login(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	resp := postJSON(t, client, base+"/api/login", `{"token":"secret-token-123456"}`, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", resp.StatusCode)
	}
	var session struct {
		CSRF string `json:"csrf"`
		Lang string `json:"lang"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	if session.CSRF == "" || session.Lang != "en" {
		t.Fatalf("login session = %+v", session)
	}
	return session.CSRF
}

func TestLoginAndMutationRequireSessionAndCSRF(t *testing.T) {
	_, host, httpSrv, client := newTestServer(t)
	base := httpSrv.URL

	resp := postJSON(t, client, base+"/api/login", `{"token":"wrong"}`, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d", resp.StatusCode)
	}

	csrf := login(t, client, base)
	resp = postJSON(t, client, base+"/api/select", `{"group":"Proxy","member":"A"}`, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing csrf status = %d", resp.StatusCode)
	}

	resp = postJSON(t, client, base+"/api/select", `{"group":"Proxy","member":"A"}`, csrf)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mutation status = %d", resp.StatusCode)
	}
	if host.selected != "Proxy/A" {
		t.Fatalf("host selected = %q", host.selected)
	}
}

func TestStatusAndRawControllerStaysHidden(t *testing.T) {
	_, _, httpSrv, client := newTestServer(t)
	base := httpSrv.URL
	login(t, client, base)

	resp, err := client.Get(base + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d", resp.StatusCode)
	}
	var status Status
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if !status.Kernel || status.Mode != "rule" || status.Traffic.Down != 12 {
		t.Fatalf("status = %+v", status)
	}

	for _, path := range []string{"/proxies", "/configs", "/connections/raw"} {
		resp, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("%s status = %d, want not found", path, resp.StatusCode)
		}
	}
}

func TestSecurityHeadersAndI18n(t *testing.T) {
	_, _, httpSrv, client := newTestServer(t)
	resp, err := client.Get(httpSrv.URL + "/api/i18n")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("X-Frame-Options") != "DENY" || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatalf("security headers = %+v", resp.Header)
	}
	var dictionary map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&dictionary); err != nil {
		t.Fatal(err)
	}
	if dictionary["远程控制台"] != "Remote Console" {
		t.Fatalf("translation = %q", dictionary["远程控制台"])
	}
}

func TestLoginRateLimit(t *testing.T) {
	auth := newAuthState()
	for i := 0; i < 5; i++ {
		auth.loginFailed("127.0.0.1")
	}
	if ok, wait := auth.loginAllowed("127.0.0.1"); ok || wait <= 0 {
		t.Fatalf("limiter ok=%v wait=%v, want blocked", ok, wait)
	}
	auth.loginSucceeded("127.0.0.1")
	if ok, _ := auth.loginAllowed("127.0.0.1"); !ok {
		t.Fatal("successful login did not clear the limiter")
	}
}

func TestLogoutClearsSession(t *testing.T) {
	_, _, httpSrv, client := newTestServer(t)
	base := httpSrv.URL
	csrf := login(t, client, base)
	resp := postJSON(t, client, base+"/api/logout", `{}`, csrf)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d", resp.StatusCode)
	}
	resp, err := client.Get(base + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status after logout = %d", resp.StatusCode)
	}
}

func TestServerStartStop(t *testing.T) {
	host := &stubHost{mode: "rule"}
	srv := New(Config{Enabled: true, Bind: "127.0.0.1", Port: 1, Token: "token-1234567890"}, host)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Serve(ln); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestConfigRoundTripAndAccessURLs(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/remote.json"
	cfg := Config{Enabled: true, Bind: "127.0.0.1", Port: 7899}
	if err := cfg.EnsureToken(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != cfg.Token || got.Bind != cfg.Bind || got.Port != cfg.Port || !got.Enabled {
		t.Fatalf("round trip = %+v", got)
	}
	urls := AccessURLs(got)
	if len(urls) != 1 || urls[0] != "http://127.0.0.1:7899" {
		t.Fatalf("urls = %v", urls)
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	_, _, httpSrv, client := newTestServer(t)
	resp := postJSON(t, client, httpSrv.URL+"/api/login", `{"token":"secret-token-123456"}`, "")
	defer resp.Body.Close()
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %+v", cookies)
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Fatalf("cookie = %+v", cookie)
	}
}

func TestLoginRateLimitOverHTTP(t *testing.T) {
	_, _, httpSrv, client := newTestServer(t)
	for i := 0; i < 4; i++ {
		resp := postJSON(t, client, httpSrv.URL+"/api/login", `{"token":"wrong"}`, "")
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d", i, resp.StatusCode)
		}
	}
	resp := postJSON(t, client, httpSrv.URL+"/api/login", `{"token":"wrong"}`, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("blocked status = %d", resp.StatusCode)
	}
}

func TestIndexAndAssetsAreServed(t *testing.T) {
	_, _, httpSrv, client := newTestServer(t)
	resp, err := client.Get(httpSrv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `<div id="root">`) {
		t.Fatalf("index status=%d body=%q", resp.StatusCode, body)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("index cache = %q", resp.Header.Get("Cache-Control"))
	}
	for _, name := range []string{"app.js", "app.css"} {
		resp, err := client.Get(httpSrv.URL + "/assets/" + name)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d", name, resp.StatusCode)
		}
		if resp.Header.Get("Cache-Control") != "no-cache" {
			t.Fatalf("%s cache = %q", name, resp.Header.Get("Cache-Control"))
		}
	}
}

func TestConfigValidation(t *testing.T) {
	if err := (Config{Bind: "example.com", Port: 7899}).Validate(); err == nil {
		t.Fatal("hostname bind accepted")
	}
	if err := (Config{Bind: "0.0.0.0", Port: 0}).Validate(); err == nil {
		t.Fatal("zero port accepted")
	}
	if cfg := (Config{}).Normalize(); cfg.Bind != DefaultBind || cfg.Port != DefaultPort {
		t.Fatalf("normalize = %+v", cfg)
	}
}

func TestConfigFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows protection is an ACL, not a Unix permission bit.
		t.Skip("Unix permission bits are not meaningful on Windows")
	}
	path := filepath.Join(t.TempDir(), "remote.json")
	cfg := Config{Enabled: true, Bind: "0.0.0.0", Port: 7899}
	if err := cfg.EnsureToken(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}
