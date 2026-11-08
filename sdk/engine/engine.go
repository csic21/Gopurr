package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopurr/internal/configyaml"
	"gopurr/internal/controllerconfig"
	"gopurr/internal/core"
	"gopurr/internal/geodata"
	"gopurr/internal/patch"
	"gopurr/internal/profile"
	"gopurr/internal/securefs"
	"gopurr/internal/workflow"

	"github.com/metacubex/mihomo/common/observable"
	"github.com/metacubex/mihomo/config"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/listener"
	LC "github.com/metacubex/mihomo/listener/config"
	"github.com/metacubex/mihomo/log"
	"github.com/metacubex/mihomo/tunnel"
	"github.com/metacubex/mihomo/tunnel/statistic"
	"go.yaml.in/yaml/v3"
)

var ownership struct {
	sync.Mutex
	home   string
	active bool
}

// Engine serializes host commands around mihomo's process-wide state. Open it
// once in a native host, use start/stop for connection changes, then Close it
// when the host exits. A process cannot change its Home, even after Close:
// upstream caches retain the directory selected on their first use.
type Engine struct {
	mu                sync.Mutex
	closeMu           sync.Mutex
	profileMu         sync.Mutex
	operation         *Operation
	operationID       uint64
	runtimeRevision   string
	runtimeEpoch      uint64
	reconnectRequired bool
	pendingPlan       *pendingTunnelPlan
	vpnPlan           *TunnelPlan
	scope             AppScope
	cancelMu          sync.Mutex
	activeCancels     map[uint64]context.CancelFunc
	nextCall          uint64
	core              *core.Core
	store             *profile.Store
	home              string
	port              int
	mode              string
	runtime           []byte
	groupOrder        []string
	delays            map[string]uint16
	manualSelections  map[string]map[string]string
	lastError         string
	closed            bool
	vpnFD             int
	vpn               bool
	logsMu            sync.Mutex
	logs              logBuffer
	logSubscription   observable.Subscription[log.Event]
	logsDone          chan struct{}
}

func Open(options Options) (*Engine, error) {
	if options.Home == "" || !filepath.IsAbs(options.Home) || filepath.Clean(options.Home) == string(filepath.Separator) {
		return nil, failure("invalid_params", "请为内核指定独立的绝对数据目录")
	}
	if options.MixedPort < 0 || options.MixedPort > 65535 {
		return nil, failure("invalid_params", "代理端口须为 0–65535")
	}
	ownership.Lock()
	defer ownership.Unlock()
	if ownership.active {
		return nil, failure("engine_in_use", "同一进程只能运行一个内核实例")
	}
	if err := securefs.PrivateDir(options.Home); err != nil {
		return nil, err
	}
	home, err := filepath.EvalSymlinks(options.Home)
	if err != nil {
		return nil, err
	}
	if ownership.home != "" && ownership.home != home {
		return nil, failure("engine_in_use", "更换内核数据目录需要重新启动宿主进程")
	}
	s, err := profile.Open(home)
	if err != nil {
		return nil, err
	}
	port := options.MixedPort
	if port == 0 {
		port, err = availablePort()
		if err != nil {
			return nil, err
		}
	}
	kernelHome := filepath.Join(home, "mihomo")
	if err := geodata.Ensure(kernelHome); err != nil {
		return nil, err
	}
	e := &Engine{core: core.New(kernelHome), store: s, home: home, port: port, mode: "rule", vpnFD: -1, delays: map[string]uint16{}, logsDone: make(chan struct{})}
	if err := e.loadPreferences(); err != nil {
		return nil, err
	}
	e.logSubscription = log.Subscribe()
	go e.watchLogs()
	ownership.home, ownership.active = home, true
	return e, nil
}

func (e *Engine) watchLogs() {
	defer close(e.logsDone)
	for event := range e.logSubscription {
		e.logsMu.Lock()
		e.logs.add(Log{At: time.Now(), Level: strings.ToLower(event.LogLevel.String()), Text: event.Payload})
		e.logsMu.Unlock()
	}
}

// Close is idempotent. It never restores or modifies OS proxy settings: this
// engine does not take ownership of those settings in the first place.
func (e *Engine) Close() error {
	e.closeMu.Lock()
	defer e.closeMu.Unlock()
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.mu.Unlock()
	e.CancelPending()
	// A profile commit can be inside Store's atomic cache/index section after
	// its last cancellation check. Drain its writer before handing process
	// ownership to a newly opened Store. Never wait with e.mu held: completion
	// publishes its result and clears operation under that mutex.
	e.profileMu.Lock()
	e.profileMu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopLocked()
	log.UnSubscribe(e.logSubscription)
	<-e.logsDone
	ownership.Lock()
	ownership.active = false
	ownership.Unlock()
	return nil
}

// Dispatch is the JSON boundary used by Swift/Kotlin/Rust hosts. The host is
// responsible for authenticating callers if it exposes this over any transport.
func (e *Engine) Dispatch(ctx context.Context, request []byte) (response []byte) {
	var req Request
	defer func() {
		if recover() != nil {
			response, _ = json.Marshal(Response{ID: req.ID, Error: failure("internal_error", "内核操作未完成，请重新启动后重试")})
		}
	}()
	if len(request) > MaxDocumentBytes {
		response, _ = json.Marshal(Response{Error: failure("invalid_request", "请求内容过大")})
		return
	}
	if err := decode(request, &req); err != nil || req.Method == "" {
		response, _ = json.Marshal(Response{Error: failure("invalid_request", "请求格式无效")})
		return
	}
	result, err := e.Call(ctx, req.Method, req.Params)
	r := Response{ID: req.ID, Result: result}
	if err != nil {
		r.Result = nil
		r.Error = asError(err)
	}
	response, _ = json.Marshal(r)
	return
}

// Call's method names and lower-camel-case result fields form API version 1.
// Changes are serialized; downloads and delay tests run outside the state lock.
func (e *Engine) Call(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if method == "engine.stop" {
		e.CancelPending()
	}
	ctx, unregister := e.registerCall(ctx)
	defer unregister()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if method == "profiles.import" || method == "profiles.refresh" {
		return e.profileNetworkCall(ctx, method, params)
	}
	if method == "proxies.delay" {
		return e.delayCall(ctx, params)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, failure("closed", "内核已关闭")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch method {
	case "engine.traffic":
		if err := emptyParams(params); err != nil {
			return nil, err
		}
		// Notifications need only atomic byte counters, not a connection walk
		// or the process-memory probe performed by engine.status.
		rates := TrafficRates{At: time.Now().UnixMilli()}
		if e.core != nil && e.core.Running() {
			rates.Up, rates.Down = statistic.DefaultManager.Now()
			rates.UpTotal, rates.DownTotal = statistic.DefaultManager.Total()
		}
		return rates, nil
	case "engine.status":
		if err := emptyParams(params); err != nil {
			return nil, err
		}
		return e.statusLocked(), nil
	case "engine.start":
		if err := emptyParams(params); err != nil {
			return nil, err
		}
		if e.core.Running() {
			return e.statusLocked(), nil
		}
		if err := e.activateLocked(ctx, e.store.ActiveID()); err != nil {
			return nil, err
		}
		return e.statusLocked(), nil
	case "engine.stop":
		if err := emptyParams(params); err != nil {
			return nil, err
		}
		e.stopLocked()
		return e.statusLocked(), nil
	case "engine.mode":
		var p struct {
			Mode string `json:"mode"`
		}
		if err := decodeParams(params, &p); err != nil {
			return nil, err
		}
		if !validMode(p.Mode) {
			return nil, failure("invalid_params", "路由模式无效")
		}
		if err := e.savePreferencesLocked(p.Mode, e.manualSelections); err != nil {
			return nil, err
		}
		e.mode = p.Mode
		if e.core.Running() {
			e.core.SetMode(p.Mode)
		}
		return e.statusLocked(), nil
	case "profiles.list":
		if err := emptyParams(params); err != nil {
			return nil, err
		}
		return e.profilesLocked(), nil
	case "profiles.update", "profiles.delete", "profiles.editor":
		return e.profileLocalCallLocked(ctx, method, params)
	case "engine.tunnelPlan":
		var p struct {
			AppScope AppScope `json:"appScope"`
		}
		if err := decodeParams(params, &p); err != nil {
			return nil, err
		}
		return e.tunnelPlanLocked(p.AppScope)
	case "profiles.activate":
		var p struct {
			ID string `json:"id"`
		}
		if err := decodeParams(params, &p); err != nil {
			return nil, err
		}
		if err := e.activateLocked(ctx, p.ID); err != nil {
			return nil, err
		}
		return e.statusLocked(), nil
	case "proxies.groups":
		if err := emptyParams(params); err != nil {
			return nil, err
		}
		if !e.core.Running() {
			return e.previewGroupsLocked()
		}
		return e.groupsLocked(), nil
	case "proxies.select":
		var p struct {
			Group  string `json:"group"`
			Member string `json:"member"`
		}
		if err := decodeParams(params, &p); err != nil {
			return nil, err
		}
		if !e.core.Running() {
			return nil, failure("not_running", "请先连接代理")
		}
		if err := e.selectAndSaveLocked(p.Group, p.Member); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	case "logs.list":
		if err := emptyParams(params); err != nil {
			return nil, err
		}
		e.logsMu.Lock()
		defer e.logsMu.Unlock()
		return map[string]any{"logs": e.logs.snapshot()}, nil
	case "engine.vpn", "engine.systemProxy":
		return nil, failure("unsupported", "当前宿主尚未接入系统 VPN 或系统代理")
	default:
		return nil, failure("method_not_found", "不支持此内核操作")
	}
}

func (e *Engine) statusLocked() Status {
	running := e.core != nil && e.core.Running()
	s := Status{APIVersion: APIVersion, Running: running, ActiveProfile: e.store.ActiveID(), MixedPort: e.port, Mode: e.mode, Error: e.lastError, Busy: e.operation != nil, ReconnectRequired: running && e.reconnectRequired, TunnelPlanVersion: TunnelPlanVersion, AppScope: cloneScope(e.scope)}
	if s.AppScope.Mode == "" {
		s.AppScope = defaultScope()
	}
	if e.operation != nil {
		operation := *e.operation
		s.Operation = &operation
	}
	if e.vpnPlan != nil && e.vpn {
		s.AppScope = cloneScope(e.vpnPlan.AppScope)
	}
	s.VPNSupported = mobileVPNSupported()
	s.Capabilities.Tun = s.VPNSupported
	s.Tun = e.vpn && s.Running
	if s.Running {
		s.Mode = e.core.Mode()
		sample := e.core.Sample()
		s.Traffic = Traffic{Up: sample.UpNow, Down: sample.DownNow, UpTotal: sample.UpTotal, DownTotal: sample.DownTotal, Connections: sample.Conns, Memory: sample.Mem, At: time.Now().UnixMilli()}
	}
	return s
}

func (e *Engine) profilesLocked() ProfileList {
	list := ProfileList{Active: e.store.ActiveID(), Profiles: []Profile{}}
	for _, p := range e.store.List() {
		summary := profileSummary(p, list.Active)
		summary.ReconnectRequired = summary.Active && e.reconnectRequired && e.core != nil && e.core.Running()
		list.Profiles = append(list.Profiles, summary)
	}
	return list
}

func profileSummary(p profile.Profile, active string) Profile {
	source := "本地配置"
	if p.URL != "" {
		if u, err := url.Parse(p.URL); err == nil {
			source = u.Hostname()
		}
	}
	return Profile{ID: p.ID, Name: p.Name, Kind: p.Kind, Source: source, Updated: p.Updated, Active: p.ID == active}
}

func (e *Engine) activateLocked(ctx context.Context, id string) error {
	if !e.profileMu.TryLock() {
		return failure("busy", "订阅正在更新，请稍后重试")
	}
	defer e.profileMu.Unlock()
	if e.vpn {
		return failure("vpn_active", "请先断开 VPN，再切换订阅")
	}
	if id == "" {
		return failure("no_profile", "请先导入并选择配置")
	}
	p, err := e.store.Get(id)
	if err != nil {
		return err
	}
	raw, err := e.store.Content(id)
	if err != nil {
		return err
	}
	candidate, err := e.prepareLocked(raw, p)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	previous := append([]byte(nil), e.runtime...)
	wasRunning := e.core.Running()
	previousGroups := e.groupsLocked().Groups
	previousRevision, previousReconnect := e.runtimeRevision, e.reconnectRequired
	restore := func(restoreContext context.Context) error {
		if wasRunning && len(previous) > 0 {
			if err := e.startLocked(restoreContext, previous); err != nil {
				e.stopLocked()
				return err
			} else {
				e.core.SetMode(e.mode)
				for _, group := range previousGroups {
					if group.Type == "Selector" && group.Now != "" {
						_ = e.core.Select(group.Name, group.Now)
					}
				}
			}
		} else {
			e.stopLocked()
		}
		e.runtimeRevision, e.reconnectRequired = previousRevision, previousReconnect
		return nil
	}
	_, err = workflow.RunConfigChange(ctx, workflow.ConfigChange{Candidate: candidate, Apply: e.startLocked, Restore: restore, Commit: func(commitCtx context.Context) error { return e.store.SetActiveContext(commitCtx, id) }})
	if err != nil {
		e.lastError = err.Error()
		return err
	}
	e.acceptRuntimeLocked(candidate)
	e.replaySelectionsLocked(id)
	e.runtimeRevision = profileRevision(p, raw)
	e.reconnectRequired = false
	e.pendingPlan = nil
	return nil
}

func (e *Engine) acceptRuntimeLocked(candidate []byte) {
	e.runtime = candidate
	e.lastError = ""
	e.delays = map[string]uint16{}
	var order struct {
		Groups []struct {
			Name string `yaml:"name"`
		} `yaml:"proxy-groups"`
	}
	_ = yaml.Unmarshal(candidate, &order)
	e.groupOrder = nil
	for _, g := range order.Groups {
		e.groupOrder = append(e.groupOrder, g.Name)
	}
}

func (e *Engine) startLocked(ctx context.Context, raw []byte) error {
	// A native host prioritizes an explicit reconnect over reloading mutable
	// upstream listener globals while the old traffic path is still live.
	e.stopLocked()
	if err := e.core.Start(raw); err != nil {
		return &Error{Code: "invalid_config", Message: err.Error()}
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	for {
		if err := core.ProbeMixed(ctx, e.port); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return failure("listener_failed", "本地代理端口未能启动，请检查端口占用")
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func (e *Engine) stopLocked() {
	e.runtimeEpoch++
	if e.core != nil && e.core.Running() {
		statistic.DefaultManager.Range(func(tracker statistic.Tracker) bool { _ = tracker.Close(); return true })
	}
	// Shutdown only closes TUN upstream. A host remains alive after Stop,
	// so close mixed TCP/UDP too. Reset the remembered TUN configuration
	// before shutdown so an Android fd reused on reconnect is recreated.
	// Also clean up a partially applied Start which never set running=true.
	listener.ReCreateMixed(0, tunnel.Tunnel)
	listener.ReCreateTun(LC.Tun{Enable: false}, tunnel.Tunnel)
	if e.core != nil && e.core.Running() {
		e.core.Stop()
	}
	e.stopVPNLocked()
	e.pendingPlan = nil
}

// prepareLocked keeps source rules and node credentials but takes exclusive
// control over every inbound and all OS-facing features. The first NSH host
// supports a local HTTP/SOCKS endpoint; it must never claim VPN is connected.
func (e *Engine) prepareLocked(raw []byte, p profile.Profile) ([]byte, error) {
	ov := patch.Default()
	ov.MixedPort = e.port
	ov.Mode = e.mode
	ov.TunEnable = false
	local := patch.ProfileOverrides{}
	if p.Overrides != nil {
		local = *p.Overrides
	}
	merged, err := patch.ApplyLayers(raw, ov, local)
	if err != nil {
		return nil, &Error{Code: "invalid_config", Message: err.Error()}
	}
	var doc map[string]any
	if err := yaml.Unmarshal(merged, &doc); err != nil {
		return nil, err
	}
	controllerconfig.Strip(doc)
	for _, key := range []string{"port", "socks-port", "redir-port", "tproxy-port", "listeners", "tunnels", "ss-config", "vmess-config", "authentication", "skip-auth-prefixes", "interface-name", "routing-mark", "tls"} {
		delete(doc, key)
	}
	doc["allow-lan"] = false
	doc["bind-address"] = "127.0.0.1"
	doc["mixed-port"] = e.port
	doc["tun"] = map[string]any{"enable": false}
	doc["iptables"] = map[string]any{"enable": false}
	doc["ntp"] = map[string]any{"enable": false}
	doc["tuic-server"] = map[string]any{"enable": false}
	doc["find-process-mode"] = "off"
	doc["geo-auto-update"] = false
	doc["profile"] = map[string]any{"store-selected": false, "store-fake-ip": false}
	if dns, ok := doc["dns"].(map[string]any); ok {
		delete(dns, "listen")
	}
	for _, key := range []string{"proxy-providers", "rule-providers"} {
		if providers, ok := doc[key].(map[string]any); ok {
			for _, item := range providers {
				if provider, ok := item.(map[string]any); ok {
					if path, ok := provider["path"].(string); ok && path != "" && !filepath.IsLocal(path) {
						return nil, failure("invalid_config", "配置引用的文件必须位于内核数据目录内")
					}
				}
			}
		}
	}
	managed, err := configyaml.Marshal(merged, doc)
	if err != nil {
		return nil, err
	}
	if _, err := config.Parse(managed); err != nil {
		return nil, &Error{Code: "invalid_config", Message: err.Error()}
	}
	return managed, nil
}

func (e *Engine) groupsLocked() GroupList {
	result := GroupList{Groups: []Group{}}
	if !e.core.Running() {
		return result
	}
	proxies := tunnel.Proxies()
	order := append([]string(nil), e.groupOrder...)
	known := map[string]bool{}
	for _, name := range order {
		known[name] = true
	}
	var extra []string
	for name := range proxies {
		if !known[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	order = append(order, extra...)
	for _, name := range order {
		p := proxies[name]
		if p == nil {
			continue
		}
		adapter, ok := p.Adapter().(interface{ Proxies() []C.Proxy })
		if !ok {
			continue
		}
		g := Group{Name: name, Type: p.Type().String(), Members: []Member{}}
		if current, ok := p.Adapter().(interface{ Now() string }); ok {
			g.Now = current.Now()
		}
		if hidden, ok := p.Adapter().(interface{ Hidden() bool }); ok {
			g.Hidden = hidden.Hidden()
		}
		for _, member := range adapter.Proxies() {
			m := Member{Name: member.Name(), Type: member.Type().String()}
			if delay, ok := e.delays[m.Name]; ok {
				m.Delay = &delay
			}
			g.Members = append(g.Members, m)
		}
		result.Groups = append(result.Groups, g)
	}
	return result
}

func availablePort() (int, error) {
	for attempt := 0; attempt < 20; attempt++ {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, err
		}
		port := listener.Addr().(*net.TCPAddr).Port
		udp, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", port))
		_ = listener.Close()
		if err == nil {
			_ = udp.Close()
			return port, nil
		}
	}
	return 0, failure("listener_failed", "无法分配本地代理端口")
}

func httpURL(address string) (*url.URL, error) {
	u, err := url.Parse(address)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || strings.ContainsAny(address, " \t\r\n") {
		return nil, failure("invalid_params", "请输入完整的 HTTP 或 HTTPS 链接")
	}
	return u, nil
}

func emptyParams(raw json.RawMessage) error { var empty struct{}; return decodeParams(raw, &empty) }
func decodeParams(raw json.RawMessage, out any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	if err := decode(raw, out); err != nil {
		return failure("invalid_params", "操作参数无效")
	}
	return nil
}
func decode(raw []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
func failure(code, message string) *Error { return &Error{Code: code, Message: message} }
func asError(err error) *Error {
	var api *Error
	if errors.As(err, &api) {
		return api
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return failure("cancelled", "操作已取消或超时")
	}
	var download *profile.DownloadError
	if errors.As(err, &download) {
		if download.StatusCode > 0 {
			return failure("download_failed", fmt.Sprintf("订阅下载失败：HTTP %d", download.StatusCode))
		}
		return failure("download_failed", "订阅下载失败，请检查链接与网络")
	}
	if errors.Is(err, profile.ErrDocumentTooLarge) {
		return failure("invalid_config", "配置超过 16 MiB")
	}
	return failure("operation_failed", err.Error())
}
