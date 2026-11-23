package engine

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gopurr/internal/core"
	"gopurr/internal/mesh"
	"gopurr/internal/profile"

	ssCore "github.com/metacubex/mihomo/transport/shadowsocks/core"
	"github.com/metacubex/mihomo/transport/socks5"
	"github.com/metacubex/mihomo/tunnel"
)

type fakeMeshBackend struct {
	mu           sync.Mutex
	status       mesh.Status
	visits       []mesh.Visit
	connectAuth  string
	connectErr   error
	switchErr    error
	calls        []string
	probeResult  mesh.ProbeResult
	probeStarted chan struct{}
	probeRelease chan struct{}
	probeOnce    sync.Once
}

func newFakeMeshBackend() *fakeMeshBackend {
	return &fakeMeshBackend{
		status: mesh.Status{
			Installed:    true,
			Official:     true,
			ShieldsKnown: true,
			ShieldsUp:    true,
			State:        "Stopped",
			Version:      "1.80.0-test",
		},
	}
}

func (b *fakeMeshBackend) Read(context.Context) (mesh.Status, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.status
	s.Peers = slices.Clone(b.status.Peers)
	s.RouteIPs = slices.Clone(b.status.RouteIPs)
	s.Services = slices.Clone(b.status.Services)
	s.Blocked = slices.Clone(b.status.Blocked)
	s.Health = slices.Clone(b.status.Health)
	return s, nil
}

func (b *fakeMeshBackend) Connect(context.Context) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, "connect")
	if b.connectErr != nil {
		return "", b.connectErr
	}
	if b.connectAuth != "" {
		b.status.Running = false
		b.status.State = "NeedsLogin"
		b.status.AuthURL = b.connectAuth
		return b.connectAuth, nil
	}
	b.status.Running = true
	b.status.State = "Running"
	b.status.AuthURL = ""
	b.status.ShieldsUp = true
	return "", nil
}

func (b *fakeMeshBackend) SetIncoming(_ context.Context, allow bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if allow {
		b.calls = append(b.calls, "allow")
	} else {
		b.calls = append(b.calls, "block")
	}
	if b.switchErr != nil {
		return b.switchErr
	}
	b.status.ShieldsUp = !allow
	return nil
}

func (b *fakeMeshBackend) Disconnect(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, "disconnect")
	b.status.Running = false
	b.status.State = "Stopped"
	b.status.AuthURL = ""
	b.status.ShieldsUp = true
	b.status.RouteIPs = nil
	b.status.ProxyAddress = ""
	b.status.ProxyPassword = ""
	return nil
}

func (b *fakeMeshBackend) Accesses(context.Context) (mesh.Observation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return mesh.Observation{Scope: "test", Visits: slices.Clone(b.visits)}, nil
}

func (b *fakeMeshBackend) SetServices(_ context.Context, raw string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, "services:"+raw)
	services, err := mesh.ParseServices(raw)
	if err != nil {
		return err
	}
	b.status.Services = services
	b.status.ShieldsUp = true
	return nil
}

func (b *fakeMeshBackend) SetDeviceBlocked(_ context.Context, id string, blocked bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, fmt.Sprintf("block:%s:%v", id, blocked))
	if blocked {
		if !slices.Contains(b.status.Blocked, id) {
			b.status.Blocked = append(b.status.Blocked, id)
		}
	} else {
		b.status.Blocked = slices.DeleteFunc(b.status.Blocked, func(v string) bool { return v == id })
	}
	return nil
}

func (b *fakeMeshBackend) Probe(ctx context.Context, id, protocol string, port uint16) mesh.ProbeResult {
	if b.probeStarted != nil {
		b.probeOnce.Do(func() { close(b.probeStarted) })
	}
	if b.probeRelease != nil {
		select {
		case <-b.probeRelease:
		case <-ctx.Done():
			return mesh.ProbeResult{DeviceID: id, Port: port, State: "unknown", Message: "cancelled", Checked: time.Now()}
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	res := b.probeResult
	if res.DeviceID == "" {
		res.DeviceID = id
	}
	if res.Port == 0 {
		res.Port = port
	}
	if res.State == "" {
		res.State = "connected"
	}
	if res.Message == "" {
		res.Message = protocol + " ok"
	}
	if res.Checked.IsZero() {
		res.Checked = time.Now()
	}
	return res
}

func (b *fakeMeshBackend) ClearHistory() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, "clearHistory")
	b.visits = nil
}

func (b *fakeMeshBackend) hasCall(target string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Contains(b.calls, target)
}

func newMeshTestEngine(t *testing.T, backend mesh.Backend) *Engine {
	t.Helper()
	home := t.TempDir()
	store, err := profile.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	port, err := availablePort()
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{
		core:        core.New(filepath.Join(home, "mihomo")),
		store:       store,
		meshBackend: backend,
		home:        home,
		port:        port,
		mode:        "rule",
		vpnFD:       -1,
		delays:      map[string]uint16{},
		logsDone:    make(chan struct{}),
	}
	close(e.logsDone)
	e.ensureMeshLocked()
	t.Cleanup(func() {
		e.CancelPending()
		e.mu.Lock()
		ctrl := e.mesh
		e.closed = true
		e.mu.Unlock()
		if ctrl != nil {
			_ = ctrl.Shutdown()
		}
		e.mu.Lock()
		e.stopLocked()
		e.mu.Unlock()
	})
	return e
}

func callErr(t *testing.T, e *Engine, method string, params any) *Error {
	t.Helper()
	body, err := json.Marshal(map[string]any{"id": "err-test", "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Result json.RawMessage `json:"result"`
		Error  *Error          `json:"error"`
	}
	if err := json.Unmarshal(e.Dispatch(context.Background(), body), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error == nil {
		t.Fatalf("%s unexpectedly succeeded: %s", method, response.Result)
	}
	return response.Error
}

func startMockSSBridge(t *testing.T, tag string, directForwardAddr string) (string, string, *atomic.Int64, func() []string) {
	t.Helper()
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		t.Fatal(err)
	}
	password := hex.EncodeToString(secret[:])
	cipher, err := ssCore.PickCipher("chacha20-ietf-poly1305", nil, password)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	var hits atomic.Int64
	var mu sync.Mutex
	var destinations []string

	go func() {
		for {
			rawConn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				stream := cipher.StreamConn(c)
				addr, err := socks5.ReadAddr(stream, make([]byte, socks5.MaxAddrLen))
				if err != nil {
					return
				}
				dst := addr.String()
				hits.Add(1)
				mu.Lock()
				destinations = append(destinations, dst)
				mu.Unlock()

				if directForwardAddr != "" {
					upstream, err := net.DialTimeout("tcp", directForwardAddr, 2*time.Second)
					if err != nil {
						return
					}
					defer upstream.Close()
					done := make(chan struct{}, 1)
					go func() { _, _ = io.Copy(upstream, stream); done <- struct{}{} }()
					_, _ = io.Copy(stream, upstream)
					<-done
					return
				}

				req, err := http.ReadRequest(bufio.NewReader(stream))
				if err != nil {
					return
				}
				_ = req.Body.Close()
				body := tag + ":" + dst
				resp := fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
				_, _ = io.WriteString(stream, resp)
			}(rawConn)
		}
	}()

	getDsts := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(destinations)
	}
	return ln.Addr().String(), password, &hits, getDsts
}

func TestMeshRPCMethodsAndStatusSummary(t *testing.T) {
	backend := newFakeMeshBackend()
	backend.status.Tailnet = "example.ts.net"
	backend.status.Self = mesh.Device{
		ID:      "self-1",
		Name:    "pixel-phone",
		DNSName: "pixel-phone.example.ts.net",
		OS:      "android",
		Owner:   "Alice",
		Login:   "alice@example.com",
		IPs:     []netip.Addr{netip.MustParseAddr("100.64.0.1")},
		Online:  true,
	}
	backend.status.Peers = []mesh.Device{{
		ID:      "peer-1",
		Name:    "mac-mini",
		DNSName: "mac-mini.example.ts.net",
		OS:      "darwin",
		Owner:   "Alice",
		Login:   "alice@example.com",
		IPs:     []netip.Addr{netip.MustParseAddr("100.64.0.2"), netip.MustParseAddr("fd7a:115c:a1e0::2")},
		Online:  true,
	}}
	backend.status.RouteIPs = []netip.Addr{netip.MustParseAddr("100.64.0.2"), netip.MustParseAddr("fd7a:115c:a1e0::2")}
	backend.visits = []mesh.Visit{{
		Key:       "v1",
		DeviceID:  "peer-1",
		Device:    "mac-mini",
		Owner:     "Alice",
		Login:     "alice@example.com",
		Remote:    "100.64.0.2:40000",
		Local:     "100.64.0.1:8080",
		Protocol:  "tcp",
		Active:    false,
		FirstSeen: time.Now().Add(-time.Minute),
		LastSeen:  time.Now(),
	}}

	e := newMeshTestEngine(t, backend)

	// Untouched mesh: engine.status MUST omit "mesh" so existing assertions stay intact.
	rawEngineStatus := call(t, e, "engine.status", nil)
	var statusMap map[string]any
	if err := json.Unmarshal(rawEngineStatus, &statusMap); err != nil {
		t.Fatal(err)
	}
	if _, exists := statusMap["mesh"]; exists {
		t.Fatalf("untouched engine.status included mesh summary: %s", rawEngineStatus)
	}

	// Untouched mesh.status returns full lowerCamelCase snapshot with non-nil arrays.
	var initialSnap map[string]any
	if err := json.Unmarshal(call(t, e, "mesh.status", nil), &initialSnap); err != nil {
		t.Fatal(err)
	}
	if initialSnap["managed"] != false {
		t.Fatalf("unexpected initial managed state: %v", initialSnap)
	}

	// First connect returns NeedsLogin + valid AuthURL -> mesh.action login returns URL.
	backend.mu.Lock()
	backend.connectAuth = "https://login.tailscale.com/a/example-token"
	backend.mu.Unlock()
	loginSnapRaw := call(t, e, "mesh.action", map[string]any{"action": "connect"})
	var loginSnap meshStatusResult
	if err := json.Unmarshal(loginSnapRaw, &loginSnap); err != nil {
		t.Fatal(err)
	}
	if !loginSnap.Managed || loginSnap.Status.State != "NeedsLogin" || loginSnap.Status.AuthURL != "https://login.tailscale.com/a/example-token" {
		t.Fatalf("unexpected login snapshot: %+v", loginSnap)
	}
	var loginRes map[string]string
	if err := json.Unmarshal(call(t, e, "mesh.action", map[string]any{"action": "login"}), &loginRes); err != nil {
		t.Fatal(err)
	}
	if loginRes["url"] != "https://login.tailscale.com/a/example-token" {
		t.Fatalf("unexpected login URL result: %+v", loginRes)
	}
	var engineStatus Status
	_ = json.Unmarshal(call(t, e, "engine.status", nil), &engineStatus)
	if engineStatus.Mesh == nil || engineStatus.Mesh.State != "NeedsLogin" || engineStatus.Mesh.AuthURL == "" {
		t.Fatalf("engine.status missing mesh NeedsLogin summary: %+v", engineStatus.Mesh)
	}

	// Reject invalid auth URL on login.
	backend.mu.Lock()
	backend.connectAuth = "https://evil.invalid/a/bad"
	backend.mu.Unlock()
	call(t, e, "mesh.action", map[string]any{"action": "connect"})
	if err := callErr(t, e, "mesh.action", map[string]any{"action": "login"}); err.Code != "invalid_params" {
		t.Fatalf("expected invalid_params for untrusted auth URL, got: %+v", err)
	}

	// Complete login -> connected Running status.
	backend.mu.Lock()
	backend.connectAuth = ""
	backend.mu.Unlock()
	connectedRaw := call(t, e, "mesh.action", map[string]any{"action": "connect"})
	var connectedSnap meshStatusResult
	if err := json.Unmarshal(connectedRaw, &connectedSnap); err != nil {
		t.Fatal(err)
	}
	if !connectedSnap.Status.Running || connectedSnap.Status.Self.IPs[0] != "100.64.0.1" || len(connectedSnap.Status.Peers) != 1 {
		t.Fatalf("unexpected connected snapshot: %+v", connectedSnap)
	}
	// Verify lowerCamelCase keys including visits[0].deviceId and device fields.
	var connectedMap map[string]any
	_ = json.Unmarshal(connectedRaw, &connectedMap)
	visitsRaw := connectedMap["visits"].([]any)
	if len(visitsRaw) != 1 {
		t.Fatalf("expected 1 visit, got: %v", visitsRaw)
	}
	visitObj := visitsRaw[0].(map[string]any)
	if visitObj["deviceId"] != "peer-1" {
		t.Fatalf("visit missing exact deviceId field: %v", visitObj)
	}

	// mesh.services validation: invalid syntax, empty+enabled, and reserved e.port.
	if err := callErr(t, e, "mesh.services", map[string]any{"services": "80-90/tcp", "enabled": true}); err.Code != "invalid_params" {
		t.Fatalf("accepted range in mesh.services: %+v", err)
	}
	if err := callErr(t, e, "mesh.services", map[string]any{"services": "", "enabled": true}); err.Code != "invalid_params" {
		t.Fatalf("accepted empty services with enabled=true: %+v", err)
	}
	if err := callErr(t, e, "mesh.services", map[string]any{"services": fmt.Sprintf("%d/tcp", e.port), "enabled": true}); err.Code != "invalid_params" {
		t.Fatalf("accepted reserved engine port in mesh.services: %+v", err)
	}

	var svcSnap meshStatusResult
	_ = json.Unmarshal(call(t, e, "mesh.services", map[string]any{"services": "8080/tcp, 5555/udp", "enabled": true}), &svcSnap)
	if svcSnap.Status.ShieldsUp || len(svcSnap.Status.Services) != 2 || svcSnap.Status.Services[0].Port != 8080 || svcSnap.Status.Services[1].Protocol != "udp" {
		t.Fatalf("mesh.services failed: %+v", svcSnap.Status)
	}

	// shieldsUp / shieldsDown / allow / block mapping.
	_ = json.Unmarshal(call(t, e, "mesh.action", map[string]any{"action": "shieldsUp"}), &svcSnap)
	if !svcSnap.Status.ShieldsUp {
		t.Fatal("shieldsUp did not enable shields")
	}
	_ = json.Unmarshal(call(t, e, "mesh.action", map[string]any{"action": "shieldsDown"}), &svcSnap)
	if svcSnap.Status.ShieldsUp {
		t.Fatal("shieldsDown did not disable shields")
	}

	// mesh.block & unblock.
	_ = json.Unmarshal(call(t, e, "mesh.block", map[string]any{"id": "peer-1", "blocked": true}), &svcSnap)
	if !slices.Contains(svcSnap.Status.Blocked, "peer-1") {
		t.Fatalf("peer-1 was not blocked: %+v", svcSnap.Status.Blocked)
	}
	_ = json.Unmarshal(call(t, e, "mesh.block", map[string]any{"id": "peer-1", "blocked": false}), &svcSnap)
	if slices.Contains(svcSnap.Status.Blocked, "peer-1") {
		t.Fatalf("peer-1 was not unblocked: %+v", svcSnap.Status.Blocked)
	}

	// mesh.autoConnect & clearHistory & refresh.
	_ = json.Unmarshal(call(t, e, "mesh.autoConnect", map[string]any{"enabled": true}), &svcSnap)
	if !svcSnap.AutoConnect {
		t.Fatal("autoConnect not persisted")
	}
	_ = json.Unmarshal(call(t, e, "mesh.action", map[string]any{"action": "clearHistory"}), &svcSnap)
	if len(svcSnap.Visits) != 0 || !backend.hasCall("clearHistory") {
		t.Fatalf("clearHistory did not clear visits: %+v", svcSnap.Visits)
	}
	_ = json.Unmarshal(call(t, e, "mesh.action", map[string]any{"action": "refresh"}), &svcSnap)
	if !svcSnap.Status.Running {
		t.Fatalf("refresh lost running status: %+v", svcSnap)
	}

	// mesh.probe runs outside e.mu so engine.status is never blocked by a slow probe.
	backend.probeStarted = make(chan struct{})
	backend.probeRelease = make(chan struct{})
	probeDone := make(chan json.RawMessage, 1)
	go func() {
		probeDone <- call(t, e, "mesh.probe", map[string]any{"id": "peer-1", "protocol": "ssh", "port": 22})
	}()
	select {
	case <-backend.probeStarted:
	case <-time.After(time.Second):
		t.Fatal("mesh.probe did not start")
	}
	statusDone := make(chan Status, 1)
	go func() {
		var s Status
		_ = json.Unmarshal(call(t, e, "engine.status", nil), &s)
		statusDone <- s
	}()
	select {
	case s := <-statusDone:
		if s.Mesh == nil || !s.Mesh.Running || s.Mesh.PeerCount != 1 {
			t.Fatalf("unexpected summary during concurrent probe: %+v", s.Mesh)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("engine.status blocked waiting for mesh.probe")
	}
	close(backend.probeRelease)
	var probeRes map[string]any
	select {
	case raw := <-probeDone:
		if err := json.Unmarshal(raw, &probeRes); err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("mesh.probe did not finish")
	}
	if probeRes["deviceId"] != "peer-1" || probeRes["state"] != "connected" || probeRes["port"] != float64(22) {
		t.Fatalf("unexpected probe response: %+v", probeRes)
	}

	// Disconnect.
	_ = json.Unmarshal(call(t, e, "mesh.action", map[string]any{"action": "disconnect"}), &svcSnap)
	if svcSnap.Status.Running || svcSnap.Status.State != "Stopped" {
		t.Fatalf("disconnect did not stop mesh: %+v", svcSnap.Status)
	}
}

func TestMeshOnlyEngineStartAndTunnelPlan(t *testing.T) {
	backend := newFakeMeshBackend()
	e := newMeshTestEngine(t, backend)

	// Without any imported profile and with mesh inactive, engine.start and tunnelPlan fail with no_profile.
	if err := callErr(t, e, "engine.start", nil); err.Code != "no_profile" {
		t.Fatalf("expected no_profile for engine.start when mesh inactive, got: %+v", err)
	}
	if err := callErr(t, e, "engine.tunnelPlan", map[string]any{}); err.Code != "no_profile" {
		t.Fatalf("expected no_profile for engine.tunnelPlan when mesh inactive, got: %+v", err)
	}
	e.mu.Lock()
	if _, err := e.startVPNLocked(context.Background(), -1, nil, nil); asError(err).Code != "invalid_params" {
		e.mu.Unlock()
		t.Fatalf("unexpected error for invalid fd: %v", err)
	}
	if _, err := e.startVPNLocked(context.Background(), 5, func(int) bool { return true }, nil); asError(err).Code != "no_profile" {
		e.mu.Unlock()
		t.Fatalf("expected no_profile for startVPNLocked when mesh inactive, got: %v", err)
	}
	e.mu.Unlock()

	// Connect mesh -> mesh-only start and tunnelPlan succeed without any imported profile.
	call(t, e, "mesh.action", map[string]any{"action": "connect"})

	var plan TunnelPlan
	if err := json.Unmarshal(call(t, e, "engine.tunnelPlan", map[string]any{"appScope": map[string]any{"mode": "all"}}), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Version != TunnelPlanVersion || plan.Token == "" {
		t.Fatalf("unexpected mesh-only tunnel plan: %+v", plan)
	}
	e.mu.Lock()
	if _, err := e.validatePendingPlanLocked(plan.Token); err != nil {
		e.mu.Unlock()
		t.Fatalf("mesh-only tunnel plan failed validation: %v", err)
	}
	e.mu.Unlock()

	// profiles.activate with "" or meshOnlyProfileID must still reject.
	if err := callErr(t, e, "profiles.activate", map[string]any{"id": meshOnlyProfileID}); err.Code != "no_profile" {
		t.Fatalf("profiles.activate accepted reserved meshOnlyProfileID: %+v", err)
	}

	var status Status
	if err := json.Unmarshal(call(t, e, "engine.start", nil), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Running || status.ActiveProfile != "" || status.Mesh == nil || !status.Mesh.AccessReady {
		t.Fatalf("unexpected mesh-only engine.start status: %+v", status)
	}

	// Mode switch invalidates old tunnel plan and keeps mesh-only running.
	call(t, e, "engine.mode", map[string]any{"mode": "direct"})
	e.mu.Lock()
	if _, err := e.validatePendingPlanLocked(plan.Token); asError(err).Code != "stale_plan" {
		e.mu.Unlock()
		t.Fatalf("mode change did not invalidate mesh-only tunnel plan: %v", err)
	}
	e.mu.Unlock()

	call(t, e, "engine.stop", nil)
	_ = json.Unmarshal(call(t, e, "engine.status", nil), &status)
	if status.Running || (status.Mesh != nil && status.Mesh.AccessReady) {
		t.Fatalf("stop did not clear AccessReady: %+v", status)
	}
}

func TestMeshLiveRoutingAcrossModesWithRealTraffic(t *testing.T) {
	directOrigin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "direct-origin")
	}))
	defer directOrigin.Close()
	directAddr := strings.TrimPrefix(directOrigin.URL, "http://")

	meshAddr, meshPassword, meshHits, meshDsts := startMockSSBridge(t, "mesh-bridge", "")
	proxyBridgeAddr, proxyBridgePass, _, _ := startMockSSBridge(t, "proxy-bridge", directAddr)

	_, proxyBridgePort, err := net.SplitHostPort(proxyBridgeAddr)
	if err != nil {
		t.Fatal(err)
	}

	profileYAML := fmt.Sprintf(`mode: rule
log-level: warning
proxies:
  - name: 代理节点
    type: ss
    server: 127.0.0.1
    port: %s
    cipher: chacha20-ietf-poly1305
    password: %s
    udp: true
proxy-groups:
  - name: 出口
    type: select
    proxies: [代理节点, DIRECT]
  - name: GLOBAL
    type: select
    proxies: [代理节点, DIRECT]
rules:
  - DOMAIN-SUFFIX,nonexistent-rule.example,出口
  - MATCH,出口
`, proxyBridgePort, proxyBridgePass)

	backend := newFakeMeshBackend()
	backend.status.Peers = []mesh.Device{{
		ID:     "peer-1",
		Name:   "mac-mini",
		IPs:    []netip.Addr{netip.MustParseAddr("100.64.0.2"), netip.MustParseAddr("fd7a:115c:a1e0::2")},
		Online: true,
	}}
	backend.status.RouteIPs = []netip.Addr{netip.MustParseAddr("100.64.0.2"), netip.MustParseAddr("fd7a:115c:a1e0::2")}
	backend.status.ProxyAddress = meshAddr
	backend.status.ProxyPassword = meshPassword

	e := newMeshTestEngine(t, backend)
	call(t, e, "profiles.import", map[string]any{"name": "ModeTest", "yaml": profileYAML})

	// Connect mesh, then start engine in direct mode from the beginning so we also test stripping initial MATCH,DIRECT.
	call(t, e, "mesh.action", map[string]any{"action": "connect"})
	call(t, e, "engine.mode", map[string]any{"mode": "direct"})
	var status Status
	_ = json.Unmarshal(call(t, e, "engine.start", nil), &status)
	if !status.Running || status.Mode != "direct" || status.Mesh == nil || !status.Mesh.AccessReady {
		t.Fatalf("unexpected initial direct+mesh status: %+v", status)
	}

	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", e.port))
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 4 * time.Second}

	httpGet := func(target string) string {
		t.Helper()
		resp, err := client.Get(target)
		if err != nil {
			t.Fatalf("GET %s failed: %v", target, err)
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	assertRulesOrder := func(want []string) {
		t.Helper()
		rules := tunnel.Rules()
		if len(rules) != len(want) {
			var got []string
			for _, r := range rules {
				got = append(got, r.RuleType().String()+"("+r.Payload()+")->"+r.Adapter())
			}
			t.Fatalf("rules count mismatch: got %v, want %v", got, want)
		}
		for i, w := range want {
			got := rules[i].RuleType().String() + "->" + rules[i].Adapter()
			if got != w {
				t.Fatalf("rule[%d] = %s (%s), want %s", i, got, rules[i].Payload(), w)
			}
		}
	}

	// 1. Direct mode: mesh traffic uses __GOPURR_TAILNET__, non-mesh hits MATCH,DIRECT (NOT 代理节点).
	assertRulesOrder([]string{
		"IPCIDR->" + meshProxyName,
		"IPCIDR->" + meshProxyName,
		"Match->DIRECT",
		"DomainSuffix->出口",
		"Match->出口",
	})
	if tunnel.Rules()[0].Payload() != "100.64.0.2/32" || tunnel.Rules()[1].Payload() != "fd7a:115c:a1e0::2/128" {
		t.Fatalf("unexpected mesh rule payloads: %s, %s", tunnel.Rules()[0].Payload(), tunnel.Rules()[1].Payload())
	}
	if e.core.Mode() != "rule" {
		t.Fatalf("expected tunnel internal mode rule while mesh routes active, got %s", e.core.Mode())
	}
	if body := httpGet("http://100.64.0.2:8080/hello"); body != "mesh-bridge:100.64.0.2:8080" {
		t.Fatalf("direct mode mesh request routed wrong: %q", body)
	}
	// Note: 代理节点 only connects to directOrigin, whereas DIRECT also reaches directOrigin.
	// Differentiate by switching 出口 or checking via proxyBridge vs directOrigin:
	// Wait: startMockSSBridge for proxyBridge forwards to directOrigin; let's verify via proxy hits or dedicated response!
	// Let's also check 100.64.0.2 hits increased and non-mesh hits DIRECT via a dedicated proxy target test below.

	// 2. Switch to "rule" mode: synthetic MATCH,DIRECT MUST be gone; non-mesh uses 出口 -> 代理节点.
	_ = json.Unmarshal(call(t, e, "engine.mode", map[string]any{"mode": "rule"}), &status)
	if status.Mode != "rule" || e.core.Mode() != "rule" {
		t.Fatalf("unexpected rule mode status: %+v / core=%s", status, e.core.Mode())
	}
	assertRulesOrder([]string{
		"IPCIDR->" + meshProxyName,
		"IPCIDR->" + meshProxyName,
		"DomainSuffix->出口",
		"Match->出口",
	})
	if body := httpGet("http://100.64.0.2:8081/hello"); body != "mesh-bridge:100.64.0.2:8081" {
		t.Fatalf("rule mode mesh request routed wrong: %q", body)
	}

	// 3. Switch to "global" mode: mesh IP-CIDR stays at top, MATCH,GLOBAL precedes profile rules.
	_ = json.Unmarshal(call(t, e, "engine.mode", map[string]any{"mode": "global"}), &status)
	if status.Mode != "global" || e.core.Mode() != "rule" {
		t.Fatalf("unexpected global mode status: %+v / core=%s", status, e.core.Mode())
	}
	assertRulesOrder([]string{
		"IPCIDR->" + meshProxyName,
		"IPCIDR->" + meshProxyName,
		"Match->GLOBAL",
		"DomainSuffix->出口",
		"Match->出口",
	})
	if body := httpGet("http://100.64.0.2:8082/hello"); body != "mesh-bridge:100.64.0.2:8082" {
		t.Fatalf("global mode mesh request routed wrong: %q", body)
	}

	// Verify real non-mesh routing differentiation across direct / rule / global using a tagged SS proxy server:
	taggedProxyAddr, taggedProxyPass, taggedHits, _ := startMockSSBridge(t, "tagged-proxy", "")
	_, taggedPort, _ := net.SplitHostPort(taggedProxyAddr)
	taggedProfileYAML := fmt.Sprintf(`mode: rule
log-level: warning
proxies:
  - name: 代理节点
    type: ss
    server: 127.0.0.1
    port: %s
    cipher: chacha20-ietf-poly1305
    password: %s
    udp: true
proxy-groups:
  - name: 出口
    type: select
    proxies: [代理节点, DIRECT]
  - name: GLOBAL
    type: select
    proxies: [代理节点, DIRECT]
rules:
  - DOMAIN-SUFFIX,nonexistent-rule.example,出口
  - MATCH,出口
`, taggedPort, taggedProxyPass)
	var taggedProfile Profile
	_ = json.Unmarshal(call(t, e, "profiles.import", map[string]any{"name": "Tagged", "yaml": taggedProfileYAML}), &taggedProfile)
	call(t, e, "profiles.activate", map[string]any{"id": taggedProfile.ID})

	// Currently mode is "global" with GLOBAL -> 代理节点:
	if body := httpGet(directOrigin.URL); !strings.HasPrefix(body, "tagged-proxy:") {
		t.Fatalf("global mode did not route non-mesh through GLOBAL(代理节点): %q", body)
	}
	if body := httpGet("http://100.64.0.2:8083/hello"); body != "mesh-bridge:100.64.0.2:8083" {
		t.Fatalf("global mode failed mesh route after profile activate: %q", body)
	}
	// Select DIRECT in GLOBAL -> non-mesh obeys GLOBAL(DIRECT) while mesh STILL hits mesh-bridge:
	call(t, e, "proxies.select", map[string]any{"group": "GLOBAL", "member": "DIRECT"})
	if body := httpGet(directOrigin.URL); body != "direct-origin" {
		t.Fatalf("global mode with GLOBAL=DIRECT did not route direct: %q", body)
	}
	if body := httpGet("http://100.64.0.2:8084/hello"); body != "mesh-bridge:100.64.0.2:8084" {
		t.Fatalf("global mode with GLOBAL=DIRECT broke mesh route: %q", body)
	}

	// Switch to "direct": non-mesh goes to directOrigin, NOT tagged-proxy, and mesh STILL hits mesh-bridge!
	beforeTagged := taggedHits.Load()
	call(t, e, "engine.mode", map[string]any{"mode": "direct"})
	if body := httpGet(directOrigin.URL); body != "direct-origin" {
		t.Fatalf("direct mode routed non-mesh through proxy: %q", body)
	}
	if taggedHits.Load() != beforeTagged {
		t.Fatalf("direct mode hit proxy bridge unexpectedly")
	}
	if body := httpGet("http://100.64.0.2:8085/hello"); body != "mesh-bridge:100.64.0.2:8085" {
		t.Fatalf("direct mode failed mesh route: %q", body)
	}

	// Switch to "rule": non-mesh uses 出口(代理节点) -> tagged-proxy, and mesh hits mesh-bridge!
	call(t, e, "engine.mode", map[string]any{"mode": "rule"})
	if body := httpGet(directOrigin.URL); !strings.HasPrefix(body, "tagged-proxy:") {
		t.Fatalf("rule mode did not route non-mesh through 出口(代理节点): %q", body)
	}
	if body := httpGet("http://100.64.0.2:8086/hello"); body != "mesh-bridge:100.64.0.2:8086" {
		t.Fatalf("rule mode failed mesh route: %q", body)
	}

	// Disconnect mesh while in global mode -> restores tunnel.Mode() == "global" & removes __GOPURR_TAILNET__.
	call(t, e, "engine.mode", map[string]any{"mode": "global"})
	call(t, e, "mesh.action", map[string]any{"action": "disconnect"})
	_ = json.Unmarshal(call(t, e, "engine.status", nil), &status)
	if status.Mode != "global" || e.core.Mode() != "global" || (status.Mesh != nil && status.Mesh.AccessReady) {
		t.Fatalf("disconnect in global mode left stale state: %+v / core=%s", status, e.core.Mode())
	}
	if _, exists := tunnel.Proxies()[meshProxyName]; exists {
		t.Fatalf("%s proxy survived mesh disconnect", meshProxyName)
	}
	assertRulesOrder([]string{
		"DomainSuffix->出口",
		"Match->出口",
	})

	// Reconnect mesh live while running in global mode -> restores __GOPURR_TAILNET__ and mesh routing.
	backend.mu.Lock()
	backend.status.Peers = []mesh.Device{{
		ID:     "peer-1",
		Name:   "mac-mini",
		IPs:    []netip.Addr{netip.MustParseAddr("100.64.0.2"), netip.MustParseAddr("fd7a:115c:a1e0::2")},
		Online: true,
	}}
	backend.status.RouteIPs = []netip.Addr{netip.MustParseAddr("100.64.0.2"), netip.MustParseAddr("fd7a:115c:a1e0::2")}
	backend.status.ProxyAddress = meshAddr
	backend.status.ProxyPassword = meshPassword
	backend.mu.Unlock()
	call(t, e, "mesh.action", map[string]any{"action": "connect"})
	assertRulesOrder([]string{
		"IPCIDR->" + meshProxyName,
		"IPCIDR->" + meshProxyName,
		"Match->GLOBAL",
		"DomainSuffix->出口",
		"Match->出口",
	})
	if body := httpGet("http://100.64.0.2:8087/hello"); body != "mesh-bridge:100.64.0.2:8087" {
		t.Fatalf("live reconnect failed mesh route: %q", body)
	}
	if meshHits.Load() < 6 || len(meshDsts()) < 6 {
		t.Fatalf("expected >=6 mesh bridge connections, got hits=%d dsts=%v", meshHits.Load(), meshDsts())
	}
	_ = proxyBridgeAddr
}

func TestMeshOpenAutoStartAndCloseLifecycle(t *testing.T) {
	ownership.Lock()
	prevHome, prevActive := ownership.home, ownership.active
	ownership.home, ownership.active = "", false
	ownership.Unlock()
	t.Cleanup(func() {
		ownership.Lock()
		ownership.home, ownership.active = prevHome, prevActive
		ownership.Unlock()
	})

	// Untouched home: Open creates e.mesh without calling Start().
	untouchedHome := t.TempDir()
	e1, err := Open(Options{Home: untouchedHome})
	if err != nil {
		t.Fatal(err)
	}
	if e1.mesh == nil {
		t.Fatal("Open did not initialize mesh controller")
	}
	if e1.mesh.Snapshot().Started || e1.mesh.Snapshot().Managed {
		t.Fatalf("Open started untouched mesh: %+v", e1.mesh.Snapshot())
	}
	if err := e1.Close(); err != nil {
		t.Fatal(err)
	}

	// Managed home: Open calls e.mesh.Start() automatically, and Close shuts it down.
	managedHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(managedHome, "mesh.json"), []byte(`{"Managed":true,"AutoConnect":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	ownership.Lock()
	ownership.home = ""
	ownership.Unlock()
	e2, err := Open(Options{Home: managedHome})
	if err != nil {
		t.Fatal(err)
	}
	if !e2.mesh.Snapshot().Started || !e2.mesh.Snapshot().Managed {
		t.Fatalf("Open did not auto-start managed mesh: %+v", e2.mesh.Snapshot())
	}
	if err := e2.Close(); err != nil {
		t.Fatal(err)
	}
	ownership.Lock()
	activeAfterClose := ownership.active
	ownership.Unlock()
	if activeAfterClose {
		t.Fatal("Close did not release ownership.active")
	}
}
