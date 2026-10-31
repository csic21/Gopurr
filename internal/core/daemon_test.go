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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gopurr/internal/core"
	"gopurr/internal/geodata"
	"gopurr/internal/kernel"
	"gopurr/internal/patch"

	"github.com/miekg/dns"
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
	for _, datMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("geodata-mode=%t", datMode), func(t *testing.T) {
			testDaemonLoadsSubscription(t, datMode)
		})
	}
}

func testDaemonLoadsSubscription(t *testing.T, datMode bool) {
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
	upstream, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dnsServer := &dns.Server{PacketConn: upstream, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, request *dns.Msg) {
		reply := new(dns.Msg)
		reply.SetReply(request)
		if len(request.Question) > 0 {
			reply.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: request.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.IPv4(203, 0, 113, 1)}}
		}
		_ = w.WriteMsg(reply)
	})}
	go dnsServer.ActivateAndServe()
	t.Cleanup(func() { dnsServer.Shutdown(); upstream.Close() })

	// Providers commonly ship HTTP/SOCKS ports. The app replaces these with
	// one mixed listener; retaining the HTTP port would make it claim the port
	// before the mixed listener can start.
	raw := []byte(fmt.Sprintf(`port: %d
socks-port: 0
mode: rule
log-level: warning
geo-auto-update: false
geodata-mode: %t
geox-url:
  mmdb: http://127.0.0.1:1/unavailable
  geoip: http://127.0.0.1:1/unavailable
  geosite: http://127.0.0.1:1/unavailable
  asn: http://127.0.0.1:1/unavailable
proxies: []
proxy-groups:
  - name: subscription
    type: select
    proxies: [DIRECT]
rules:
  - GEOIP,CN,subscription,no-resolve
  - GEOSITE,cn,subscription
  - IP-ASN,13335,subscription,no-resolve
  - MATCH,subscription
`, port, datMode))
	ov := patch.Default()
	ov.MixedPort = port
	localDNS := patch.DefaultDNSSettings()
	localDNS.EnhancedMode = "redir-host"
	localDNS.Nameserver = []string{"udp://" + upstream.LocalAddr().String()}
	cfg, err := patch.ApplyLayers(raw, ov, patch.ProfileOverrides{DNS: &localDNS, ExtraRules: []string{"DOMAIN,isolated.example,DIRECT"}})
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
	t.Run("kernel_DNS_diagnostic", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := r.QueryDNS(ctx, "isolated.example"); err != nil {
			t.Fatalf("DNS diagnostic through authenticated gateway: %v", err)
		}
		found := false
		for _, rule := range r.Rules() {
			if rule.Payload == "isolated.example" {
				found = true
			}
		}
		if !found {
			t.Fatal("subscription rule did not reach real kernel")
		}
	})

	t.Run("HTTP", func(t *testing.T) {
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer origin.Close()
		if _, err := r.DelayContext(context.Background(), "DIRECT", origin.URL, 2000); err != nil {
			t.Fatalf("DIRECT diagnostic against local origin: %v", err)
		}
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

	t.Run("background_database_updates", func(t *testing.T) {
		working, err := os.ReadFile(filepath.Join(opts.Home, "GeoSite.dat"))
		if err != nil {
			t.Fatal(err)
		}
		var attempts atomic.Int32
		failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("not a database"))
		}))
		defer failed.Close()
		reload := func(source string) {
			t.Helper()
			doc := strings.ReplaceAll(string(cfg), "http://127.0.0.1:1/unavailable", source)
			doc = strings.ReplaceAll(doc, "geo-auto-update: false", "geo-auto-update: true")
			if err := os.WriteFile(opts.ConfigPath, []byte(doc), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := r.Reload(opts.ConfigPath); err != nil {
				t.Fatal(err)
			}
		}
		wait := func(condition func() bool) {
			t.Helper()
			deadline := time.Now().Add(10 * time.Second)
			for !condition() {
				if time.Now().After(deadline) {
					logs, _ := os.ReadFile(logFile.Name())
					t.Fatalf("database updater timed out: %s", logs)
				}
				time.Sleep(20 * time.Millisecond)
			}
		}
		reload(failed.URL)
		wait(func() bool { return attempts.Load() >= 3 })
		// All three enabled databases failed validation; the proxy still serves
		// requests and none of the last working files was replaced.
		got, err := os.ReadFile(filepath.Join(opts.Home, "GeoSite.dat"))
		if err != nil || string(got) != string(working) {
			t.Fatal("invalid update replaced working database")
		}
		if err := r.Probe(context.Background()); err != nil {
			t.Fatalf("failed update stopped proxy: %v", err)
		}
		// Append a valid protobuf unknown field to make the new GeoSite file
		// different while keeping the original rule categories available.
		updated := append(append([]byte(nil), working...), 0x80, 0x06, 0x01)
		good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			name := strings.TrimPrefix(r.URL.Path, "/")
			if name == "GeoSite.dat" {
				_, _ = w.Write(updated)
				return
			}
			data, err := geodata.Bundled(name)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			// MMDB readers allow trailing bytes. Change the file hash without
			// changing its records to exercise mmap close, atomic save and reopen.
			if name == "geoip.metadb" || name == "ASN.mmdb" {
				data = append(data, 0)
			}
			_, _ = w.Write(data)
		}))
		defer good.Close()
		doc := strings.ReplaceAll(string(cfg), "geo-auto-update: false", "geo-auto-update: true")
		for key, name := range map[string]string{"mmdb": "geoip.metadb", "geoip": "GeoIP.dat", "geosite": "GeoSite.dat", "asn": "ASN.mmdb"} {
			doc = strings.ReplaceAll(doc, key+": http://127.0.0.1:1/unavailable", key+": "+good.URL+"/"+name)
		}
		if err := os.WriteFile(opts.ConfigPath, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := r.Reload(opts.ConfigPath); err != nil {
			t.Fatal(err)
		}
		wait(func() bool { _, err := os.Stat(filepath.Join(opts.Home, ".geo-updates.json")); return err == nil })
		got, err = os.ReadFile(filepath.Join(opts.Home, "GeoSite.dat"))
		if err != nil || string(got) != string(updated) {
			t.Fatal("valid database update was not installed")
		}
		for _, name := range []string{"ASN.mmdb", "geoip.metadb"} {
			if datMode && name == "geoip.metadb" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(opts.Home, name))
			if err != nil {
				t.Fatal(err)
			}
			baseline, err := geodata.Bundled(name)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != string(append(baseline, 0)) {
				t.Fatalf("%s was not atomically replaced", name)
			}
		}
		if err := r.Probe(context.Background()); err != nil {
			t.Fatalf("update stopped proxy: %v", err)
		}
	})
}
