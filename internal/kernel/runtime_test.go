package kernel

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"gopurr/internal/core"
)

func TestStartRestoresRuntimeAfterSilentListenerFailure(t *testing.T) {
	previous := "mixed-port: 18081\ntun:\n  enable: false\n"
	active := previous
	var reloads []string
	manager, _ := uxController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			_, _ = w.Write([]byte(`{"version":"test"}`))
		case "/configs":
			var req struct{ Payload string }
			_ = json.NewDecoder(r.Body).Decode(&req)
			active = req.Payload
			reloads = append(reloads, active)
			w.WriteHeader(http.StatusNoContent)
		case "/gopurr/runtime":
			// Simulate mihomo returning 204 even though a new listener failed.
			_ = json.NewEncoder(w).Encode(core.RuntimeStatus{MixedPort: 18081, ProxyReady: active == previous})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	if err := os.WriteFile(manager.ConfigPath(), []byte(previous), 0600); err != nil {
		t.Fatal(err)
	}
	err := New(manager).Start([]byte("mixed-port: 18082\n"))
	if err == nil || !strings.Contains(err.Error(), "代理未就绪") {
		t.Fatalf("silent failure accepted: %v", err)
	}
	raw, _ := os.ReadFile(manager.ConfigPath())
	if active != previous || string(raw) != previous || len(reloads) != 2 {
		t.Fatalf("rollback incomplete: active=%q file=%q reloads=%d", active, raw, len(reloads))
	}
}

func TestRuntimeRequiresObservedTunDNSAndRoute(t *testing.T) {
	e := runtimeExpectation{MixedPort: 7890}
	e.Tun.Enable, e.Tun.AutoRoute = true, true
	good := core.RuntimeStatus{MixedPort: 7890, ProxyReady: true, TunEnabled: true, TunReady: true, DNSReady: true, AutoRoute: true}
	if err := e.check(good); err != nil {
		t.Fatal(err)
	}
	for _, broken := range []core.RuntimeStatus{
		{MixedPort: 7890, ProxyReady: true},
		{MixedPort: 7890, ProxyReady: true, TunEnabled: true, DNSReady: true, AutoRoute: true},
		{MixedPort: 7890, ProxyReady: true, TunEnabled: true, TunReady: true, AutoRoute: true},
		{MixedPort: 7890, ProxyReady: true, TunEnabled: true, TunReady: true, DNSReady: true},
	} {
		if e.check(broken) == nil {
			t.Fatalf("accepted incomplete TUN state: %+v", broken)
		}
	}
}
