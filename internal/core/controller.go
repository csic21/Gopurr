package core

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/metacubex/mihomo/tunnel/statistic"
	"gopurr/internal/configyaml"
	"gopurr/internal/controllerconfig"
	"gopurr/internal/geodata"
	"gopurr/internal/sysproxy"

	"go.yaml.in/yaml/v3"
)

// Mihomo intentionally omits authentication on Unix sockets. Its socket stays
// in a private daemon-owned directory; only this authenticated gateway is
// exposed to the UI. Debug, UI and executable upgrade routes are not exposed.
type controllerServer struct {
	*http.Server
	broker *sysproxy.ServiceBroker
}

func (s *controllerServer) Close() error {
	s.broker.Close()
	return s.Server.Close()
}

func serveController(socket, backend, secret string, geo *geoUpdater, restart ...func()) (*controllerServer, error) {
	listener, err := listenController(socket, filepath.Dir(backend))
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", backend)
	}}
	proxy := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: "localhost"})
	proxy.Transport = transport
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		// UI timeouts and closed streams are expected cancellations. Logging
		// each one can saturate the service log during a reconnect storm.
		if r.Context().Err() != nil {
			return
		}
		log.Printf("controller %s %s: %v", r.Method, r.URL.Path, err)
		http.Error(w, "Controller unavailable", http.StatusBadGateway)
	}
	proxy.FlushInterval = -1
	proxy.ModifyResponse = func(resp *http.Response) error {
		if strings.TrimSuffix(resp.Request.URL.Path, "/") == "/configs" && resp.StatusCode >= 200 && resp.StatusCode < 300 && (resp.Request.Method == http.MethodPut || resp.Request.Method == http.MethodPatch) {
			// The request handler holds geo.mu until this response is forwarded.
			geo.configDigest = resp.Request.Header.Get("X-Gopurr-Config-Digest")
			select {
			case geo.wake <- struct{}{}:
			default:
			}
		}
		return nil
	}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	broker := sysproxy.NewServiceBroker(filepath.Join(geo.home, ".system-proxy.json"))
	if err := broker.Recover(); err != nil {
		listener.Close()
		return nil, fmt.Errorf("恢复系统代理: %w", err)
	}
	server.RegisterOnShutdown(broker.Close)
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+secret)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		path := strings.TrimSuffix(r.URL.Path, "/")
		if r.Method == http.MethodConnect && path == "/gopurr/system-proxy" {
			broker.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodPost && path == "/gopurr/restart" && len(restart) != 0 {
			w.WriteHeader(http.StatusNoContent)
			go func() { time.Sleep(100 * time.Millisecond); restart[0]() }()
			return
		}
		if r.Method == http.MethodGet && path == "/gopurr/runtime" {
			geo.mu.Lock()
			defer geo.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			status := runtimeStatus(r.Context())
			status.ConfigDigest = geo.configDigest
			_ = json.NewEncoder(w).Encode(status)
			return
		}
		if r.Method == http.MethodGet && path == "/gopurr/connections/count" {
			count := 0
			statistic.DefaultManager.Range(func(statistic.Tracker) bool { count++; return true })
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(struct {
				Count int `json:"count"`
			}{count})
			return
		}
		allowed := false
		switch r.Method {
		case http.MethodGet:
			allowed = path == "/version" || path == "/configs" || path == "/traffic" || path == "/memory" || path == "/logs" || path == "/rules" || path == "/connections" || path == "/proxies" || strings.HasPrefix(path, "/proxies/") || path == "/dns/query"
		case http.MethodPatch, http.MethodPut:
			allowed = path == "/configs" || (r.Method == http.MethodPut && strings.HasPrefix(path, "/proxies/"))
		case http.MethodDelete:
			allowed = path == "/connections" || strings.HasPrefix(path, "/connections/")
		}
		if !allowed {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if (r.Method == http.MethodPut || r.Method == http.MethodPatch) && path == "/configs" {
			geo.mu.Lock()
			defer geo.mu.Unlock()
			r.Header.Del("X-Gopurr-Config-Digest")
			if r.Method == http.MethodPut {
				if err := sanitizeReload(r); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}
		}
		proxy.ServeHTTP(w, r)
	})
	server.RegisterOnShutdown(transport.CloseIdleConnections)
	go func() { _ = server.Serve(listener) }()
	return &controllerServer{server, broker}, nil
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
	geodata.Defaults(doc)
	data, err := configyaml.Marshal([]byte(request.Payload), doc)
	if err != nil {
		return err
	}
	r.Header.Set("X-Gopurr-Config-Digest", ConfigDigest([]byte(request.Payload)))
	request.Payload = string(data)
	encoded, err := json.Marshal(request)
	if err != nil {
		return err
	}
	r.Body = io.NopCloser(strings.NewReader(string(encoded)))
	r.ContentLength = int64(len(encoded))
	return nil
}
