package mesh

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/netip"
	"time"

	N "github.com/metacubex/mihomo/common/net"
	"github.com/metacubex/mihomo/transport/shadowsocks/core"
	"github.com/metacubex/mihomo/transport/socks5"
)

// The local bridge authenticates every TCP stream and UDP datagram using
// mihomo's existing AEAD implementation. A UDP port number is never a credential.
// The ephemeral key stays in private runtime configuration, outside backups.
func (b *Embedded) startBridge(n *nodeSession) error {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return err
	}
	n.password = hex.EncodeToString(secret[:])
	cipher, err := core.PickCipher("chacha20-ietf-poly1305", nil, n.password)
	if err != nil {
		return err
	}
	n.cipher = cipher
	for range 8 {
		n.listener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
		n.packet, err = net.ListenPacket("udp", n.listener.Addr().String())
		if err == nil {
			break
		}
		n.listener.Close()
	}
	if err != nil {
		return err
	}
	n.packet = cipher.PacketConn(N.NewEnhancePacketConn(n.packet))
	n.udpFlows = map[string]*bridgeUDPFlow{}
	if n.sockets == nil {
		n.sockets = map[*trackedConn]bool{}
	}
	b.mu.Lock()
	b.node, b.allow = n, false
	b.mu.Unlock()
	go b.serveBridgeTCP(n)
	go b.serveBridgeUDP(n)
	return nil
}

func (b *Embedded) dialPeer(n *nodeSession, ctx context.Context, network, address string) (net.Conn, error) {
	dst, err := netip.ParseAddrPort(address)
	b.mu.Lock()
	allowed := b.node == n && n.peers[dst.Addr()]
	b.mu.Unlock()
	if err != nil || !allowed || dst.Port() == 0 || (network != "tcp" && network != "udp") {
		return nil, errors.New("目标不是已知的组网设备")
	}
	return n.dial(ctx, network, dst.String())
}

func (b *Embedded) serveBridgeTCP(n *nodeSession) {
	l := &trackedListener{Listener: n.listener, backend: b, node: n}
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			stream := n.cipher.StreamConn(c)
			addr, err := socks5.ReadAddr(stream, make([]byte, socks5.MaxAddrLen))
			if err != nil {
				return
			}
			_ = c.SetDeadline(time.Time{})
			ctx, cancel := context.WithTimeout(n.ctx, 5*time.Second)
			peer, err := b.dialPeer(n, ctx, "tcp", addr.String())
			cancel()
			if err != nil {
				return
			}
			defer peer.Close()
			done := make(chan struct{}, 1)
			go func() { relay(peer, stream, false); closeWrite(peer); done <- struct{}{} }()
			relay(stream, peer, false)
			closeWrite(c)
			<-done
		}()
	}
}

type bridgeUDPFlow struct {
	queue       chan []byte
	source      net.Addr
	destination string
}

func (b *Embedded) serveBridgeUDP(n *nodeSession) {
	buf := make([]byte, 65535)
	for {
		size, source, err := n.packet.ReadFrom(buf)
		if err != nil {
			if n.ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			continue // unauthenticated or malformed datagrams are discarded
		}
		addr := socks5.SplitAddr(buf[:size])
		if addr == nil {
			continue
		}
		dst, err := netip.ParseAddrPort(addr.String())
		if err != nil || dst.Port() == 0 {
			continue
		}
		key := source.String() + "/" + dst.String()
		b.mu.Lock()
		if b.node != n || !n.peers[dst.Addr()] {
			b.mu.Unlock()
			continue
		}
		f := n.udpFlows[key]
		if f == nil {
			if len(n.udpFlows) >= 128 {
				b.mu.Unlock()
				continue
			}
			f = &bridgeUDPFlow{queue: make(chan []byte, 16), source: source, destination: dst.String()}
			n.udpFlows[key] = f
			go b.runBridgeUDPFlow(n, key, f)
		}
		b.mu.Unlock()
		payload := append([]byte(nil), buf[len(addr):size]...)
		select {
		case f.queue <- payload:
		default:
		}
	}
}

func (b *Embedded) runBridgeUDPFlow(n *nodeSession, key string, f *bridgeUDPFlow) {
	defer func() { b.mu.Lock(); delete(n.udpFlows, key); b.mu.Unlock() }()
	dialCtx, cancelDial := context.WithTimeout(n.ctx, 5*time.Second)
	peer, err := b.dialPeer(n, dialCtx, "udp", f.destination)
	cancelDial()
	if err != nil {
		return
	}
	defer peer.Close()
	ctx, cancel := context.WithCancel(n.ctx)
	defer cancel()
	go func() {
		defer cancel()
		buf := make([]byte, 65000)
		for {
			_ = peer.SetReadDeadline(time.Now().Add(time.Minute))
			size, err := peer.Read(buf)
			if err != nil {
				return
			}
			packet := append(socks5.ParseAddr(f.destination), buf[:size]...)
			if _, err := n.packet.WriteTo(packet, f.source); err != nil {
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-f.queue:
			_ = peer.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err := peer.Write(data); err != nil {
				return
			}
		}
	}
}
