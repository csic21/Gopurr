package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dlclark/regexp2"
	C "github.com/metacubex/mihomo/constant"
	"go.yaml.in/yaml/v3"

	"gopurr/internal/buildinfo"
	"gopurr/internal/core"
	"gopurr/internal/patch"
	"gopurr/internal/profile"
	"gopurr/internal/remote"
)

// webHost is the narrow bridge between the authenticated browser server and
// the desktop app. Mutations that touch UI-owned fields are dispatched to the
// main thread; kernel calls stay on the request goroutine.
type webHost struct{ a *App }

func (h webHost) Status(ctx context.Context) (remote.Status, error) {
	a := h.a
	if a == nil || a.k == nil {
		return remote.Status{}, errors.New(tr("内核客户端没有初始化"))
	}
	s := remote.Status{Version: buildinfo.Version, Kernel: a.k.Running()}
	sample := a.k.Sample()
	s.Traffic = remote.Traffic{
		Up: sample.UpNow, Down: sample.DownNow,
		UpTotal: sample.UpTotal, DownTotal: sample.DownTotal,
		Stale: sample.TrafficStale,
	}
	if !sample.TrafficAt.IsZero() {
		s.Traffic.At = sample.TrafficAt.UnixMilli()
	}
	s.Connections = sample.Conns
	s.ConnectionsStale = sample.ConnectionsStale
	if s.Kernel {
		s.Proxies, s.Rules, _ = a.k.Counts()
	}

	runOnMain(func() {
		s.Mode = a.modeToken
		s.SystemProxy = a.sysApplied
		s.Tun = a.tunOn
		s.Notice = a.notice
		s.Busy = busyNetwork(a)
		if text := a.sysErrText(); text != "" {
			s.Error = text
		} else if text := a.tunErrText(); text != "" {
			s.Error = text
		}
		if installed, _, _ := a.svcState(); installed {
			s.TunInstalled = true
		}
	})
	s.SystemProxyAvailable = systemProxyAvailable()
	s.TunAvailable = s.Kernel && a.k.IsRoot()
	if !s.SystemProxyAvailable && s.SystemProxy {
		s.SystemProxyAvailable = true
	}
	return s, nil
}

func (h webHost) Groups(ctx context.Context) ([]remote.Group, error) {
	a := h.a
	if a == nil || a.k == nil {
		return nil, errors.New(tr("内核客户端没有初始化"))
	}
	if !a.k.Running() {
		return a.previewGroupsOffline(), nil
	}
	groups := a.k.Groups()
	out := make([]remote.Group, 0, len(groups))
	for _, group := range groups {
		item := remote.Group{
			Name:   group.Name,
			Type:   group.Type,
			Now:    group.Now,
			Hidden: group.Hidden,
			Global: group.Name == "GLOBAL",
		}
		item.Members = make([]remote.Member, 0, len(group.Members))
		for _, name := range group.Members {
			member := remote.Member{Name: name, Type: group.MemberTypes[name]}
			if delay, ok := group.Delays[name]; ok {
				value := int(delay)
				member.Delay = &value
				member.DelayKnown = true
			}
			item.Members = append(item.Members, member)
		}
		out = append(out, item)
	}
	return out, nil
}

func (h webHost) Select(ctx context.Context, group, member string) error {
	a := h.a
	if a == nil || a.k == nil {
		return errors.New(tr("内核客户端没有初始化"))
	}
	if !a.k.Running() {
		return errors.New(tr("内核尚未就绪"))
	}
	return a.webSelect(group, member)
}

func (h webHost) SetMode(ctx context.Context, mode string) error {
	a := h.a
	if a == nil || a.k == nil {
		return errors.New(tr("内核客户端没有初始化"))
	}
	if !a.k.Running() {
		return errors.New(tr("内核尚未就绪"))
	}
	return a.webSetMode(mode)
}

func (h webHost) SetSystemProxy(ctx context.Context, on bool) error {
	a := h.a
	if a == nil {
		return errors.New(tr("应用尚未就绪"))
	}
	if !systemProxyAvailable() {
		return errors.New(tr("当前平台暂不支持自动设置系统代理"))
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	result := make(chan error, 1)
	runOnMain(func() {
		if a.ShuttingDown() {
			result <- errors.New(tr("应用正在退出"))
			return
		}
		if busyNetwork(a) {
			result <- errors.New(tr("网络操作进行中，请稍后重试"))
			return
		}
		a.requestSystemProxy(on, true, true, func(err error) { result <- err })
	})
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return errors.New(tr("操作超时，请在本机确认系统授权"))
	}
}

func (h webHost) SetTun(ctx context.Context, on bool) error {
	a := h.a
	if a == nil {
		return errors.New(tr("应用尚未就绪"))
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	result := make(chan struct{}, 1)
	var rejected error
	runOnMain(func() {
		if a.ShuttingDown() {
			rejected = errors.New(tr("应用正在退出"))
			result <- struct{}{}
			return
		}
		if busyNetwork(a) {
			rejected = errors.New(tr("网络操作进行中，请稍后重试"))
			result <- struct{}{}
			return
		}
		a.requestTun(on, func() { result <- struct{}{} })
	})
	select {
	case <-result:
	case <-ctx.Done():
		return errors.New(tr("操作超时，请稍后重试"))
	}
	if rejected != nil {
		return rejected
	}
	var err error
	runOnMain(func() {
		if a.tunOn != on {
			message := a.tunErrText()
			if message == "" {
				message = tr("TUN 未就绪，请检查系统服务")
			}
			err = errors.New(message)
		}
	})
	return err
}

func (h webHost) Connections(ctx context.Context) ([]remote.Connection, error) {
	a := h.a
	if a == nil || a.k == nil {
		return nil, errors.New(tr("内核客户端没有初始化"))
	}
	conns, err := a.k.CachedConnections(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]remote.Connection, 0, len(conns))
	for _, conn := range conns {
		item := remote.Connection{
			ID: conn.ID, Host: conn.Host, Network: conn.Network,
			Process: conn.Process, ProcessPath: conn.ProcessPath,
			Source:      joinAddress(conn.SourceIP, conn.SourcePort),
			Destination: joinAddress(conn.DestinationIP, conn.DestinationPort),
			Rule:        conn.Rule, RulePayload: conn.RulePayload, Chain: conn.Chain,
			Chains: append([]string(nil), conn.Chains...),
			Up:     conn.Up, Down: conn.Down, Start: conn.Start,
			SourceIP: conn.SourceIP, SourcePort: conn.SourcePort,
			DestinationIP: conn.DestinationIP, DestinationPort: conn.DestinationPort,
		}
		out = append(out, item)
	}
	return out, nil
}

func (h webHost) CloseConnection(ctx context.Context, id string) error {
	a := h.a
	if a == nil || a.k == nil {
		return errors.New(tr("内核客户端没有初始化"))
	}
	return a.k.CloseConnection(id)
}

func (h webHost) CloseAllConnections(ctx context.Context) error {
	a := h.a
	if a == nil || a.k == nil {
		return errors.New(tr("内核客户端没有初始化"))
	}
	return a.k.CloseAllConnections()
}

func (h webHost) Logs(ctx context.Context) ([]remote.LogEntry, error) {
	a := h.a
	if a == nil || a.k == nil {
		return nil, errors.New(tr("内核客户端没有初始化"))
	}
	lines := a.k.Logs()
	out := make([]remote.LogEntry, 0, len(lines))
	for _, line := range lines {
		out = append(out, remote.LogEntry{At: line.At, Level: line.Level, Text: line.Text})
	}
	return out, nil
}

func (h webHost) ClearLogs(ctx context.Context) error {
	a := h.a
	if a == nil || a.k == nil {
		return errors.New(tr("内核客户端没有初始化"))
	}
	a.k.ClearLogs()
	return nil
}

func (h webHost) Delay(ctx context.Context, group string, names []string) (map[string]remote.DelayResult, error) {
	a := h.a
	if a == nil || a.k == nil {
		return nil, errors.New(tr("内核客户端没有初始化"))
	}
	if !a.k.Running() {
		return nil, errors.New(tr("内核尚未就绪"))
	}
	groups := a.k.Groups()
	byName := make(map[string]core.Group, len(groups))
	groupNames := make(map[string]bool, len(groups))
	for _, item := range groups {
		byName[item.Name] = item
		groupNames[item.Name] = true
	}
	if len(names) == 0 {
		item, ok := byName[group]
		if !ok {
			return nil, errors.New(tr("策略组不存在"))
		}
		names = uniqueNodesForWeb(item, groupNames)
	}
	names = uniqueNodeNames(names, groupNames)
	if len(names) == 0 {
		return map[string]remote.DelayResult{}, nil
	}

	prefs := a.Prefs()
	testURL := prefs.TestURL
	timeout := prefs.TestTimeoutMS
	concurrency := prefs.TestConcurrency
	if concurrency < 1 {
		concurrency = 8
	}
	if concurrency > 32 {
		concurrency = 32
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan string)
	results := make(map[string]remote.DelayResult, len(names))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range jobs {
				delay, err := a.k.DelayContext(ctx, name, testURL, timeout)
				mu.Lock()
				if err != nil {
					results[name] = remote.DelayResult{Error: err.Error()}
				} else {
					results[name] = remote.DelayResult{Delay: int(delay)}
				}
				mu.Unlock()
			}
		}()
	}
feed:
	for _, name := range names {
		select {
		case jobs <- name:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return results, err
	}
	return results, nil
}

func (h webHost) Language() string {
	if strings.HasPrefix(currentLocale(), "zh") {
		return "zh-Hans"
	}
	return "en"
}

func joinAddress(ip, port string) string {
	if ip == "" {
		return ""
	}
	if port == "" {
		return ip
	}
	return net.JoinHostPort(ip, port)
}

func uniqueNodesForWeb(group core.Group, groups map[string]bool) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(group.Members))
	for _, name := range group.Members {
		if groups[name] || seen[name] || isBuiltin(name) {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func uniqueNodeNames(names []string, groups map[string]bool) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if groups[name] || seen[name] || isBuiltin(name) {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// webSelect mirrors the desktop action without blocking the UI thread on the
// controller request or on closing each previous connection.
func (a *App) webSelect(group, member string) error {
	a.proxySelectionActive.Add(1)
	defer a.proxySelectionActive.Add(-1)
	a.proxySelectionMu.Lock()
	defer a.proxySelectionMu.Unlock()
	a.bmu.Lock()
	busy := a.kernelBusy || a.svcBusy || a.sysBusy || a.tunChanging || a.subBusy || a.shutdownPending.Load()
	a.bmu.Unlock()
	if busy {
		return errors.New(tr("网络操作进行中，请稍后重试"))
	}
	changed := true
	for _, item := range a.k.Groups() {
		if item.Name == group {
			changed = item.Now != member
			break
		}
	}
	var old []core.Conn
	var snapshotErr error
	if changed && a.Prefs().CloseOnSwitch {
		old, snapshotErr = a.k.ConnectionsWithError()
	}
	if err := a.k.Select(group, member); err != nil {
		return err
	}
	cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	failed := closeProxyConnections(cleanup, old, group, func(id string) error { return a.k.CloseConnectionContext(cleanup, id) })
	cancel()
	runOnMain(func() {
		a.notice = ""
		if snapshotErr != nil {
			a.notice = tr("节点已切换，但无法读取旧连接: ") + snapshotErr.Error()
		}
		if failed != 0 {
			if a.notice != "" {
				a.notice += "；"
			}
			a.notice += fmt.Sprintf(tr("节点已切换，%d 条旧连接未能断开"), failed)
		}
		a.connFetched = time.Time{}
		a.invalidate()
	})
	return nil
}

func (a *App) webSetMode(mode string) error {
	if mode != "rule" && mode != "global" && mode != "direct" {
		return errors.New(tr("无效的路由模式"))
	}
	if a.k == nil || !a.k.Running() {
		return errors.New(tr("内核尚未就绪"))
	}
	var meshMode bool
	var startErr error
	runOnMain(func() {
		if !a.hasMeshRoutes() {
			return
		}
		meshMode = true
		if a.ShuttingDown() {
			startErr = errors.New(tr("应用正在退出"))
			return
		}
		startErr = a.applyMeshMode(mode)
		a.invalidate()
	})
	if meshMode {
		return startErr
	}
	if err := a.k.SetMode(mode); err != nil {
		return err
	}
	runOnMain(func() {
		a.modeToken = mode
		a.modePick = labelFor(mode)
		a.modeIdx = modeIndex(mode)
		a.ov.Mode = mode
		a.notice = ""
		a.saveOverrides()
		a.invalidate()
	})
	return nil
}

type desktopPreviewNode struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
}

type desktopPreviewGroup struct {
	Name                string   `yaml:"name"`
	Type                string   `yaml:"type"`
	Proxies             []string `yaml:"proxies"`
	Use                 []string `yaml:"use"`
	IncludeAll          bool     `yaml:"include-all"`
	IncludeAllProxies   bool     `yaml:"include-all-proxies"`
	IncludeAllProviders bool     `yaml:"include-all-providers"`
	Hidden              bool     `yaml:"hidden"`
	Filter              string   `yaml:"filter"`
	ExcludeFilter       string   `yaml:"exclude-filter"`
	ExcludeType         string   `yaml:"exclude-type"`
}

type desktopPreviewProvider struct {
	Type          string               `yaml:"type"`
	Path          string               `yaml:"path"`
	URL           string               `yaml:"url"`
	Payload       []desktopPreviewNode `yaml:"payload"`
	Filter        string               `yaml:"filter"`
	ExcludeFilter string               `yaml:"exclude-filter"`
	ExcludeType   string               `yaml:"exclude-type"`
	Override      struct {
		Prefix       string `yaml:"additional-prefix"`
		Suffix       string `yaml:"additional-suffix"`
		Replacements []any  `yaml:"proxy-name"`
		Expressions  []any  `yaml:"override-expr"`
	} `yaml:"override"`
}

// previewGroupsOffline statically parses the active profile's composed config
// when the kernel is stopped so users can browse, add, and edit nodes offline.
func (a *App) previewGroupsOffline() []remote.Group {
	var raw []byte
	var global patch.Overrides
	var local patch.ProfileOverrides
	var kernelHome string
	runOnMain(func() {
		global = a.runtimeOverrides()
		global.TunEnable = false
		kernelHome = filepath.Join(a.dir, "mihomo")
		if a.store != nil {
			if id := a.store.ActiveID(); id != "" {
				if body, err := a.store.Content(id); err == nil {
					raw = body
					local = a.profileOverrides(id)
				}
			}
		}
		if len(raw) == 0 && strings.TrimSpace(a.cfgText) != "" {
			raw = []byte(a.cfgText)
		}
	})
	if len(raw) == 0 {
		return []remote.Group{}
	}
	composed, err := patch.ApplyLayers(raw, global, local)
	if err != nil {
		return []remote.Group{}
	}
	return buildOfflineRemoteGroups(composed, kernelHome)
}

func buildOfflineRemoteGroups(raw []byte, kernelHome string) []remote.Group {
	var doc struct {
		Proxies   []desktopPreviewNode              `yaml:"proxies"`
		Groups    []desktopPreviewGroup             `yaml:"proxy-groups"`
		Providers map[string]desktopPreviewProvider `yaml:"proxy-providers"`
	}
	if yaml.Unmarshal(raw, &doc) != nil {
		return []remote.Group{}
	}
	byName := map[string]string{"DIRECT": "Direct", "REJECT": "Reject", "REJECT-DROP": "RejectDrop", "PASS": "Pass", "COMPATIBLE": "Compatible"}
	var all []string
	for _, n := range doc.Proxies {
		byName[n.Name] = desktopPreviewType(n.Type)
		all = append(all, n.Name)
	}
	global := []string{"DIRECT", "REJECT"}
	global = append(global, all...)
	hasGlobal := false
	for _, g := range doc.Groups {
		byName[g.Name] = desktopPreviewType(g.Type)
		global = append(global, g.Name)
		hasGlobal = hasGlobal || g.Name == "GLOBAL"
	}
	var providerOrder []string
	for name := range doc.Providers {
		providerOrder = append(providerOrder, name)
	}
	sort.Strings(providerOrder)
	providerNodes := make(map[string][]desktopPreviewNode)
	for _, name := range providerOrder {
		provider := doc.Providers[name]
		nodes := provider.Payload
		if provider.Type != "inline" {
			path := provider.Path
			if path == "" && provider.Type == "http" {
				path = C.Path.GetPathByHash("proxies", provider.URL)
			}
			if cached, ok := readDesktopPreviewProvider(kernelHome, path); ok {
				nodes = cached
			}
		}
		if len(provider.Override.Replacements) > 0 || len(provider.Override.Expressions) > 0 {
			continue
		}
		matches := desktopPreviewFilter(provider.Filter, provider.ExcludeFilter, provider.ExcludeType)
		for _, n := range nodes {
			if matches(n) {
				n.Name = provider.Override.Prefix + n.Name + provider.Override.Suffix
				providerNodes[name] = append(providerNodes[name], n)
			}
		}
	}
	if !hasGlobal && len(doc.Groups) > 0 {
		doc.Groups = append(doc.Groups, desktopPreviewGroup{Name: "GLOBAL", Type: "select", Proxies: global})
	}
	out := make([]remote.Group, 0, len(doc.Groups))
	for _, source := range doc.Groups {
		g := remote.Group{
			Name:    source.Name,
			Type:    desktopPreviewType(source.Type),
			Hidden:  source.Hidden,
			Global:  source.Name == "GLOBAL",
			Members: []remote.Member{},
		}
		matches := desktopPreviewFilter(source.Filter, source.ExcludeFilter, source.ExcludeType)
		explicitMatches := desktopPreviewFilter("", source.ExcludeFilter, source.ExcludeType)
		seen := map[string]bool{}
		add := func(n desktopPreviewNode) {
			if n.Name != "" && !seen[n.Name] {
				seen[n.Name] = true
				g.Members = append(g.Members, remote.Member{Name: n.Name, Type: desktopPreviewType(n.Type)})
			}
		}
		for _, name := range source.Proxies {
			if typ, ok := byName[name]; ok && explicitMatches(desktopPreviewNode{Name: name, Type: typ}) {
				add(desktopPreviewNode{Name: name, Type: typ})
			}
		}
		if source.IncludeAll || source.IncludeAllProxies {
			for _, name := range all {
				n := desktopPreviewNode{Name: name, Type: byName[name]}
				if matches(n) {
					add(n)
				}
			}
		}
		use := source.Use
		if source.IncludeAll || source.IncludeAllProviders {
			use = providerOrder
		}
		for _, name := range use {
			for _, n := range providerNodes[name] {
				if matches(n) {
					add(n)
				}
			}
		}
		if g.Type == "Selector" && len(g.Members) > 0 {
			g.Now = g.Members[0].Name
		}
		out = append(out, g)
	}
	return out
}

func readDesktopPreviewProvider(kernelHome, path string) ([]desktopPreviewNode, bool) {
	if kernelHome == "" || path == "" {
		return nil, false
	}
	root, err := filepath.EvalSymlinks(kernelHome)
	if err != nil {
		return nil, false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, false
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, false
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > profile.MaxDocumentBytes {
		return nil, false
	}
	raw, err := io.ReadAll(io.LimitReader(f, profile.MaxDocumentBytes+1))
	var doc struct {
		Proxies []desktopPreviewNode `yaml:"proxies"`
	}
	if err != nil || len(raw) > profile.MaxDocumentBytes || yaml.Unmarshal(raw, &doc) != nil || doc.Proxies == nil {
		return nil, false
	}
	return doc.Proxies, true
}

func desktopPreviewFilter(include, exclude, excludeType string) func(desktopPreviewNode) bool {
	compile := func(pattern string) []*regexp2.Regexp {
		var patterns []*regexp2.Regexp
		if pattern == "" {
			return patterns
		}
		for _, part := range strings.Split(pattern, "`") {
			r, err := regexp2.Compile(part, regexp2.None)
			if err != nil {
				continue
			}
			r.MatchTimeout = 10 * time.Millisecond
			patterns = append(patterns, r)
		}
		return patterns
	}
	includePatterns, excludePatterns := compile(include), compile(exclude)
	excludedTypes := strings.Split(excludeType, "|")
	match := func(patterns []*regexp2.Regexp, name string) bool {
		for _, r := range patterns {
			if ok, _ := r.MatchString(name); ok {
				return true
			}
		}
		return false
	}
	return func(n desktopPreviewNode) bool {
		for _, typ := range excludedTypes {
			if typ != "" && desktopPreviewType(typ) == desktopPreviewType(n.Type) {
				return false
			}
		}
		return (include == "" || match(includePatterns, n.Name)) && (exclude == "" || !match(excludePatterns, n.Name))
	}
}

func desktopPreviewType(typ string) string {
	switch strings.ToLower(typ) {
	case "select", "selector":
		return "Selector"
	case "url-test", "urltest":
		return "URLTest"
	case "load-balance", "loadbalance":
		return "LoadBalance"
	case "ss", "shadowsocks":
		return "Shadowsocks"
	case "ssr", "shadowsocksr":
		return "ShadowsocksR"
	case "direct":
		return "Direct"
	case "reject":
		return "Reject"
	case "http":
		return "Http"
	case "fallback":
		return "Fallback"
	case "relay":
		return "Relay"
	default:
		return typ
	}
}
