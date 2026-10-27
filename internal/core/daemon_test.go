package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"gopurr/internal/core"
	"gopurr/internal/kernel"
	"gopurr/internal/patch"
)

// Keep mihomo's global state and signal handling in a separate process, just
// like the application does.
func TestDaemonHelperProcess(t *testing.T) {
	if os.Getenv("GOPURR_DAEMON_TEST") != "1" {
		return
	}
	var opts core.DaemonOptions
	if err := json.Unmarshal([]byte(os.Getenv("GOPURR_DAEMON_OPTIONS")), &opts); err != nil {
		t.Fatal(err)
	}
	if err := core.RunDaemon(opts); err != nil {
		t.Fatal(err)
	}
}

func TestDaemonLoadsSubscription(t *testing.T) {
	// A short pathname also works within macOS's unix socket path limit.
	dir, err := os.MkdirTemp("", "gp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	// Providers commonly ship HTTP/SOCKS ports. The app replaces these with
	// one mixed listener; retaining the HTTP port would make it claim the port
	// before the mixed listener can start.
	raw := []byte(fmt.Sprintf(`port: %d
socks-port: 0
mode: rule
log-level: warning
proxies: []
proxy-groups:
  - name: subscription
    type: select
    proxies: [DIRECT]
rules:
  - MATCH,subscription
`, port))
	ov := patch.Default()
	ov.MixedPort = port
	cfg, err := patch.Apply(raw, ov)
	if err != nil {
		t.Fatal(err)
	}
	opts := core.DaemonOptions{
		ConfigPath: filepath.Join(dir, "runtime.yaml"),
		Socket:     filepath.Join(dir, "core.sock"),
		Secret:     "local-regression-secret",
		Home:       filepath.Join(dir, "mihomo"),
	}
	if err := os.WriteFile(opts.ConfigPath, cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	logFile, err := os.Create(filepath.Join(dir, "core.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logFile.Close() })
	encoded, err := json.Marshal(opts)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDaemonHelperProcess$")
	cmd.Env = append(os.Environ(), "GOPURR_DAEMON_TEST=1", "GOPURR_DAEMON_OPTIONS="+string(encoded))
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})

	r := kernel.NewRemote(opts.Socket, opts.Secret)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for r.Probe(ctx) != nil {
		if ctx.Err() != nil {
			logs, _ := os.ReadFile(logFile.Name())
			t.Fatalf("daemon did not become ready: %s", logs)
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Run("reload", func(t *testing.T) {
		// runtime.yaml is next to, rather than inside, the kernel home.
		// Passing its pathname to mihomo triggers its safe-path rejection.
		if err := r.Reload(opts.ConfigPath); err != nil {
			t.Fatalf("reload subscription: %v", err)
		}
		for _, group := range r.Groups() {
			if group.Name == "subscription" && len(group.Members) == 1 && group.Members[0] == "DIRECT" {
				return
			}
		}
		t.Fatal("subscription group is missing from the controller")
	})

	t.Run("SOCKS", func(t *testing.T) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		if _, err := conn.Write([]byte{5, 1, 0}); err != nil {
			t.Fatal(err)
		}
		var reply [2]byte
		if _, err := io.ReadFull(conn, reply[:]); err != nil || reply != [2]byte{5, 0} {
			t.Fatalf("mixed port does not accept SOCKS: reply=%v err=%v", reply, err)
		}
	})

	t.Run("HTTP", func(t *testing.T) {
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer origin.Close()
		proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
		transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
		resp, err := client.Get(origin.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("HTTP proxy status = %d", resp.StatusCode)
		}
	})
}
