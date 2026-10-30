package mesh

import (
	"context"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
)

func shortcutDevice() Device {
	return Device{ID: "peer-A", IPs: []netip.Addr{netip.MustParseAddr("100.70.0.2")}}
}

func TestShortcutTargetsRejectInjectionAndNonTailnetAddresses(t *testing.T) {
	for _, username := range []string{"root;touch", "a\nb", "a@evil.example", "-oProxyCommand", "a\"b", "user:password"} {
		s := Shortcut{Kind: "ssh", Port: 22, User: username}
		if _, err := s.URI(shortcutDevice()); err == nil {
			t.Errorf("unsafe SSH user accepted: %q", username)
		}
		if _, err := s.SSHArguments(shortcutDevice()); err == nil {
			t.Error("unsafe command args")
		}
	}
	for _, raw := range []string{"127.0.0.1", "192.168.1.1", "8.8.8.8", "::1"} {
		d := Device{ID: "peer-A", IPs: []netip.Addr{netip.MustParseAddr(raw)}}
		if _, err := (Shortcut{Kind: "ssh", Port: 22}).URI(d); err == nil {
			t.Errorf("non-tailnet target accepted: %s", raw)
		}
	}
}

func TestShortcutURIsAndRDPSettingsPreserveIdentityChecks(t *testing.T) {
	d := shortcutDevice()
	s := Shortcut{Kind: "ssh", Port: 2222, User: "ubuntu"}
	uri, err := s.URI(d)
	if err != nil || uri != "ssh://ubuntu@100.70.0.2:2222" {
		t.Fatalf("SSH URI: %s %v", uri, err)
	}
	d.IPs = []netip.Addr{netip.MustParseAddr("fd7a:115c:a1e0::2")}
	uri, err = s.URI(d)
	if err != nil || uri != "ssh://ubuntu@[fd7a:115c:a1e0::2]:2222" {
		t.Fatal("IPv6 URI")
	}
	s = DefaultShortcut("windows")
	raw, err := s.RDPFile(d)
	if err != nil || !strings.Contains(string(raw), "authentication level:i:2") || !strings.Contains(string(raw), "prompt for credentials:i:1") || strings.Contains(string(raw), "password") || !strings.Contains(string(raw), "redirectdrives:i:0") {
		t.Fatal("unsafe RDP defaults")
	}
	if DefaultShortcut("darwin").Kind != "vnc" || DefaultShortcut("linux").Kind != "ssh" {
		t.Fatal("platform defaults")
	}
}

func TestProbeDistinguishesProtocolResponseFromHandshakeAndRejection(t *testing.T) {
	for _, scenario := range []string{"ssh", "vnc", "closed", "silent", "other", "stale"} {
		t.Run(scenario, func(t *testing.T) {
			b := NewEmbedded(t.TempDir(), nil)
			n, _ := testNode(t, b)
			d := shortcutDevice()
			n.devices = map[string]Device{d.ID: d}
			n.peers[d.IPs[0]] = true
			dialed := false
			n.dial = func(context.Context, string, string) (net.Conn, error) {
				dialed = true
				client, server := net.Pipe()
				go func() {
					defer server.Close()
					switch scenario {
					case "ssh":
						io.WriteString(server, "SS")
						io.WriteString(server, "H-2.0-test\r\n")
					case "vnc":
						io.WriteString(server, "RFB 003.008\n")
					case "silent":
						io.Copy(io.Discard, server)
					case "other":
						io.WriteString(server, "HTTP/1.1 200 OK\r\n")
					}
				}()
				return client, nil
			}
			kind := "ssh"
			if scenario == "vnc" {
				kind = "vnc"
			}
			id := d.ID
			if scenario == "stale" {
				id = "removed"
			}
			r := b.Probe(context.Background(), id, kind, 22)
			expected := map[string]string{"ssh": "verified", "vnc": "verified", "closed": "failed", "silent": "connected", "other": "connected", "stale": "unknown"}[scenario]
			if r.State != expected {
				t.Fatalf("%s got %+v", scenario, r)
			}
			if scenario == "stale" && dialed {
				t.Fatal("probed unknown device")
			}
		})
	}
}
