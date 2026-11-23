package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopurr/internal/mesh"
	"gopurr/internal/patch"
	"gopurr/internal/profile"

	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/config"
	C "github.com/metacubex/mihomo/constant"
	R "github.com/metacubex/mihomo/rules"
	RC "github.com/metacubex/mihomo/rules/common"
	RW "github.com/metacubex/mihomo/rules/wrapper"
	"github.com/metacubex/mihomo/tunnel"
)

const (
	meshOnlyProfileID   = "__mesh_only__"
	meshProxyName       = "__GOPURR_TAILNET__"
	meshOnlyProfileYAML = "mixed-port: 7890\nmode: direct\nproxies: []\nproxy-groups:\n  - name: DIRECT-GROUP\n    type: select\n    proxies: [DIRECT]\nrules:\n  - MATCH,DIRECT\n"
	meshActionWait      = 2 * time.Second
)

type meshDevicePayload struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	DNSName  string    `json:"dnsName"`
	OS       string    `json:"os"`
	Owner    string    `json:"owner"`
	Login    string    `json:"login"`
	IPs      []string  `json:"ips"`
	Tags     []string  `json:"tags"`
	Online   bool      `json:"online"`
	Active   bool      `json:"active"`
	LastSeen time.Time `json:"lastSeen"`
	Path     string    `json:"path"`
}

type meshServicePayload struct {
	Protocol string `json:"protocol"`
	Port     uint16 `json:"port"`
}

type meshStatusPayload struct {
	Installed    bool                 `json:"installed"`
	Official     bool                 `json:"official"`
	Running      bool                 `json:"running"`
	ShieldsKnown bool                 `json:"shieldsKnown"`
	ShieldsUp    bool                 `json:"shieldsUp"`
	State        string               `json:"state"`
	Version      string               `json:"version"`
	AuthURL      string               `json:"authURL"`
	Tailnet      string               `json:"tailnet"`
	Self         meshDevicePayload    `json:"self"`
	Peers        []meshDevicePayload  `json:"peers"`
	Services     []meshServicePayload `json:"services"`
	Blocked      []string             `json:"blocked"`
	Health       []string             `json:"health"`
}

type meshVisitPayload struct {
	Key       string    `json:"key"`
	DeviceID  string    `json:"deviceId"`
	Device    string    `json:"device"`
	Owner     string    `json:"owner"`
	Login     string    `json:"login"`
	Remote    string    `json:"remote"`
	Local     string    `json:"local"`
	Protocol  string    `json:"protocol"`
	Active    bool      `json:"active"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
}

type meshStatusResult struct {
	Managed      bool               `json:"managed"`
	Started      bool               `json:"started"`
	Busy         bool               `json:"busy"`
	Action       string             `json:"action"`
	AutoConnect  bool               `json:"autoConnect"`
	AccessReady  bool               `json:"accessReady"`
	Error        string             `json:"error"`
	MonitorError string             `json:"monitorError"`
	Updated      time.Time          `json:"updated"`
	Status       meshStatusPayload  `json:"status"`
	Visits       []meshVisitPayload `json:"visits"`
}

type meshProbePayload struct {
	DeviceID string    `json:"deviceId"`
	State    string    `json:"state"`
	Message  string    `json:"message"`
	Port     uint16    `json:"port"`
	Checked  time.Time `json:"checked"`
}

func (e *Engine) ensureMeshLocked() *mesh.Controller {
	if e.mesh != nil {
		return e.mesh
	}
	if e.home == "" && e.meshBackend == nil {
		return nil
	}
	backend := e.meshBackend
	if backend == nil {
		backend = mesh.NewEmbedded(filepath.Join(e.home, "tailnet"), func(port uint16) bool { return int(port) == e.port })
	}
	journal := ""
	if e.home != "" {
		journal = filepath.Join(e.home, "mesh.json")
	}
	e.mesh = mesh.New(backend, journal, e.onMeshUpdate)
	return e.mesh
}

// onMeshUpdate runs from mesh.Controller's wake hook (including synchronously
// from Controller.Start/Do/SetAutoConnect). Callers must not hold e.mu when
// invoking those Controller methods.
func (e *Engine) onMeshUpdate() {
	e.meshWakeMu.Lock()
	if e.meshWakeCh != nil {
		close(e.meshWakeCh)
		e.meshWakeCh = nil
	}
	e.meshWakeMu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return
	}
	e.syncLiveMeshRoutesLocked()
}

func (e *Engine) meshNotifyChan() chan struct{} {
	e.meshWakeMu.Lock()
	defer e.meshWakeMu.Unlock()
	if e.meshWakeCh == nil {
		e.meshWakeCh = make(chan struct{})
	}
	return e.meshWakeCh
}

func (e *Engine) waitMeshIdle(ctx context.Context, ctrl *mesh.Controller, timeout time.Duration) {
	if ctrl == nil {
		return
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	// Fallback ticker covers tests that supply a Controller without onMeshUpdate.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		ch := e.meshNotifyChan()
		if !ctrl.Snapshot().Busy {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-ch:
		case <-ticker.C:
		}
	}
}

func (e *Engine) meshRuntimeRoutesLocked() ([]string, string, string) {
	if e.mesh == nil {
		return nil, "", ""
	}
	s := e.mesh.Snapshot()
	if !s.Managed || !s.Status.Official {
		return nil, "", ""
	}
	var ips []string
	for _, ip := range s.Status.RouteIPs {
		ips = append(ips, ip.String())
	}
	if s.Status.Running {
		for _, d := range s.Status.Peers {
			for _, ip := range d.IPs {
				ips = append(ips, ip.String())
			}
		}
	}
	if len(ips) == 0 {
		return nil, "", ""
	}
	sort.Strings(ips)
	ips = slices.Compact(ips)
	return ips, s.Status.ProxyAddress, s.Status.ProxyPassword
}

func (e *Engine) meshRouteKeyLocked() string {
	ips, addr, password := e.meshRuntimeRoutesLocked()
	if len(ips) == 0 || addr == "" {
		return ""
	}
	return strings.Join(ips, ",") + "|" + addr + "|" + password
}

func (e *Engine) meshActiveLocked() bool {
	if e.mesh == nil {
		return false
	}
	snap := e.mesh.Snapshot()
	ips, _, _ := e.meshRuntimeRoutesLocked()
	return snap.Managed || snap.Status.Running || len(ips) > 0
}

func (e *Engine) meshAccessReadyLocked() bool {
	if e.mesh == nil || e.core == nil || !e.core.Running() || e.meshRouteError != "" {
		return false
	}
	if mobileVPNSupported() && !e.vpn {
		return false
	}
	snap := e.mesh.Snapshot()
	return snap.Managed && snap.Status.Running && snap.Status.Official
}

func (e *Engine) meshSummaryLocked() *MeshSummary {
	if e.mesh == nil {
		return nil
	}
	snap := e.mesh.Snapshot()
	if !snap.Managed && !snap.Status.Running && snap.Status.State != "NeedsLogin" && snap.Status.State != "Starting" {
		return nil
	}
	errMsg := snap.Error
	if errMsg == "" {
		errMsg = snap.MonitorError
	}
	if errMsg == "" {
		errMsg = e.meshRouteError
	}
	return &MeshSummary{
		Managed:     snap.Managed,
		Running:     snap.Status.Running,
		State:       snap.Status.State,
		SelfName:    snap.Status.Self.Name,
		SelfIP:      snap.Status.Self.IP(),
		Tailnet:     snap.Status.Tailnet,
		PeerCount:   len(snap.Status.Peers),
		ShieldsUp:   snap.Status.ShieldsUp,
		AuthURL:     snap.Status.AuthURL,
		AccessReady: e.meshAccessReadyLocked(),
		Error:       errMsg,
	}
}

func toMeshDevicePayload(d mesh.Device) meshDevicePayload {
	ips := make([]string, 0, len(d.IPs))
	for _, ip := range d.IPs {
		ips = append(ips, ip.String())
	}
	tags := make([]string, len(d.Tags))
	copy(tags, d.Tags)
	return meshDevicePayload{
		ID:       d.ID,
		Name:     d.Name,
		DNSName:  d.DNSName,
		OS:       d.OS,
		Owner:    d.Owner,
		Login:    d.Login,
		IPs:      ips,
		Tags:     tags,
		Online:   d.Online,
		Active:   d.Active,
		LastSeen: d.LastSeen,
		Path:     d.Path,
	}
}

func (e *Engine) meshStatusLocked() meshStatusResult {
	var snap mesh.Snapshot
	if ctrl := e.ensureMeshLocked(); ctrl != nil {
		snap = ctrl.Snapshot()
	}
	peers := make([]meshDevicePayload, 0, len(snap.Status.Peers))
	for _, p := range snap.Status.Peers {
		peers = append(peers, toMeshDevicePayload(p))
	}
	services := make([]meshServicePayload, 0, len(snap.Status.Services))
	for _, svc := range snap.Status.Services {
		services = append(services, meshServicePayload{Protocol: svc.Protocol, Port: svc.Port})
	}
	blocked := make([]string, len(snap.Status.Blocked))
	copy(blocked, snap.Status.Blocked)
	health := make([]string, len(snap.Status.Health))
	copy(health, snap.Status.Health)
	visits := make([]meshVisitPayload, 0, len(snap.Visits))
	for _, v := range snap.Visits {
		visits = append(visits, meshVisitPayload{
			Key:       v.Key,
			DeviceID:  v.DeviceID,
			Device:    v.Device,
			Owner:     v.Owner,
			Login:     v.Login,
			Remote:    v.Remote,
			Local:     v.Local,
			Protocol:  v.Protocol,
			Active:    v.Active,
			FirstSeen: v.FirstSeen,
			LastSeen:  v.LastSeen,
		})
	}
	errMsg := snap.Error
	if errMsg == "" {
		errMsg = e.meshRouteError
	}
	return meshStatusResult{
		Managed:      snap.Managed,
		Started:      snap.Started,
		Busy:         snap.Busy,
		Action:       snap.Action,
		AutoConnect:  snap.AutoConnect,
		AccessReady:  e.meshAccessReadyLocked(),
		Error:        errMsg,
		MonitorError: snap.MonitorError,
		Updated:      snap.Updated,
		Status: meshStatusPayload{
			Installed:    snap.Status.Installed,
			Official:     snap.Status.Official,
			Running:      snap.Status.Running,
			ShieldsKnown: snap.Status.ShieldsKnown,
			ShieldsUp:    snap.Status.ShieldsUp,
			State:        snap.Status.State,
			Version:      snap.Status.Version,
			AuthURL:      snap.Status.AuthURL,
			Tailnet:      snap.Status.Tailnet,
			Self:         toMeshDevicePayload(snap.Status.Self),
			Peers:        peers,
			Services:     services,
			Blocked:      blocked,
			Health:       health,
		},
		Visits: visits,
	}
}

func (e *Engine) meshStatusCallLocked(params json.RawMessage) (any, error) {
	if err := emptyParams(params); err != nil {
		return nil, err
	}
	return e.meshStatusLocked(), nil
}

func (e *Engine) meshMutationCall(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "mesh.action":
		var req struct {
			Action string `json:"action"`
		}
		if err := decodeParams(params, &req); err != nil {
			return nil, err
		}
		switch req.Action {
		case "connect", "disconnect", "refresh", "allow", "block", "shieldsUp", "shieldsDown", "clearHistory", "login":
		default:
			return nil, failure("invalid_params", "不支持的组网操作")
		}
		ctrl, started, err := e.meshControllerForCall()
		if err != nil {
			return nil, err
		}
		if !started {
			ctrl.Start()
			if req.Action != "clearHistory" {
				e.waitMeshIdle(ctx, ctrl, meshActionWait)
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
		}
		switch req.Action {
		case "login":
			auth := ctrl.Snapshot().Status.AuthURL
			if !mesh.ValidAuthURL(auth) {
				return nil, failure("invalid_params", "登录地址未通过验证，请重新连接")
			}
			return map[string]any{"url": auth}, nil
		case "clearHistory":
			ctrl.ClearHistory()
		case "refresh":
			e.waitMeshIdle(ctx, ctrl, meshActionWait)
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			ctrl.Refresh()
			e.waitMeshIdle(ctx, ctrl, meshActionWait)
		case "connect":
			snap := ctrl.Snapshot()
			if !started && snap.Managed && snap.AutoConnect && (snap.Status.Running || snap.Status.AuthURL != "" || snap.Busy) {
				e.waitMeshIdle(ctx, ctrl, meshActionWait)
			} else {
				e.waitMeshIdle(ctx, ctrl, meshActionWait)
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				ctrl.Do("connect")
				e.waitMeshIdle(ctx, ctrl, meshActionWait)
			}
		case "disconnect":
			ctrl.Do("disconnect")
			e.waitMeshIdle(ctx, ctrl, meshActionWait)
		case "allow", "shieldsDown":
			e.waitMeshIdle(ctx, ctrl, meshActionWait)
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			ctrl.Do("allow")
			e.waitMeshIdle(ctx, ctrl, meshActionWait)
		case "block", "shieldsUp":
			ctrl.Do("block")
			e.waitMeshIdle(ctx, ctrl, meshActionWait)
		}

	case "mesh.services":
		var req struct {
			Services string `json:"services"`
			Enabled  bool   `json:"enabled"`
		}
		if err := decodeParams(params, &req); err != nil {
			return nil, err
		}
		parsed, err := mesh.ParseServices(req.Services)
		if err != nil {
			return nil, failure("invalid_params", err.Error())
		}
		if req.Enabled && len(parsed) == 0 {
			return nil, failure("invalid_params", "请明确填写要开放的服务端口")
		}
		e.mu.Lock()
		if e.closed {
			e.mu.Unlock()
			return nil, failure("closed", "内核已关闭")
		}
		for _, svc := range parsed {
			if int(svc.Port) == e.port {
				port := e.port
				e.mu.Unlock()
				return nil, failure("invalid_params", fmt.Sprintf("端口 %d 为本地代理保留端口，不能开放", port))
			}
		}
		ctrl := e.ensureMeshLocked()
		var started bool
		if ctrl != nil {
			started = ctrl.Snapshot().Started
		}
		e.mu.Unlock()
		if ctrl == nil {
			return nil, failure("invalid_state", "组网控制器未初始化")
		}
		if !started {
			ctrl.Start()
			e.waitMeshIdle(ctx, ctrl, meshActionWait)
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		ctrl.SetServices(mesh.FormatServices(parsed), req.Enabled)
		e.waitMeshIdle(ctx, ctrl, meshActionWait)

	case "mesh.block":
		var req struct {
			ID      string `json:"id"`
			Blocked bool   `json:"blocked"`
		}
		if err := decodeParams(params, &req); err != nil {
			return nil, err
		}
		if req.ID == "" || len(req.ID) > 256 {
			return nil, failure("invalid_params", "无效的设备标识")
		}
		ctrl, started, err := e.meshControllerForCall()
		if err != nil {
			return nil, err
		}
		if !started {
			ctrl.Start()
			e.waitMeshIdle(ctx, ctrl, meshActionWait)
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		ctrl.SetDeviceBlocked(req.ID, req.Blocked)
		e.waitMeshIdle(ctx, ctrl, meshActionWait)

	case "mesh.autoConnect":
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := decodeParams(params, &req); err != nil {
			return nil, err
		}
		ctrl, _, err := e.meshControllerForCall()
		if err != nil {
			return nil, err
		}
		ctrl.SetAutoConnect(req.Enabled)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, failure("closed", "内核已关闭")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.syncLiveMeshRoutesLocked()
	return e.meshStatusLocked(), nil
}

func (e *Engine) meshControllerForCall() (*mesh.Controller, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, false, failure("closed", "内核已关闭")
	}
	ctrl := e.ensureMeshLocked()
	if ctrl == nil {
		return nil, false, failure("invalid_state", "组网控制器未初始化")
	}
	return ctrl, ctrl.Snapshot().Started, nil
}

func (e *Engine) meshProbeCall(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		ID       string `json:"id"`
		Protocol string `json:"protocol"`
		Port     uint16 `json:"port"`
	}
	if err := decodeParams(params, &req); err != nil {
		return nil, err
	}
	if req.ID == "" || req.Port == 0 {
		return nil, failure("invalid_params", "设备标识与检测端口不能为空")
	}
	ctrl, _, err := e.meshControllerForCall()
	if err != nil {
		return nil, err
	}
	res := ctrl.Probe(ctx, req.ID, req.Protocol, req.Port)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return meshProbePayload{
		DeviceID: res.DeviceID,
		State:    res.State,
		Message:  res.Message,
		Port:     res.Port,
		Checked:  res.Checked,
	}, nil
}

func (e *Engine) runtimeSourceForPlanLocked(forceMeshOnly bool) (string, profile.Profile, []byte, error) {
	if forceMeshOnly {
		if !e.meshActiveLocked() {
			return "", profile.Profile{}, nil, failure("no_profile", "请先导入并选择配置")
		}
		return meshOnlyProfileID, profile.Profile{ID: meshOnlyProfileID, Name: "Mesh"}, []byte(meshOnlyProfileYAML), nil
	}
	return e.activeRuntimeSourceLocked()
}

func (e *Engine) activeRuntimeSourceLocked() (string, profile.Profile, []byte, error) {
	id := ""
	if e.store != nil {
		id = e.store.ActiveID()
	}
	if id == "" {
		if !e.meshActiveLocked() {
			return "", profile.Profile{}, nil, failure("no_profile", "请先导入并选择配置")
		}
		return meshOnlyProfileID, profile.Profile{ID: meshOnlyProfileID, Name: "Mesh"}, []byte(meshOnlyProfileYAML), nil
	}
	p, err := e.store.Get(id)
	if err != nil {
		return "", profile.Profile{}, nil, err
	}
	raw, err := e.store.Content(id)
	if err != nil {
		return "", profile.Profile{}, nil, err
	}
	return id, p, raw, nil
}

func (e *Engine) syncLiveMeshRoutesLocked() {
	if e.core == nil || !e.core.Running() {
		return
	}
	if e.mesh != nil && e.mesh.Snapshot().Busy {
		return
	}
	key := e.meshRouteKeyLocked()
	if key == e.meshRouteKey {
		return
	}
	if err := e.applyLiveRoutingLocked(); err != nil {
		e.meshRouteError = err.Error()
		return
	}
	e.meshRouteKey = key
	e.meshRouteError = ""
}

func (e *Engine) captureBaseRulesLocked(candidate []byte, baseRuleCount int) {
	all := tunnel.Rules()
	if baseRuleCount >= 0 {
		prefixLen := len(all) - baseRuleCount
		if prefixLen < 0 {
			prefixLen = 0
		}
		e.baseRules = slices.Clone(all[prefixLen:])
	} else {
		e.baseRules = stripInjectedMeshRules(all)
	}
	e.baseSubRules = nil
	if len(candidate) > 0 {
		if rawCfg, err := config.UnmarshalRawConfig(candidate); err == nil && len(rawCfg.SubRules) > 0 {
			if parsed, err := config.Parse(candidate); err == nil {
				e.baseSubRules = parsed.SubRules
			}
		}
	}
}

func (e *Engine) applyLiveRoutingLocked() error {
	if e.core == nil || !e.core.Running() {
		return nil
	}
	ips, addr, password := e.meshRuntimeRoutesLocked()

	baseRules := e.baseRules
	baseSubRules := e.baseSubRules
	if baseRules == nil && len(e.runtime) > 0 {
		parsed, err := config.Parse(e.runtime)
		if err != nil {
			return err
		}
		baseRules = stripInjectedMeshRules(parsed.Rules)
		baseSubRules = parsed.SubRules
		e.baseRules = baseRules
		e.baseSubRules = baseSubRules
	}

	currentProxies := tunnel.Proxies()
	nextProxies := make(map[string]C.Proxy, len(currentProxies)+1)
	for k, v := range currentProxies {
		if k == meshProxyName {
			continue
		}
		nextProxies[k] = v
	}

	var prefixRules []C.Rule
	targetMode := e.mode
	if !validMode(targetMode) {
		targetMode = "rule"
	}

	if len(ips) > 0 && addr != "" {
		meshRulesYAML := buildMeshRuleLines(ips)
		if len(meshRulesYAML) > 0 {
			host, portText, splitErr := net.SplitHostPort(addr)
			port, portErr := strconv.Atoi(portText)
			if splitErr != nil || portErr != nil || host != "127.0.0.1" || port < 1 || port > 65535 || len(password) < 32 {
				return errors.New("内置组网桥接地址或凭据无效")
			}
			tailnetProxy, err := adapter.ParseProxy(map[string]any{
				"name":     meshProxyName,
				"type":     "ss",
				"server":   host,
				"port":     port,
				"cipher":   "chacha20-ietf-poly1305",
				"password": password,
				"udp":      true,
			}, adapter.WithTunnelForAPI(tunnel.Tunnel))
			if err != nil {
				return err
			}
			nextProxies[meshProxyName] = tailnetProxy

			for _, line := range meshRulesYAML {
				parsed, err := parseLiveRuleLine(line)
				if err != nil {
					return err
				}
				prefixRules = append(prefixRules, parsed)
			}
			switch targetMode {
			case "direct":
				parsed, err := parseLiveRuleLine("MATCH,DIRECT")
				if err != nil {
					return err
				}
				prefixRules = append(prefixRules, parsed)
			case "global":
				parsed, err := parseLiveRuleLine("MATCH,GLOBAL")
				if err != nil {
					return err
				}
				prefixRules = append(prefixRules, parsed)
			}
		}
	}

	nextRules := make([]C.Rule, 0, len(prefixRules)+len(baseRules))
	nextRules = append(nextRules, prefixRules...)
	nextRules = append(nextRules, baseRules...)

	tunnel.UpdateProxies(nextProxies, tunnel.Providers())
	tunnel.UpdateRules(nextRules, baseSubRules, tunnel.RuleProviders())
	if len(prefixRules) > 0 {
		e.core.SetMode("rule")
	} else {
		e.core.SetMode(targetMode)
	}
	return nil
}

func buildMeshRuleLines(ips []string) []string {
	var rules []string
	seen := map[netip.Addr]bool{}
	v4Prefix := netip.MustParsePrefix("100.64.0.0/10")
	v6Prefix := netip.MustParsePrefix("fd7a:115c:a1e0::/48")
	for _, raw := range ips {
		ip, err := netip.ParseAddr(raw)
		if err != nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || seen[ip] {
			continue
		}
		if !v4Prefix.Contains(ip) {
			continue
		}
		seen[ip] = true
		prefix := netip.PrefixFrom(ip, 32).String()
		rules = append(rules, "IP-CIDR,"+prefix+","+meshProxyName+",no-resolve")
	}
	for _, raw := range ips {
		ip, err := netip.ParseAddr(raw)
		if err != nil || seen[ip] || !v6Prefix.Contains(ip) {
			continue
		}
		seen[ip] = true
		prefix := netip.PrefixFrom(ip, 128).String()
		rules = append(rules, "IP-CIDR6,"+prefix+","+meshProxyName+",no-resolve")
	}
	return rules
}

func parseLiveRuleLine(line string) (C.Rule, error) {
	tp, payload, target, params := RC.ParseRulePayload(line, true)
	if target == "" {
		return nil, fmt.Errorf("invalid rule %q", line)
	}
	parsed, err := R.ParseRule(tp, payload, target, params, nil)
	if err != nil {
		return nil, err
	}
	return RW.NewRuleWrapper(parsed), nil
}

func stripInjectedMeshRules(rules []C.Rule) []C.Rule {
	idx := 0
	for idx < len(rules) && rules[idx] != nil && rules[idx].Adapter() == meshProxyName {
		idx++
	}
	if idx > 0 && idx < len(rules) && rules[idx] != nil && rules[idx].RuleType() == C.MATCH && (rules[idx].Adapter() == "DIRECT" || rules[idx].Adapter() == "GLOBAL") {
		idx++
	}
	out := make([]C.Rule, 0, len(rules)-idx)
	for _, r := range rules[idx:] {
		if r != nil {
			out = append(out, r)
		}
	}
	return out
}

func baseRuleCount(raw []byte, ov patch.Overrides, local patch.ProfileOverrides) int {
	baseOv := ov
	baseOv.MeshIPs = nil
	baseOv.MeshProxyAddress = ""
	baseOv.MeshProxyPassword = ""
	baseMerged, err := patch.ApplyLayers(raw, baseOv, local)
	if err != nil {
		return -1
	}
	rawCfg, err := config.UnmarshalRawConfig(baseMerged)
	if err != nil {
		return -1
	}
	return len(rawCfg.Rule)
}
