package core

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"gopurr/internal/controllerconfig"

	"go.yaml.in/yaml/v3"
)

// Mihomo intentionally omits authentication on Unix sockets. Its socket stays
// in a private daemon-owned directory; only this authenticated gateway is
// exposed to the UI. Debug, UI and executable upgrade routes are not exposed.
func serveController(socket, backend, secret string) (*http.Server, error) {
	listener, err := listenController(socket, filepath.Dir(backend))
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", backend)
	}}
	proxy := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: "localhost"})
	proxy.Transport = transport
	proxy.FlushInterval = -1
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+secret)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		path := strings.TrimSuffix(r.URL.Path, "/")
		allowed := false
		switch r.Method {
		case http.MethodGet:
			allowed = path == "/version" || path == "/configs" || path == "/traffic" || path == "/memory" || path == "/logs" || path == "/rules" || path == "/connections" || path == "/proxies" || strings.HasPrefix(path, "/proxies/")
		case http.MethodPatch, http.MethodPut:
			allowed = path == "/configs" || (r.Method == http.MethodPut && strings.HasPrefix(path, "/proxies/"))
		case http.MethodDelete:
			allowed = path == "/connections" || strings.HasPrefix(path, "/connections/")
		}
		if !allowed {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPut && path == "/configs" {
			if err := sanitizeReload(r); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		proxy.ServeHTTP(w, r)
	})
	server.RegisterOnShutdown(transport.CloseIdleConnections)
	go func() { _ = server.Serve(listener) }()
	return server, nil
}

func sanitizeReload(r *http.Request) error {
	const limit = 32 << 20
	raw, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	r.Body.Close()
	if err != nil {
		return err
	}
	if len(raw) > limit {
		return fmt.Errorf("配置过大")
	}
	var request struct {
		Payload string `json:"payload"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return err
	}
	if request.Payload == "" || request.Path != "" {
		return fmt.Errorf("只能通过 payload 重载配置")
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(request.Payload), &doc); err != nil {
		return err
	}
	if doc == nil {
		return fmt.Errorf("配置不能为空")
	}
	controllerconfig.Strip(doc)
	data, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	request.Payload = string(data)
	encoded, err := json.Marshal(request)
	if err != nil {
		return err
	}
	r.Body = io.NopCloser(strings.NewReader(string(encoded)))
	r.ContentLength = int64(len(encoded))
	return nil
}
