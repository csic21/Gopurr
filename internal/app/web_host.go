package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"gopurr/internal/buildinfo"
	"gopurr/internal/core"
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
		return []remote.Group{}, nil
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
	if len(names) > 100 {
		names = names[:100]
	}

	prefs := a.Prefs()
	testURL := prefs.TestURL
	timeout := prefs.TestTimeoutMS
	concurrency := prefs.TestConcurrency
	if concurrency < 1 {
		concurrency = 4
	}
	if concurrency > 8 {
		concurrency = 8
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
