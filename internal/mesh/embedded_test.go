package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/tailscale/client/local"
	"github.com/metacubex/tailscale/ipn"
)

func testNode(t *testing.T, b *Embedded) (*nodeSession, *atomic.Bool) {
	t.Helper()
	unknown := new(atomic.Bool)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/localapi/v0/status":
			io.WriteString(w, `{"BackendState":"Running"}`)
		case "/localapi/v0/whois":
			if unknown.Load() {
				http.Error(w, "unknown", 404)
				return
			}
			if r.URL.Query().Get("addr") != "100.70.0.2:51234" {
				t.Errorf("wrong caller: %s", r.URL.RawQuery)
			}
			io.WriteString(w, `{"Node":{"StableID":"peer-A","Name":"workstation.example.ts.net.","Addresses":["100.70.0.2/32"]},"UserProfile":{"DisplayName":"Alice","LoginName":"alice@example.com"}}`)
		case "/localapi/v0/prefs":
			var p ipn.MaskedPrefs
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				t.Error(err)
			}
			if !p.CorpDNSSet || p.CorpDNS || !p.RouteAllSet || p.RouteAll || p.RunSSH || p.RunWebClient || p.RemoteConfig {
				t.Error("unsafe prefs")
			}
			json.NewEncoder(w).Encode(p.Prefs)
		default:
			http.Error(w, "unexpected API", 404)
		}
	}))
	t.Cleanup(api.Close)
	addr := strings.TrimPrefix(api.URL, "http://")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	n := &nodeSession{ctx: ctx, cancel: cancel, self: map[netip.Addr]bool{netip.MustParseAddr("100.70.0.1"): true}, peers: map[netip.Addr]bool{},
		lc: &local.Client{Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", addr)
		}}}
	b.node = n
	return n, unknown
}

func TestEmbeddedReadyWithoutClientAndDoesNotStartNetworkOnRead(t *testing.T) {
	b := NewEmbedded(t.TempDir(), nil)
	s, err := b.Read(context.Background())
	if err != nil || !s.Installed || !s.Official || !s.ShieldsKnown || !s.ShieldsUp || b.node != nil || s.Running {
		t.Fatalf("embedded initial state: %+v %v", s, err)
	}
	if err := b.SetIncoming(context.Background(), true); err == nil {
		t.Fatal("opened without connection/ports")
	}
}

func TestExplicitServicesPersistButOpenAccessNeverSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	b := NewEmbedded(dir, func(p uint16) bool { return p == 7890 })
	if err := b.SetServices(context.Background(), "22/tcp, 3389/tcp, 5353/udp"); err != nil {
		t.Fatal(err)
	}
	if err := b.SetDeviceBlocked(context.Background(), "peer-A", true); err != nil {
		t.Fatal(err)
	}
	b.allow = true
	next := NewEmbedded(dir, nil)
	s, err := next.Read(context.Background())
	if err != nil || !s.ShieldsUp || len(s.Services) != 3 || len(s.Blocked) != 1 {
		t.Fatalf("restart policy: %+v %v", s, err)
	}
	info, _ := os.Stat(filepath.Join(dir, "access.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("public policy file")
	}
	if err := b.SetServices(context.Background(), "7890"); err == nil {
		t.Fatal("exposed own proxy")
	}
}

func TestPrivateIdentityStorePreservesKeysAndRejectsLinksAndCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	s, err := newPrivateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteState("machine", []byte("private-key-test")); err != nil {
		t.Fatal(err)
	}
	value, _ := s.ReadState("machine")
	value[0] = 'x'
	value, _ = s.ReadState("machine")
	if string(value) != "private-key-test" {
		t.Fatal("mutable key leaked")
	}
	next, err := newPrivateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	value, _ = next.ReadState("machine")
	if string(value) != "private-key-test" {
		t.Fatal("identity not persisted")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("public identity file")
	}
	link := filepath.Join(filepath.Dir(path), "link")
	if os.Symlink(path, link) == nil {
		if _, err := newPrivateStore(link); err == nil {
			t.Fatal("followed symlink")
		}
	}
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newPrivateStore(path); err == nil {
		t.Fatal("silently reset identity")
	}
}

func TestServicesRejectWildcardsRangesAndUnexpectedProtocols(t *testing.T) {
	for _, raw := range []string{"*", "0", "65536", "22-80", "22/sctp", "22/tcp/udp", strings.Repeat("22,", 1000) + "bad"} {
		if _, err := ParseServices(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	s, err := ParseServices("22, 22/tcp, 53/udp")
	if err != nil || len(s) != 2 || FormatServices(s) != "22/tcp, 53/udp" {
		t.Fatal("service normalization")
	}
}

func TestAuthenticatedTCPForwardingIsRecordedAndRevocationClosesExistingStreams(t *testing.T) {
	for _, revoke := range []string{"all", "device"} {
		t.Run(revoke, func(t *testing.T) {
			b := NewEmbedded(t.TempDir(), nil)
			n, _ := testNode(t, b)
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			port := uint16(ln.Addr().(*net.TCPAddr).Port)
			b.policy.Services = []Service{{Protocol: "tcp", Port: port}}
			if err := b.SetIncoming(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			go func() {
				c, err := ln.Accept()
				if err == nil {
					defer c.Close()
					io.Copy(c, c)
				}
			}()
			remote, client := net.Pipe()
			defer client.Close()
			done := make(chan struct{})
			go func() {
				b.forward(n, "tcp", netip.MustParseAddrPort("100.70.0.2:51234"), netip.AddrPortFrom(netip.MustParseAddr("100.70.0.1"), port), remote)
				close(done)
			}()
			client.SetDeadline(time.Now().Add(3 * time.Second))
			if _, err := client.Write([]byte("hello")); err != nil {
				t.Fatal(err)
			}
			data := make([]byte, 5)
			if _, err := io.ReadFull(client, data); err != nil || string(data) != "hello" {
				t.Fatalf("relay: %s %v", data, err)
			}
			obs, _ := b.Accesses(context.Background())
			if len(obs.Visits) != 1 || !obs.Visits[0].Active || obs.Visits[0].DeviceID != "peer-A" || obs.Visits[0].Login != "alice@example.com" {
				t.Fatalf("identity missing: %+v", obs)
			}
			if revoke == "all" {
				b.BlockNow()
			} else {
				b.BlockDeviceNow("peer-A")
			}
			if _, err := client.Read(data); err == nil {
				t.Fatal("existing connection survived revocation")
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("forwarder did not stop")
			}
			obs, _ = b.Accesses(context.Background())
			if len(obs.Visits) != 1 || obs.Visits[0].Active {
				t.Fatal("short connection not retained")
			}
			if revoke == "device" && !b.allow {
				t.Fatal("device ban disabled all peers")
			}
		})
	}
}

func TestIncomingUnknownIdentityPortsProtocolAndDestinationFailClosed(t *testing.T) {
	for _, scenario := range []string{"unknown", "closed", "port", "protocol", "destination", "blocked", "reserved", "stale"} {
		t.Run(scenario, func(t *testing.T) {
			b := NewEmbedded(t.TempDir(), nil)
			n, unknown := testNode(t, b)
			b.allow = true
			b.policy.Services = []Service{{Protocol: "tcp", Port: 22}}
			dst := netip.MustParseAddrPort("100.70.0.1:22")
			protocol := "tcp"
			switch scenario {
			case "unknown":
				unknown.Store(true)
			case "closed":
				b.allow = false
			case "port":
				dst = netip.MustParseAddrPort("100.70.0.1:23")
			case "protocol":
				protocol = "udp"
			case "destination":
				dst = netip.MustParseAddrPort("192.168.1.1:22")
			case "blocked":
				b.policy.Blocked = []string{"peer-A"}
			case "reserved":
				b.reserved = func(uint16) bool { return true }
			case "stale":
				b.node = nil
			}
			remote, client := net.Pipe()
			defer client.Close()
			go b.forward(n, protocol, netip.MustParseAddrPort("100.70.0.2:51234"), dst, remote)
			client.SetReadDeadline(time.Now().Add(time.Second))
			if _, err := client.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
				t.Fatalf("did not reject: %v", err)
			}
			obs, _ := b.Accesses(context.Background())
			if len(obs.Visits) != 0 {
				t.Fatal("rejected access reported as established")
			}
		})
	}
}

func TestConnectRejectsAmbientEnrollmentSecretBeforeStarting(t *testing.T) {
	t.Setenv("TS_AUTHKEY", "do-not-use-this-key")
	b := NewEmbedded(t.TempDir(), nil)
	_, err := b.Connect(context.Background())
	if err == nil || b.node != nil || strings.Contains(err.Error(), "do-not-use-this-key") {
		t.Fatal("ambient enrollment accepted or leaked")
	}
}

func TestOfficialLoginURLsAndUntrustedNames(t *testing.T) {
	for _, raw := range []string{"http://login.tailscale.com/a/x", "https://login.tailscale.com.evil/a/x", "https://user@login.tailscale.com/a/x", "https://login.tailscale.com:8443/a/x", "file:///x"} {
		if ValidAuthURL(raw) {
			t.Error("unsafe auth URL", raw)
		}
	}
	if !ValidAuthURL("https://login.tailscale.com/a/example") {
		t.Fatal("official auth rejected")
	}
	if got := clean("A\u202e\nB"); got != "AB" {
		t.Fatal("untrusted display controls")
	}
}

func TestSynchronousRevocationSurvivesPolicyStorageFailure(t *testing.T) {
	b := NewEmbedded(t.TempDir(), nil)
	n, _ := testNode(t, b)
	b.allow = true
	if err := os.RemoveAll(b.dir); err != nil {
		t.Fatal(err)
	}
	if err := b.SetDeviceBlocked(context.Background(), "peer-A", true); err == nil {
		t.Fatal("ignored failed persistence")
	}
	if b.permittedLocked(n, "tcp", netip.MustParseAddrPort("100.70.0.2:51234"), netip.MustParseAddrPort("100.70.0.1:22"), "peer-A") {
		t.Fatal("storage failure defeated local ban")
	}
	if err := b.SetIncoming(context.Background(), false); err != nil || b.allow {
		t.Fatal("could not close without storage")
	}
}

type packetPeerConn struct {
	*net.UDPConn
	peer *net.UDPAddr
}

func (c *packetPeerConn) Write(p []byte) (int, error) { return c.WriteToUDP(p, c.peer) }

func TestUDPForwardingPreservesPacketsAndStopsOnRevocation(t *testing.T) {
	b := NewEmbedded(t.TempDir(), nil)
	n, _ := testNode(t, b)
	service, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	remoteSocket, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer remoteSocket.Close()
	client, err := net.DialUDP("udp", nil, remoteSocket.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	port := uint16(service.LocalAddr().(*net.UDPAddr).Port)
	b.policy.Services = []Service{{Protocol: "udp", Port: port}}
	if err := b.SetIncoming(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		b.forward(n, "udp", netip.MustParseAddrPort("100.70.0.2:51234"), netip.AddrPortFrom(netip.MustParseAddr("100.70.0.1"), port), &packetPeerConn{remoteSocket, client.LocalAddr().(*net.UDPAddr)})
		close(done)
	}()
	packets := []string{"first", strings.Repeat("x", 1400), "third"}
	go func() {
		buf := make([]byte, 65535)
		for range packets {
			n, addr, err := service.ReadFromUDP(buf)
			if err != nil {
				return
			}
			service.WriteToUDP(buf[:n], addr)
		}
	}()
	for _, packet := range packets {
		client.SetDeadline(time.Now().Add(3 * time.Second))
		if _, err := client.Write([]byte(packet)); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 65535)
		n, err := client.Read(buf)
		if err != nil || string(buf[:n]) != packet {
			t.Fatalf("UDP packet changed: %d %v", n, err)
		}
	}
	b.BlockNow()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("UDP relay survived revocation")
	}
	obs, _ := b.Accesses(context.Background())
	if len(obs.Visits) != 1 || obs.Visits[0].Protocol != "udp" || obs.Visits[0].Active {
		t.Fatal("UDP access audit incorrect")
	}
}
