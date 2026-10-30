package mesh

import (
	"context"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/mihomo/adapter/outbound"
	C "github.com/metacubex/mihomo/constant"
)

// Exercise the real mihomo outbound against the packaged bridge. Only the
// encrypted tailnet transport is replaced with a local echo service.
func TestMihomoBridgeAuthenticatesTCPAndUDPAndRestrictsDestinations(t *testing.T) {
	for _, protocol := range []string{"tcp", "udp"} {
		for _, scenario := range []string{"valid", "wrong-key", "unknown-peer"} {
			t.Run(protocol+"/"+scenario, func(t *testing.T) {
				b := NewEmbedded(t.TempDir(), nil)
				ctx, cancel := context.WithCancel(context.Background())
				n := &nodeSession{ctx: ctx, cancel: cancel, peers: map[netip.Addr]bool{netip.MustParseAddr("100.70.0.2"): true}}
				var dialed atomic.Int32
				var serviceAddress string
				if protocol == "tcp" {
					ln, err := net.Listen("tcp", "127.0.0.1:0")
					if err != nil {
						t.Fatal(err)
					}
					defer ln.Close()
					serviceAddress = ln.Addr().String()
					go func() {
						for {
							c, err := ln.Accept()
							if err != nil {
								return
							}
							go func() { defer c.Close(); io.Copy(c, c) }()
						}
					}()
				} else {
					pc, err := net.ListenPacket("udp", "127.0.0.1:0")
					if err != nil {
						t.Fatal(err)
					}
					defer pc.Close()
					serviceAddress = pc.LocalAddr().String()
					go func() {
						buf := make([]byte, 65535)
						for {
							size, addr, err := pc.ReadFrom(buf)
							if err != nil {
								return
							}
							pc.WriteTo(buf[:size], addr)
						}
					}()
				}
				n.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
					if network != protocol || addr != "100.70.0.2:22" {
						t.Errorf("unexpected tailnet dial %s %s", network, addr)
					}
					dialed.Add(1)
					return (&net.Dialer{}).DialContext(ctx, network, serviceAddress)
				}
				if err := b.startBridge(n); err != nil {
					t.Fatal(err)
				}
				defer b.Disconnect(context.Background())
				password := n.password
				if scenario == "wrong-key" {
					password = strings.Repeat("f", 64)
				}
				adapter, err := outbound.NewShadowSocks(outbound.ShadowSocksOption{Name: "mesh-test", Server: "127.0.0.1", Port: n.listener.Addr().(*net.TCPAddr).Port, Cipher: "chacha20-ietf-poly1305", Password: password, UDP: true})
				if err != nil {
					t.Fatal(err)
				}
				defer adapter.Close()
				ip := netip.MustParseAddr("100.70.0.2")
				if scenario == "unknown-peer" {
					ip = netip.MustParseAddr("100.70.0.99")
				}
				metadata := &C.Metadata{NetWork: C.TCP, Type: C.INNER, DstIP: ip, DstPort: 22}
				var data []byte
				if protocol == "tcp" {
					conn, err := adapter.DialContext(ctx, metadata)
					if err != nil {
						t.Fatal(err)
					}
					defer conn.Close()
					conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
					_, err = conn.Write([]byte("hello"))
					if err == nil {
						data = make([]byte, 5)
						_, err = io.ReadFull(conn, data)
					}
					if scenario == "valid" && err != nil {
						t.Fatal(err)
					}
					if scenario != "valid" && err == nil {
						t.Fatal("unauthorized stream forwarded")
					}
				} else {
					metadata.NetWork = C.UDP
					conn, err := adapter.ListenPacketContext(ctx, metadata)
					if err != nil {
						t.Fatal(err)
					}
					defer conn.Close()
					conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
					if _, err := conn.WriteTo([]byte("hello"), net.UDPAddrFromAddrPort(netip.AddrPortFrom(ip, 22))); err != nil {
						t.Fatal(err)
					}
					buf := make([]byte, 1500)
					size, _, err := conn.ReadFrom(buf)
					data = buf[:size]
					if scenario == "valid" && err != nil {
						t.Fatal(err)
					}
					if scenario != "valid" && err == nil {
						t.Fatal("unauthenticated datagram forwarded")
					}
				}
				if scenario == "valid" {
					if string(data) != "hello" || dialed.Load() != 1 {
						t.Fatalf("bridge failed: %q (%d dials)", data, dialed.Load())
					}
				} else if dialed.Load() != 0 {
					t.Fatal("rejected request reached tailnet dialer")
				}
			})
		}
	}
}
