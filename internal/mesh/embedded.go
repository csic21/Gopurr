package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/metacubex/mihomo/transport/shadowsocks/core"
	"github.com/metacubex/tailscale/client/local"
	"github.com/metacubex/tailscale/envknob"
	"github.com/metacubex/tailscale/ipn"
	"github.com/metacubex/tailscale/tsnet"
	"github.com/metacubex/tailscale/types/nettype"
	"gopurr/internal/securefs"
)

const controlURL = "https://controlplane.tailscale.com"

type accessPolicy struct {
	Services []Service
	Blocked  []string
}

// Embedded owns its node, authentication state, access gate and connections.
// It never discovers or executes an installed Tailscale application.
type Embedded struct {
	mu      sync.Mutex
	dir     string
	initErr error
	policy  accessPolicy
	allow   bool // never persisted; false before any network activity
	node    *nodeSession
	visits  []Visit
	flows   map[*accessFlow]bool
	pending chan struct{}
	// Gopurr's own proxy/API listeners may never be exposed as remote services.
	reserved func(uint16) bool
}

type nodeSession struct {
	server   *tsnet.Server
	lc       *local.Client
	ctx      context.Context
	cancel   context.CancelFunc
	listener net.Listener
	password string
	peers    map[netip.Addr]bool
	devices  map[string]Device
	self     map[netip.Addr]bool
	sockets  map[*trackedConn]bool
	packet   net.PacketConn
	cipher   core.Cipher
	udpFlows map[string]*bridgeUDPFlow
	dial     func(context.Context, string, string) (net.Conn, error)
}

type accessFlow struct {
	node          *nodeSession
	visit         Visit
	remote, local net.Conn
	cancel        context.CancelFunc
}

func NewEmbedded(dir string, reserved func(uint16) bool) *Embedded {
	b := &Embedded{dir: dir, flows: map[*accessFlow]bool{}, pending: make(chan struct{}, 128), reserved: reserved}
	b.initErr = securefs.PrivateDir(dir)
	if b.initErr != nil {
		return b
	}
	path := filepath.Join(dir, "access.json")
	if err := securefs.PrivateFile(path); err == nil {
		info, statErr := os.Stat(path)
		if statErr != nil || info.Size() > 65536 {
			b.initErr = errors.New("组网访问设置无法读取")
			return b
		}
		raw, err := os.ReadFile(path)
		if err != nil || json.Unmarshal(raw, &b.policy) != nil {
			b.initErr = errors.New("组网访问设置损坏，访问保持关闭")
		}
		validated, err := ParseServices(FormatServices(b.policy.Services))
		if err != nil || len(b.policy.Blocked) > 1000 {
			b.initErr = errors.New("组网访问设置无效，访问保持关闭")
		}
		b.policy.Services = validated
	} else if !errors.Is(err, os.ErrNotExist) {
		b.initErr = err
	}
	return b
}

func (b *Embedded) Read(ctx context.Context) (Status, error) {
	b.mu.Lock()
	n := b.node
	s := Status{Installed: true, Official: true, State: "Stopped", ShieldsKnown: true, ShieldsUp: !b.allow,
		Services: slices.Clone(b.policy.Services), Blocked: slices.Clone(b.policy.Blocked)}
	initErr := b.initErr
	b.mu.Unlock()
	if initErr != nil {
		return s, initErr
	}
	if n == nil {
		return s, nil
	}
	rawStatus, err := n.lc.Status(ctx)
	if err != nil {
		return s, errors.New("无法读取内置组网内核状态")
	}
	encoded, _ := json.Marshal(rawStatus)
	var raw wireStatus
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return s, err
	}
	s.State, s.Version, s.Running = raw.BackendState, clean(raw.Version), raw.BackendState == "Running"
	s.Self, s.Health = device(raw.Self, raw.User), raw.Health
	if raw.CurrentTailnet != nil {
		s.Tailnet = clean(raw.CurrentTailnet.Name)
	}
	if ValidAuthURL(raw.AuthURL) {
		s.AuthURL = raw.AuthURL
	}
	peers, self := map[netip.Addr]bool{}, map[netip.Addr]bool{}
	devices := map[string]Device{}
	for _, ip := range s.Self.IPs {
		self[ip] = true
	}
	for _, p := range raw.Peer {
		if p == nil {
			continue
		}
		d := device(p, raw.User)
		devices[d.ID] = d
		s.Peers = append(s.Peers, d)
		for _, ip := range d.IPs {
			peers[ip] = true
		}
	}
	sort.Slice(s.Peers, func(i, j int) bool {
		if s.Peers[i].Online != s.Peers[j].Online {
			return s.Peers[i].Online
		}
		return s.Peers[i].Name < s.Peers[j].Name
	})
	b.mu.Lock()
	if b.node == n {
		n.peers, n.self, n.devices = peers, self, devices
		s.ShieldsUp = !b.allow
		if s.Running && n.listener != nil {
			s.ProxyAddress, s.ProxyPassword = n.listener.Addr().String(), n.password
		}
	}
	b.mu.Unlock()
	return s, nil
}

func (b *Embedded) Connect(ctx context.Context) (auth string, retErr error) {
	if err := b.Disconnect(ctx); err != nil {
		return "", err
	}
	b.mu.Lock()
	initErr := b.initErr
	b.mu.Unlock()
	if initErr != nil {
		return "", initErr
	}
	// Do not let ambient CLI credentials or debug flags enroll another identity,
	// select another control server, enable public serving or bypass safeguards.
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		if value != "" && (strings.HasPrefix(key, "TS_") || strings.HasPrefix(key, "TSNET_")) && key != "TS_NO_LOGS_NO_SUPPORT" {
			return "", fmt.Errorf("请清除环境变量 %s 后连接内置组网", clean(key))
		}
	}
	store, err := newPrivateStore(filepath.Join(b.dir, "identity.json"))
	if err != nil {
		return "", err
	}
	envknob.SetNoLogsNoSupport()
	host, _ := os.Hostname()
	nctx, cancel := context.WithCancel(context.Background())
	n := &nodeSession{ctx: nctx, cancel: cancel, peers: map[netip.Addr]bool{}, self: map[netip.Addr]bool{}, sockets: map[*trackedConn]bool{}}
	n.server = &tsnet.Server{Dir: b.dir, Store: store, Hostname: clean(host) + "-gopurr", ControlURL: controlURL,
		// This userspace node has no system Tailscale interface to bypass.
		// Follow the system's working network (including Gopurr TUN) instead
		// of tsnet's default sockets bound to the physical interface.
		SystemDialer:         (&net.Dialer{}).DialContext,
		SystemPacketListener: (&net.ListenConfig{}).ListenPacket,
		LookupHook: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
		Logf: func(string, ...any) {}, UserLogf: func(string, ...any) {}}
	n.server.RegisterFallbackTCPHandler(func(src, dst netip.AddrPort) (func(net.Conn), bool) {
		return func(c net.Conn) { b.forward(n, "tcp", src, dst, c) }, true
	})
	n.server.RegisterFallbackUDPHandler(func(src, dst netip.AddrPort) (func(nettype.ConnPacketConn), bool) {
		return func(c nettype.ConnPacketConn) { b.forward(n, "udp", src, dst, c) }, true
	})
	defer func() {
		if retErr != nil {
			cancel()
			_ = n.server.Close()
		}
	}()
	lc, err := n.server.LocalClient()
	if err != nil {
		return "", fmt.Errorf("内置组网内核启动失败：%w", err)
	}
	n.lc = lc
	if _, err := lc.EditPrefs(ctx, safePrefs(true, true)); err != nil {
		return "", fmt.Errorf("内置组网防护初始化失败：%w", err)
	}
	if err := waitForLogin(ctx, lc); err != nil {
		return "", err
	}
	n.dial = n.server.Dial
	if err := b.startBridge(n); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		_ = b.Disconnect(context.Background())
		return "", err
	}
	s, err := b.Read(ctx)
	if err != nil {
		_ = b.Disconnect(context.Background())
		return "", err
	}
	return s.AuthURL, nil
}

func safePrefs(running, shields bool) *ipn.MaskedPrefs {
	return &ipn.MaskedPrefs{Prefs: ipn.Prefs{ControlURL: controlURL, WantRunning: running, ShieldsUp: shields},
		ControlURLSet: true, WantRunningSet: true, ShieldsUpSet: true, CorpDNSSet: true, RouteAllSet: true,
		RunSSHSet: true, RunWebClientSet: true, RemoteConfigSet: true, ExitNodeIDSet: true, ExitNodeIPSet: true,
		AdvertiseRoutesSet: true, AdvertiseTagsSet: true, DriveSharesSet: true}
}

func (b *Embedded) SetIncoming(ctx context.Context, allow bool) error {
	b.mu.Lock()
	n := b.node
	if !allow {
		b.blockLocked("")
	}
	if allow {
		b.blockLocked("")
		if n == nil || len(b.policy.Services) == 0 || b.initErr != nil {
			b.mu.Unlock()
			return errors.New("请先连接组网并明确授权本机服务端口")
		}
	}
	b.mu.Unlock()
	if n == nil {
		return nil
	}
	if allow {
		status, err := n.lc.Status(ctx)
		if err != nil || status.BackendState != "Running" {
			return errors.New("组网尚未连接，访问保持关闭")
		}
	}
	prefs, err := n.lc.EditPrefs(ctx, safePrefs(true, !allow))
	if err != nil || prefs.ShieldsUp != !allow {
		return errors.New("内置组网设置未确认，访问保持关闭")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.node != n {
		return errors.New("组网已断开")
	}
	b.allow = allow
	return nil
}

// These gates are synchronous so a slow control operation cannot delay revocation.
func (b *Embedded) BlockNow() { b.mu.Lock(); b.blockLocked(""); b.mu.Unlock() }
func (b *Embedded) BlockDeviceNow(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !slices.Contains(b.policy.Blocked, id) {
		b.policy.Blocked = append(b.policy.Blocked, id)
	}
	b.blockLocked(id)
}

func (b *Embedded) blockLocked(deviceID string) {
	if deviceID == "" {
		b.allow = false
	}
	for f := range b.flows {
		if deviceID == "" || f.visit.DeviceID == deviceID {
			if f.cancel != nil {
				f.cancel()
			}
			f.remote.Close()
			if f.local != nil {
				f.local.Close()
			}
		}
	}
}

func (b *Embedded) Disconnect(context.Context) error {
	b.mu.Lock()
	n := b.node
	b.blockLocked("")
	b.node = nil
	if n != nil {
		n.cancel()
		if n.listener != nil {
			n.listener.Close()
		}
		if n.packet != nil {
			n.packet.Close()
		}
		for c := range n.sockets {
			c.Conn.Close()
		}
	}
	b.mu.Unlock()
	if n != nil && n.server != nil {
		return n.server.Close()
	}
	return nil
}

func (b *Embedded) SetServices(ctx context.Context, raw string) error {
	services, err := ParseServices(raw)
	if err != nil {
		return err
	}
	if err := b.SetIncoming(ctx, false); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range services {
		if b.reservedLocked(s.Port) {
			return errors.New("不能开放 Gopurr 自身的代理或控制端口")
		}
	}
	next := b.policy
	next.Services = services
	return b.savePolicyLocked(next)
}

func (b *Embedded) SetDeviceBlocked(_ context.Context, id string, blocked bool) error {
	if id == "" || len(id) > 256 {
		return errors.New("无效的设备标识")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	next := b.policy
	next.Blocked = slices.Clone(next.Blocked)
	if blocked {
		b.blockLocked(id)
		if !slices.Contains(next.Blocked, id) {
			next.Blocked = append(next.Blocked, id)
		}
	} else {
		next.Blocked = slices.DeleteFunc(next.Blocked, func(s string) bool { return s == id })
	}
	if err := b.savePolicyLocked(next); err != nil {
		if blocked {
			b.policy.Blocked = next.Blocked
		} // fail closed even if storage is unavailable
		return err
	}
	return nil
}

func (b *Embedded) savePolicyLocked(next accessPolicy) error {
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err := securefs.WriteFile(filepath.Join(b.dir, "access.json"), raw, 0600); err != nil {
		return errors.New("组网访问设置保存失败")
	}
	b.policy = next
	b.initErr = nil
	return nil
}

func (b *Embedded) Accesses(context.Context) (Observation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	visits := slices.Clone(b.visits)
	for f := range b.flows {
		v := f.visit
		v.Active = true
		v.LastSeen = time.Now()
		visits = append([]Visit{v}, visits...)
	}
	sort.Slice(visits, func(i, j int) bool {
		if visits[i].Active != visits[j].Active {
			return visits[i].Active
		}
		if visits[i].Active {
			return visits[i].FirstSeen.After(visits[j].FirstSeen)
		}
		return visits[i].LastSeen.After(visits[j].LastSeen)
	})
	if len(visits) > 100 {
		visits = visits[:100]
	}
	return Observation{Visits: visits, Scope: "内置组网实际接收的 TCP / UDP 服务连接；账号来自加密网络的设备身份"}, nil
}

func (b *Embedded) ClearHistory() {
	b.mu.Lock()
	b.visits = nil
	b.mu.Unlock()
}

func (b *Embedded) permittedLocked(n *nodeSession, protocol string, src, dst netip.AddrPort, id string) bool {
	return b.node == n && b.allow && n.self[dst.Addr()] && id != "" && !slices.Contains(b.policy.Blocked, id) &&
		slices.Contains(b.policy.Services, Service{Protocol: protocol, Port: dst.Port()}) && !b.reservedLocked(dst.Port())
}

func (b *Embedded) reservedLocked(port uint16) bool {
	if b.reserved != nil && b.reserved(port) {
		return true
	}
	if b.node != nil && b.node.listener != nil {
		if addr, ok := b.node.listener.Addr().(*net.TCPAddr); ok && addr.Port == int(port) {
			return true
		}
	}
	return false
}

func (b *Embedded) forward(n *nodeSession, protocol string, src, dst netip.AddrPort, remote net.Conn) {
	defer remote.Close()
	select {
	case b.pending <- struct{}{}:
		defer func() { <-b.pending }()
	default:
		return
	}
	b.mu.Lock()
	eligible := b.node == n && b.allow && n.self[dst.Addr()] && len(b.flows) < 64 &&
		slices.Contains(b.policy.Services, Service{Protocol: protocol, Port: dst.Port()}) && !b.reservedLocked(dst.Port())
	b.mu.Unlock()
	if !eligible {
		return
	}
	ctx, cancel := context.WithTimeout(n.ctx, 3*time.Second)
	defer cancel()
	who, err := n.lc.WhoIs(ctx, src.String())
	if err != nil || who.Node == nil || who.UserProfile == nil || (who.UserProfile.LoginName == "" && len(who.Node.Tags) == 0) {
		return
	}
	id := string(who.Node.StableID)
	f := &accessFlow{node: n, remote: remote, cancel: cancel, visit: Visit{DeviceID: id, Device: clean(strings.TrimSuffix(who.Node.Name, ".")),
		Owner: clean(who.UserProfile.DisplayName), Login: clean(who.UserProfile.LoginName), Protocol: protocol,
		Remote: src.String(), Local: dst.String(), FirstSeen: time.Now(), LastSeen: time.Now()}}
	if len(who.Node.Tags) > 0 {
		f.visit.Owner, f.visit.Login = "带标签的设备", ""
	}
	f.visit.Key = fmt.Sprintf("%s/%s/%s/%d", protocol, src, dst, f.visit.FirstSeen.UnixNano())
	b.mu.Lock()
	if !b.permittedLocked(n, protocol, src, dst, id) || len(b.flows) >= 64 {
		b.mu.Unlock()
		return
	}
	b.flows[f] = true
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.flows, f)
		f.visit.LastSeen = time.Now()
		b.visits = append([]Visit{f.visit}, b.visits...)
		if len(b.visits) > 100 {
			b.visits = b.visits[:100]
		}
	}()
	localConn, err := (&net.Dialer{}).DialContext(ctx, protocol, net.JoinHostPort("127.0.0.1", fmt.Sprint(dst.Port())))
	if err != nil {
		return
	}
	defer localConn.Close()
	b.mu.Lock()
	f.local = localConn
	allowed := b.permittedLocked(n, protocol, src, dst, id)
	b.mu.Unlock()
	if !allowed {
		return
	}
	// UDP flows expire after 60 seconds of inactivity; packet boundaries are
	// preserved by one Read / one Write rather than io.Copy's stream fast paths.
	done := make(chan struct{}, 1)
	go func() {
		relay(localConn, remote, protocol == "udp")
		if protocol == "tcp" {
			closeWrite(localConn)
		} else {
			localConn.Close()
			remote.Close()
		}
		done <- struct{}{}
	}()
	relay(remote, localConn, protocol == "udp")
	if protocol == "tcp" {
		closeWrite(remote)
	} else {
		localConn.Close()
		remote.Close()
	}
	<-done
}

func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	} else {
		_ = c.Close()
	}
}

func relay(dst, src net.Conn, datagram bool) {
	if !datagram {
		_, _ = io.Copy(dst, src)
		return
	}
	buf := make([]byte, 65535)
	for {
		_ = src.SetReadDeadline(time.Now().Add(time.Minute))
		n, err := src.Read(buf)
		if err != nil {
			return
		}
		if _, err := dst.Write(buf[:n]); err != nil {
			return
		}
	}
}

type trackedListener struct {
	net.Listener
	backend *Embedded
	node    *nodeSession
}
type trackedConn struct {
	net.Conn
	backend *Embedded
	node    *nodeSession
}

func (l *trackedListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		t := &trackedConn{Conn: c, backend: l.backend, node: l.node}
		l.backend.mu.Lock()
		current, full := l.backend.node == l.node, len(l.node.sockets) >= 128
		if current && !full {
			l.node.sockets[t] = true
		}
		l.backend.mu.Unlock()
		if !current {
			c.Close()
			return nil, net.ErrClosed
		}
		if full {
			c.Close()
			continue
		}
		_ = c.SetDeadline(time.Now().Add(10 * time.Second))
		return t, nil
	}
}

func (c *trackedConn) Close() error {
	err := c.Conn.Close()
	c.backend.mu.Lock()
	delete(c.node.sockets, c)
	c.backend.mu.Unlock()
	return err
}
