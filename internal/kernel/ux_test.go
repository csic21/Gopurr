package kernel

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func uxController(t *testing.T, handler http.Handler) (*Manager, *Remote) {
	t.Helper()
	dir, err := os.MkdirTemp("", "gopurr-ux-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	manager, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", manager.socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() { _ = server.Close() })
	return manager, manager.Remote()
}

func TestDelayOptionsAndCancellation(t *testing.T) {
	started := make(chan struct{}, 1)
	_, remote := uxController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("timeout") != "12000" || r.URL.Query().Get("url") != "https://example.com/check?a=1&b=2" {
			t.Errorf("measurement options lost: %s", r.URL.RawQuery)
		}
		if !strings.Contains(r.URL.Path, "香港 / 01") {
			t.Errorf("node name lost: %s", r.URL.Path)
		}
		started <- struct{}{}
		<-r.Context().Done()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := remote.DelayContext(ctx, "香港 / 01", "https://example.com/check?a=1&b=2", 12000)
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("delay did not start")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not abort active request")
	}
}

func TestGroupsIncludeHealthAndVisibility(t *testing.T) {
	_, remote := uxController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"proxies":{
		"选择":{"type":"Selector","now":"HK","all":["HK","JP"]},
		"隐藏":{"type":"URLTest","hidden":true,"all":["HK"]},
		"HK":{"type":"Shadowsocks","history":[{"delay":120},{"delay":42}]},
		"JP":{"type":"VLESS","history":[{"delay":0}]}
		}}`))
	}))
	groups := remote.Groups()
	if len(groups) != 2 {
		t.Fatalf("groups = %#v", groups)
	}
	for _, g := range groups {
		if g.Delays["HK"] != 42 || g.MemberTypes["HK"] != "Shadowsocks" {
			t.Fatalf("missing node metadata: %#v", g)
		}
		if g.Name == "隐藏" && !g.Hidden {
			t.Fatal("hidden flag lost")
		}
	}
}

func TestFailedReloadKeepsLastRuntimeConfig(t *testing.T) {
	manager, _ := uxController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_, _ = w.Write([]byte(`{"version":"test"}`))
			return
		}
		http.Error(w, "invalid rule", http.StatusBadRequest)
	}))
	previous := []byte("rules: [MATCH,DIRECT]\n")
	if err := os.WriteFile(manager.ConfigPath(), previous, 0o600); err != nil {
		t.Fatal(err)
	}
	k := New(manager)
	if err := k.Start([]byte("invalid: config")); err == nil {
		t.Fatal("invalid reload succeeded")
	}
	got, err := os.ReadFile(manager.ConfigPath())
	if err != nil || string(got) != string(previous) {
		t.Fatalf("last working config lost: %q, %v", got, err)
	}
	if !k.Running() {
		t.Fatal("reload failure stopped working controller")
	}
	matches, _ := filepath.Glob(filepath.Join(manager.dir, ".runtime-*.yaml"))
	if len(matches) > 0 {
		t.Fatalf("left temporary files: %v", matches)
	}
}
