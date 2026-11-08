package engine

import (
	"context"
	"net/netip"
	"time"

	"github.com/metacubex/mihomo/listener"
	"go.yaml.in/yaml/v3"
)

// StartVPN attaches a system-created Android TUN. The host keeps its descriptor;
// the engine duplicates it for mihomo. Call only after Android's VPN permission
// flow, from the VpnService owning that descriptor. JSON callers cannot attach
// arbitrary file descriptors or bypass the system's permission flow.
func (e *Engine) StartVPN(ctx context.Context, fd int, protect func(int) bool) (Status, error) {
	ctx, unregister := e.registerCall(ctx)
	defer unregister()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return Status{}, failure("closed", "内核已关闭")
	}
	if !mobileVPNSupported() {
		return Status{}, failure("unsupported", "当前平台尚未接入系统 VPN")
	}
	if e.vpn && e.core.Running() {
		return e.statusLocked(), nil
	}
	if _, err := e.tunnelPlanLocked(defaultScope()); err != nil {
		return Status{}, err
	}
	return e.startVPNLocked(ctx, fd, protect, e.pendingPlan)
}

// StartVPNWithPlan attaches only the descriptor built from a previously issued
// plan. Its opaque token binds the exact active profile and source revision.
func (e *Engine) StartVPNWithPlan(ctx context.Context, fd int, protect func(int) bool, token string) (Status, error) {
	ctx, unregister := e.registerCall(ctx)
	defer unregister()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return Status{}, failure("closed", "内核已关闭")
	}
	if !mobileVPNSupported() {
		return Status{}, failure("unsupported", "当前平台尚未接入系统 VPN")
	}
	if e.vpn && e.core.Running() && e.vpnPlan != nil && token == e.vpnPlan.Token {
		return e.statusLocked(), nil
	}
	pending, err := e.validatePendingPlanLocked(token)
	if err != nil {
		return Status{}, err
	}
	if e.vpn && e.core.Running() {
		return Status{}, failure("vpn_active", "请先断开 VPN，再更改连接范围")
	}
	return e.startVPNLocked(ctx, fd, protect, pending)
}

func (e *Engine) startVPNLocked(ctx context.Context, fd int, protect func(int) bool, pending *pendingTunnelPlan) (Status, error) {
	if fd < 0 || protect == nil {
		return Status{}, failure("invalid_params", "VPN 网卡与套接字保护回调不能为空")
	}
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	item, err := e.store.Get(e.store.ActiveID())
	if err != nil {
		return Status{}, failure("no_profile", "请先导入并选择配置")
	}
	raw, err := e.store.Content(item.ID)
	if err != nil {
		return Status{}, err
	}
	if pending == nil || pending.profileID != item.ID || pending.revision != profileRevision(item, raw) || pending.mode != e.mode {
		return Status{}, failure("stale_plan", "VPN 配置已变化，请重新准备连接")
	}
	ownedFD, err := duplicateVPNDescriptor(fd)
	if err != nil {
		return Status{}, failure("vpn_failed", "无法接收系统 VPN 网卡")
	}
	managed, err := vpnConfigurationWithPlan(pending.prepared, ownedFD, pending.plan)
	if err != nil {
		closeVPNDescriptor(ownedFD)
		return Status{}, err
	}
	e.stopLocked()
	setVPNProtector(protect)
	if err := e.core.Start(managed); err != nil {
		closeVPNDescriptor(ownedFD)
		setVPNProtector(nil)
		e.lastError = err.Error()
		return Status{}, err
	}
	e.vpnFD = ownedFD
	if !listener.GetTunConf().Enable {
		e.stopLocked()
		e.lastError = "系统 VPN 网卡未能启动"
		return Status{}, failure("vpn_failed", e.lastError)
	}
	probeCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if err := probeMixedVPN(probeCtx, e.port); err != nil {
		e.stopLocked()
		return Status{}, err
	}
	if err := ctx.Err(); err != nil {
		e.stopLocked()
		return Status{}, err
	}
	e.vpn = true
	plan := clonePlan(pending.plan)
	e.vpnPlan = &plan
	e.scope = cloneScope(plan.AppScope)
	e.acceptRuntimeLocked(managed)
	e.replaySelectionsLocked(item.ID)
	e.runtimeRevision = pending.revision
	e.reconnectRequired = false
	return e.statusLocked(), nil
}

// StopVPN stops packet processing before the host closes its system TUN.
func (e *Engine) StopVPN(ctx context.Context) (Status, error) {
	e.CancelPending()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return Status{}, failure("closed", "内核已关闭")
	}
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	e.stopLocked()
	return e.statusLocked(), nil
}

func (e *Engine) stopVPNLocked() {
	// The live listener owns vpnFD. Closing an fd again after mihomo shuts down
	// could close an unrelated socket which has reused that descriptor number.
	e.vpnFD = -1
	e.vpn = false
	e.vpnPlan = nil
	setVPNProtector(nil)
}

func vpnConfiguration(raw []byte, fd int) ([]byte, error) {
	plan, err := newTunnelPlan(defaultScope())
	if err != nil {
		return nil, err
	}
	return vpnConfigurationWithPlan(raw, fd, plan)
}

func vpnConfigurationWithPlan(raw []byte, fd int, plan TunnelPlan) ([]byte, error) {
	if plan.Version != TunnelPlanVersion || len(plan.Addresses) != 2 || len(plan.DNSServers) != 2 || len(plan.Routes) != 2 {
		return nil, failure("invalid_plan", "VPN 配置版本或结构无效")
	}
	address4, err := netip.ParsePrefix(plan.Addresses[0])
	if err != nil || !address4.Addr().Is4() {
		return nil, failure("invalid_plan", "VPN IPv4 地址无效")
	}
	address6, err := netip.ParsePrefix(plan.Addresses[1])
	if err != nil || !address6.Addr().Is6() {
		return nil, failure("invalid_plan", "VPN IPv6 地址无效")
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	doc["ipv6"] = plan.IPv6
	doc["tun"] = map[string]any{
		"enable": true, "device": "Gopurr", "stack": "gvisor",
		"file-descriptor": fd, "mtu": plan.MTU,
		"auto-route": false, "auto-detect-interface": false,
		"inet6-address": []string{address6.String()},
		"dns-hijack":    []string{"any:53", "tcp://any:53"},
	}
	dns, _ := doc["dns"].(map[string]any)
	if dns == nil {
		dns = map[string]any{}
	}
	dns["enable"] = true
	// Mihomo derives the IPv4 TUN subnet from this prefix; its raw YAML
	// inet4-address field is disabled. Keep it aligned with VpnService.Builder.
	dns["fake-ip-range"] = netip.PrefixFrom(address4.Addr(), 16).String()
	delete(dns, "listen")
	if _, exists := dns["nameserver"]; !exists {
		dns["nameserver"] = []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"}
	}
	if _, exists := dns["default-nameserver"]; !exists {
		dns["default-nameserver"] = []string{"1.1.1.1", "8.8.8.8"}
	}
	doc["dns"] = dns
	return yaml.Marshal(doc)
}
