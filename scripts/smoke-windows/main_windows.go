// Command smoke-windows verifies the actual SCM service and embedded Wintun on
// an ephemeral CI machine. Proxy changes are restored and automatic routing
// captures only a reserved test destination; no default route is changed.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopurr/internal/kernel"
	"gopurr/internal/service"
	"gopurr/internal/sysproxy"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func main() {
	if handled, err := sysproxy.HandleCommand(os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--proxy-owner" {
		if err := proxyOwner(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (err error) {
	binary := flag.String("binary", "", "application executable")
	flag.Parse()
	if !windows.GetCurrentProcessToken().IsElevated() {
		return errors.New("service smoke test requires an administrator CI runner")
	}
	exe, err := filepath.Abs(*binary)
	if err != nil {
		return err
	}
	if service.Installed() {
		return errors.New("refusing to replace an existing Gopurr service")
	}
	dir, err := os.MkdirTemp("", "gp-smoke-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	manager, err := kernel.NewManager(dir)
	if err != nil {
		return err
	}
	// Mixed inbound needs both TCP and UDP; a TCP-only free check lets UDP bind
	// fail inside mihomo, which then closes the TCP listener (connection refused
	// while Probe/TUN still look healthy).
	port, err := freeMixedPort()
	if err != nil {
		return err
	}
	config := fmt.Sprintf(`mixed-port: %d
allow-lan: false
mode: rule
log-level: info
ipv6: false
geo-auto-update: false
hosts:
  gopurr-smoke.invalid: 127.0.0.1
geox-url:
  mmdb: http://127.0.0.1:1/unavailable
  geoip: http://127.0.0.1:1/unavailable
  geosite: http://127.0.0.1:1/unavailable
  asn: http://127.0.0.1:1/unavailable
dns:
  enable: false
tun:
  enable: true
  device: Gopurr-CI
  stack: mixed
  auto-route: false
  auto-detect-interface: true
rules:
  - GEOIP,CN,DIRECT,no-resolve
  - GEOSITE,cn,DIRECT
  - IP-ASN,13335,DIRECT,no-resolve
  - MATCH,DIRECT
`, port)
	if err := os.WriteFile(manager.ConfigPath(), []byte(config), 0o600); err != nil {
		return err
	}
	admin := func(action string) error {
		result := filepath.Join(dir, "result.json")
		_ = os.Remove(result)
		cmd := exec.Command(exe, "--service-admin", action, dir, result)
		out, cmdErr := cmd.CombinedOutput()
		data, _ := os.ReadFile(result)
		var status struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(data, &status); err != nil {
			return fmt.Errorf("%s: %v (%s), result %s", action, cmdErr, out, data)
		}
		if status.Error != "" {
			return errors.New(status.Error)
		}
		return cmdErr
	}
	defer func() {
		if cleanupErr := admin("uninstall"); cleanupErr != nil && err == nil {
			err = cleanupErr
		}
		if err != nil {
			if logs := service.LogTail(dir); logs != "" {
				fmt.Fprintln(os.Stderr, logs)
			}
		}
	}()
	wait := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		for {
			if manager.Remote().Probe(ctx) == nil && manager.IsRoot() {
				// Status is written only after mixed-port accepts; still dial here so
				// old builds and transient closes fail before the HTTP step.
				conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
				if err == nil {
					conn.Close()
					return nil
				}
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	if err := admin("install"); err != nil {
		return err
	}
	if err := wait(); err != nil {
		return fmt.Errorf("service readiness: %w", err)
	}
	before, err := manager.Status()
	if err != nil {
		return err
	}
	adapterContext, cancelAdapter := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancelAdapter()
	adapter := exec.CommandContext(adapterContext, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `
$ErrorActionPreference = 'Stop'
$deadline = (Get-Date).AddSeconds(25)
do {
  $adapter = Get-NetAdapter -IncludeHidden -Name Gopurr-CI -ErrorAction SilentlyContinue
  if ($adapter) { $adapter | Format-List Name,Status,InterfaceDescription; exit 0 }
  Start-Sleep -Milliseconds 500
} while ((Get-Date) -lt $deadline)
Get-NetAdapter -IncludeHidden | Format-List Name,Status,InterfaceDescription
throw 'Wintun adapter Gopurr-CI was not created'
`)
	if out, err := adapter.CombinedOutput(); err != nil {
		return fmt.Errorf("Wintun adapter missing: %v %s", err, out)
	} else {
		fmt.Print(string(out))
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer origin.Close()
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	httpDeadline := time.Now().Add(20 * time.Second)
	var lastHTTP error
	for {
		resp, err := client.Get(origin.URL)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				lastHTTP = fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
			} else {
				lastHTTP = nil
				break
			}
		} else {
			lastHTTP = err
		}
		if time.Now().After(httpDeadline) {
			return fmt.Errorf("HTTP proxy through service: %w", lastHTTP)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err := manager.Remote().Reload(manager.ConfigPath()); err != nil {
		return fmt.Errorf("reload: %w", err)
	}
	if err := testSystemProxy(port, origin.URL, dir); err != nil {
		return err
	}
	fmt.Println("PASS: Windows system proxy request and complete crash restoration")
	if err := testTunTraffic(manager, port, origin.URL, config); err != nil {
		return err
	}
	fmt.Println("PASS: native TUN traffic, automatic routing and DNS hijack")
	if err := testServiceLifecycle(manager); err != nil {
		return err
	}
	fmt.Println("PASS: SCM stop, start and explicit restart")
	if err := testServiceRecovery(manager); err != nil {
		return err
	}
	fmt.Println("PASS: SCM recovery after forced kernel termination")
	before, err = manager.Status()
	if err != nil {
		return err
	}
	if err := admin("install"); err != nil {
		return fmt.Errorf("service upgrade: %w", err)
	}
	if err := wait(); err != nil {
		return fmt.Errorf("upgraded service readiness: %w", err)
	}
	after, err := manager.Status()
	if err != nil {
		return err
	}
	if before.PID == after.PID {
		return errors.New("upgrade did not replace service process")
	}
	fmt.Printf("SCM install/recovery, system proxy/crash restoration, TUN routing/DNS, reload and service upgrade passed (PID %d -> %d).\n", before.PID, after.PID)
	return nil
}

func proxyOwner(args []string) error {
	if len(args) != 3 {
		return errors.New("proxy owner arguments missing")
	}
	port, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	controller := sysproxy.NewControllerAt(args[0])
	if err := controller.Enable(port, nil); err != nil {
		return err
	}
	if err := os.WriteFile(args[2], []byte("ready"), 0600); err != nil {
		return err
	}
	select {} // the CI parent kills this process to exercise the real watcher
}

func proxyEqual(a, b sysproxy.State) bool {
	return a.Flags == b.Flags && a.RawServer == b.RawServer && a.Bypass == b.Bypass && a.PAC == b.PAC
}

func testSystemProxy(port int, origin, dir string) error {
	original, err := sysproxy.Current()
	if err != nil {
		return err
	}
	defer sysproxy.Restore(original)
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	ready := filepath.Join(dir, "proxy-ready")
	child := exec.Command(exe, "--proxy-owner", filepath.Join(dir, "proxy-recovery.json"), strconv.Itoa(port), ready)
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		return err
	}
	waited := false
	defer func() {
		if !waited {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	}()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return errors.New("proxy owner not ready")
		}
		time.Sleep(100 * time.Millisecond)
	}
	target := strings.Replace(origin, "127.0.0.1", "gopurr-smoke.invalid", 1)
	// .NET reads the user's actual Windows proxy settings. A Go HTTP client
	// with ProxyURL would only test the listener, not OS proxy integration.
	command := `$ErrorActionPreference='Stop'; $url=$env:GOPURR_SMOKE_TARGET; $proxy=[System.Net.WebRequest]::GetSystemWebProxy(); if ($proxy.GetProxy([uri]$url).Port -ne [int]$env:GOPURR_SMOKE_PORT) { throw 'System proxy was not applied' }; $r=[System.Net.WebRequest]::Create($url); $r.Proxy=$proxy; $r.Timeout=10000; $s=$r.GetResponse(); try { if ([int]$s.StatusCode -ne 204) { throw 'Unexpected HTTP response' } } finally { $s.Close() }`
	probe := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command)
	probe.Env = append(os.Environ(), "GOPURR_SMOKE_TARGET="+target, "GOPURR_SMOKE_PORT="+strconv.Itoa(port))
	if out, err := probe.CombinedOutput(); err != nil {
		return fmt.Errorf("Windows system proxy request: %w %s", err, out)
	}
	_ = child.Process.Kill()
	_ = child.Wait()
	waited = true
	deadline = time.Now().Add(15 * time.Second)
	for {
		current, err := sysproxy.Current()
		if err == nil && proxyEqual(current, original) {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("crash watcher did not restore full Windows proxy settings")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func testTunTraffic(manager *kernel.Manager, port int, origin, original string) (err error) {
	bridge, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer bridge.Close()
	target, _ := url.Parse(origin)
	go func() {
		for {
			conn, err := bridge.Accept()
			if err != nil {
				return
			}
			go bridgeSOCKS(conn, target.Host)
		}
	}()
	config := fmt.Sprintf(`mixed-port: %d
mode: rule
geo-auto-update: false
ipv6: false
hosts:
  gopurr-tun.invalid: 203.0.113.77
dns:
  enable: true
  enhanced-mode: redir-host
  nameserver: [127.0.0.1:1]
tun:
  enable: true
  device: Gopurr-CI
  stack: mixed
  auto-route: true
  strict-route: true
  auto-detect-interface: true
  inet4-address: [198.18.0.1/30]
  route-address: [203.0.113.77/32]
  dns-hijack: [any:53, tcp://any:53]
proxies:
  - {name: ci-bridge, type: socks5, server: 127.0.0.1, port: %d}
rules: ["MATCH,ci-bridge"]
`, port, bridge.Addr().(*net.TCPAddr).Port)
	defer func() {
		_ = os.WriteFile(manager.ConfigPath(), []byte(original), 0600)
		if restoreErr := manager.Remote().Reload(manager.ConfigPath()); err == nil {
			err = restoreErr
		}
	}()
	if err := os.WriteFile(manager.ConfigPath(), []byte(config), 0600); err != nil {
		return err
	}
	if err := manager.Remote().Reload(manager.ConfigPath()); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	state, err := manager.Remote().Runtime(ctx)
	if err != nil || !state.TunReady || !state.AutoRoute || !state.DNSReady {
		return fmt.Errorf("TUN not actually ready: %+v %v", state, err)
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	response, err := client.Get("http://gopurr-tun.invalid/")
	if err != nil {
		return fmt.Errorf("native TUN DNS/routing request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != 204 {
		return fmt.Errorf("TUN HTTP status %d", response.StatusCode)
	}
	return nil
}

func bridgeSOCKS(conn net.Conn, target string) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	var header [2]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil || header[0] != 5 {
		return
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}
	_, _ = conn.Write([]byte{5, 0})
	var request [4]byte
	if _, err := io.ReadFull(conn, request[:]); err != nil || request[1] != 1 {
		return
	}
	length := 0
	switch request[3] {
	case 1:
		length = 4
	case 4:
		length = 16
	case 3:
		var n [1]byte
		if _, err := io.ReadFull(conn, n[:]); err != nil {
			return
		}
		length = int(n[0])
	default:
		return
	}
	if _, err := io.CopyN(io.Discard, conn, int64(length+2)); err != nil {
		return
	}
	upstream, err := net.DialTimeout("tcp", target, time.Second)
	if err != nil {
		return
	}
	defer upstream.Close()
	_, _ = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
	go func() { _, _ = io.Copy(upstream, conn); _ = upstream.Close() }()
	_, _ = io.Copy(conn, upstream)
}

func testServiceRecovery(manager *kernel.Manager) error {
	before, err := manager.Status()
	if err != nil {
		return err
	}
	process, err := os.FindProcess(before.PID)
	if err != nil {
		return err
	}
	if err := process.Kill(); err != nil {
		return err
	}
	if err := waitServicePID(manager, before.PID); err != nil {
		return fmt.Errorf("SCM did not restore the crashed kernel's data path: %w", err)
	}
	return nil
}

func testServiceLifecycle(manager *kernel.Manager) error {
	before, err := manager.Status()
	if err != nil {
		return err
	}
	if err := service.StopRunning(); err != nil {
		return fmt.Errorf("SCM stop: %w", err)
	}
	// A user stop must stay stopped; SCM failure recovery must not undo it.
	time.Sleep(3 * time.Second)
	// Running is a deliberately cached UI hint. Query SCM itself so cached
	// controller evidence cannot be mistaken for an unexpected restart.
	scm, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("query SCM after stop: %w", err)
	}
	svcHandle, err := scm.OpenService(service.Label)
	if err != nil {
		scm.Disconnect()
		return fmt.Errorf("open service after stop: %w", err)
	}
	state, err := svcHandle.Query()
	svcHandle.Close()
	scm.Disconnect()
	if err != nil {
		return fmt.Errorf("query service after stop: %w", err)
	}
	if state.State != svc.Stopped {
		return fmt.Errorf("SCM restarted an intentionally stopped service: state %v", state.State)
	}
	if manager.Health(time.Second) == kernel.HealthOK {
		return errors.New("controller still responds after SCM stop")
	}
	if err := service.EnsureRunning(); err != nil {
		return fmt.Errorf("SCM start: %w", err)
	}
	if err := waitServicePID(manager, before.PID); err != nil {
		return err
	}
	started, err := manager.Status()
	if err != nil {
		return err
	}
	if err := service.Restart(filepath.Dir(manager.ConfigPath())); err != nil {
		return fmt.Errorf("SCM restart: %w", err)
	}
	return waitServicePID(manager, started.PID)
}

func waitServicePID(manager *kernel.Manager, previous int) error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		status, err := manager.Status()
		if err == nil && status.PID != previous && manager.Running() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			runtime, err := manager.Remote().Runtime(ctx)
			cancel()
			if err == nil && runtime.ProxyReady {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("SCM service did not become ready with a new process")
}

func freeMixedPort() (int, error) {
	var lastErr error
	for i := 0; i < 128; i++ {
		// Windows can reserve a whole UDP block inside TCP's ephemeral range.
		// After probing port zero, sample below that range rather than walking
		// sequentially through one excluded block on a hosted runner.
		candidate := 0
		if i >= 16 {
			candidate = 10000 + rand.IntN(30000)
		}
		address := net.JoinHostPort("127.0.0.1", strconv.Itoa(candidate))
		ln, err := net.Listen("tcp4", address)
		if err != nil {
			lastErr = err
			continue
		}
		port := ln.Addr().(*net.TCPAddr).Port
		udp, err := net.ListenPacket("udp4", ln.Addr().String())
		if err != nil {
			lastErr = err
			ln.Close()
			continue
		}
		udp.Close()
		ln.Close()
		return port, nil
	}
	return 0, fmt.Errorf("could not reserve a free TCP+UDP mixed port: %w", lastErr)
}
