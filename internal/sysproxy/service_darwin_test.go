//go:build darwin

package sysproxy

import (
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestCurrentDetectsEachProtocolAndAutomaticProxyConflict(t *testing.T) {
	n := newNetworkFixture()
	for i := range n.proxies {
		n.proxies[i] = proxySetting{true, "127.0.0.1", 7890}
	}
	state, err := currentUsing(n.run)
	if err != nil || !state.OwnedBy(7890) {
		t.Fatalf("complete proxy: %v %v", state, err)
	}
	for i := range n.proxies {
		n.proxies[i].enabled = false
		state, err = currentUsing(n.run)
		if err != nil || state.OwnedBy(7890) {
			t.Fatalf("disabled protocol %d appeared enabled: %v", i, err)
		}
		n.proxies[i].enabled = true
	}
	n.automatic.pac = true
	state, err = currentUsing(n.run)
	if err != nil || state.OwnedBy(7890) {
		t.Fatal("PAC conflict was ignored")
	}
	n.automatic.pac, n.automatic.discovery = false, true
	state, err = currentUsing(n.run)
	if err != nil || state.OwnedBy(7890) {
		t.Fatal("automatic discovery conflict was ignored")
	}
	n.fail = "-getsecurewebproxy Wi-Fi"
	if _, err = currentUsing(n.run); err == nil {
		t.Fatal("partial observation was advertised as complete")
	}
}

func TestProxyLeaseRestoresPACAndDiscoveryAfterCrash(t *testing.T) {
	n := newNetworkFixture()
	original := automaticProxy{"https://corp.example/proxy.pac", true, true}
	n.automatic = original
	journal := filepath.Join(t.TempDir(), "leases.json")
	leases, err := loadLeases(journal, n.run)
	if err != nil {
		t.Fatal(err)
	}
	if err := leases.enable(7890, nil); err != nil {
		t.Fatal(err)
	}
	if n.automatic.pac || n.automatic.discovery {
		t.Fatal("automatic proxy was not suspended during manual takeover")
	}
	broker := &ServiceBroker{journal: journal, command: n.run, privileged: true}
	if err := broker.Recover(); err != nil {
		t.Fatal(err)
	}
	if n.automatic != original {
		t.Fatalf("automatic proxy not restored: %+v", n.automatic)
	}
	n.assertOriginal(t)
}

func TestInstalledServiceReusesAuthorizationAndRestoresOnDisconnect(t *testing.T) {
	n := newNetworkFixture()
	dir, err := os.MkdirTemp("/tmp", "gp-broker-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.WriteFile(filepath.Join(dir, "core.secret"), []byte("fixture-token"), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", filepath.Join(dir, "core.sock"))
	if err != nil {
		t.Fatal(err)
	}
	b := &ServiceBroker{journal: filepath.Join(dir, "protected.json"), command: n.run, privileged: true}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodConnect || r.URL.Path != "/gopurr/system-proxy" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		b.ServeHTTP(w, r)
	})}
	go server.Serve(listener)
	t.Cleanup(func() { b.Close(); server.Close() })
	newClient := func() *helperController {
		return &helperController{serviceOpen: func() (net.Conn, error) {
			// The test server is deliberately unprivileged and uses fake OS
			// commands. Production verifies the service's root peer credentials.
			return connectProxyServiceUsing(dir, func(*net.UnixConn) error { return nil })
		}, open: func() (net.Conn, error) {
			t.Error("unexpected administrator prompt")
			return nil, errors.New("unexpected authorization")
		}}
	}
	for session := 0; session < 2; session++ {
		c := newClient()
		if err := c.Enable(7890, nil); err != nil {
			t.Fatal(err)
		}
		if session == 0 {
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
		} else {
			// Simulate SIGKILL of the UI: no Restore or Close request.
			c.conn.Close()
		}
		b.mu.Lock()
		b.mu.Unlock() // wait for EOF cleanup to complete
		n.assertOriginal(t)
	}
}

func TestBusyServiceDoesNotFallBackToAnotherAuthorization(t *testing.T) {
	c := &helperController{serviceOpen: func() (net.Conn, error) { return nil, errors.New("service busy") },
		open: func() (net.Conn, error) { t.Fatal("competing authorization requested"); return nil, nil }}
	if err := c.Enable(7890, nil); err == nil {
		t.Fatal("service conflict was ignored")
	}
}

func TestProxyRestoreVerifiesOSWritesAndKeepsRecoveryForRetry(t *testing.T) {
	n := newNetworkFixture()
	journal := filepath.Join(t.TempDir(), "restore.json")
	l, err := loadLeases(journal, n.run)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.enable(7890, nil); err != nil {
		t.Fatal(err)
	}
	l.command = func(name string, args ...string) (string, error) {
		if len(args) > 2 && args[0] == "-setsecurewebproxy" && args[2] == "https.example" {
			return "", nil
		}
		return n.run(name, args...)
	}
	if err := l.restore(); err == nil {
		t.Fatal("silently ignored OS write was accepted")
	}
	if _, err := os.Stat(journal); err != nil {
		t.Fatal("failed restore lost its durable snapshot")
	}
	l.command = n.run
	if err := l.restore(); err != nil {
		t.Fatal(err)
	}
	n.assertOriginal(t)
}
