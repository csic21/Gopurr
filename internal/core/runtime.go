package core

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/metacubex/mihomo/component/resolver"
	"github.com/metacubex/mihomo/hub/executor"
)

// RuntimeStatus describes observed local traffic paths, not Internet reachability.
// Read it under the same lock as configuration reloads.
type RuntimeStatus struct {
	MixedPort  int    `json:"mixedPort"`
	ProxyReady bool   `json:"proxyReady"`
	ProxyError string `json:"proxyError,omitempty"`
	TunEnabled bool   `json:"tunEnabled"`
	TunReady   bool   `json:"tunReady"`
	TunDevice  string `json:"tunDevice,omitempty"`
	TunStack   string `json:"tunStack,omitempty"`
	AutoRoute  bool   `json:"autoRoute"`
	DNSReady   bool   `json:"dnsReady"`
	TunError   string `json:"tunError,omitempty"`
}

func runtimeStatus(ctx context.Context) RuntimeStatus {
	g := executor.GetGeneral()
	s := RuntimeStatus{MixedPort: g.MixedPort, TunEnabled: g.Tun.Enable,
		TunDevice: g.Tun.Device, TunStack: g.Tun.Stack.String(),
		DNSReady: resolver.DefaultResolver != nil}
	if g.MixedPort > 0 {
		if err := ProbeMixed(ctx, g.MixedPort); err != nil {
			s.ProxyError = err.Error()
		} else {
			s.ProxyReady = true
		}
	}
	if !s.TunEnabled {
		return s
	}
	if !s.DNSReady {
		s.TunError = "内置 DNS 未就绪"
		return s
	}
	iface, err := net.InterfaceByName(s.TunDevice)
	if err != nil || iface.Flags&net.FlagUp == 0 {
		s.TunError = "TUN 网卡未就绪"
		return s
	}
	addresses, err := iface.Addrs()
	if err != nil || len(addresses) == 0 {
		s.TunError = "TUN 网卡尚未分配地址"
		return s
	}
	if g.Tun.AutoRoute {
		if err := checkTunRoutes(iface.Index); err != nil {
			s.TunError = err.Error()
			return s
		}
		s.AutoRoute = true
	}
	s.TunReady = true
	return s
}

// ProbeMixed completes a SOCKS5 greeting. A successful TCP dial alone can hit
// an unrelated process, or a listener mihomo has closed after UDP bind failed.
// No destination is contacted and no system network settings are changed.
func ProbeMixed(ctx context.Context, port int) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("本地代理端口不可连接: %w", err)
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	if _, err := conn.Write([]byte{5, 1, 0}); err != nil {
		return fmt.Errorf("本地代理握手失败: %w", err)
	}
	var reply [2]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		return fmt.Errorf("本地代理握手失败: %w", err)
	}
	if reply != [2]byte{5, 0} {
		return fmt.Errorf("本地代理未接受免认证连接，请检查入站认证设置")
	}
	return nil
}
