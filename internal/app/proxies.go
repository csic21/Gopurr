package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"gopurr/internal/core"
)

type proxyState struct {
	modeIdx       int
	proxyGroup    string
	proxySort     string
	proxyOnlyLive bool
	nodeList      ui.ListState
	listIdentity  string

	// Measurement state is protected by App.bmu.
	testCancel   context.CancelFunc
	testRun      uint64
	testDone     int
	testTotal    int
	testOK       int
	testTargets  map[string]bool
	delayErrors  map[string]string
	testFinished time.Time
	testSummary  string
}

var routeTokens = []string{"rule", "global", "direct"}

func modeIndex(token string) int {
	for i, v := range routeTokens {
		if token == v {
			return i
		}
	}
	return 0
}

func (a *App) modeControl(c *ui.Context) {
	softSegmented(c, &a.modeIdx, "规则", "全局", "直连")
	if a.modeIdx >= 0 && a.modeIdx < len(routeTokens) && routeTokens[a.modeIdx] != a.modeToken {
		a.UseMode(routeTokens[a.modeIdx])
	}
}

func visibleGroups(groups []core.Group, mode string) []core.Group {
	var out []core.Group
	for _, g := range groups {
		if g.Hidden {
			continue
		}
		if (mode == "global") == (g.Name == "GLOBAL") {
			out = append(out, g)
		}
	}
	return out
}

// Each leaf is counted and tested once even when several groups reference it.
func uniqueNodes(groups []core.Group) []string {
	groupNames := map[string]bool{}
	for _, g := range groups {
		groupNames[g.Name] = true
	}
	seen := map[string]bool{}
	var nodes []string
	for _, g := range groups {
		for _, m := range g.Members {
			if groupNames[m] || isBuiltin(m) || seen[m] {
				continue
			}
			seen[m] = true
			nodes = append(nodes, m)
		}
	}
	return nodes
}

func isBuiltin(name string) bool {
	switch name {
	case "DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE":
		return true
	}
	return false
}

func groupTypeLabel(kind string) string {
	switch strings.ToLower(kind) {
	case "selector":
		return "手动选择"
	case "urltest", "url-test":
		return "自动选择"
	case "fallback":
		return "故障转移"
	case "loadbalance", "load-balance":
		return "负载均衡"
	case "relay":
		return "链式代理"
	default:
		return kind
	}
}

func manualGroup(g core.Group) bool { return strings.EqualFold(g.Type, "Selector") }

func (a *App) proxiesPage(c *ui.Context) {
	pal := paletteFor(c)
	running := a.k.Running()
	var all []core.Group
	if running {
		all = a.k.Groups()
	}
	groups := visibleGroups(all, a.modeToken)
	nodes := uniqueNodes(all)
	hint := "按规则为不同应用与网站分流"
	if a.modeToken == "global" {
		hint = "全部连接使用 GLOBAL 的出口"
	} else if a.modeToken == "direct" {
		hint = "全部连接直接访问网络"
	}
	a.pageHeader(c, "代理", fmt.Sprintf("%d 个策略组 · %d 个节点", len(visibleGroups(all, "rule")), len(nodes)), func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Children(func() { a.modeControl(c) }).Tooltip(hint)
			if !running {
				a.kernelButton(c, false)
				return
			}
			softButton(c, "测试全部").Disabled(a.testingGroup() != "" || len(nodes) == 0).
				OnClick(func() { a.testGroup("全部节点", nodes) })
		})
	})

	a.delayProgress(c)
	if !running {
		emptyState(c, "内核尚未就绪", "应用会自动启动内核。检查上方错误提示后，可以重试启动。")
		return
	}
	if a.modeToken == "direct" {
		emptyState(c, "当前为直连模式", "所有流量直接连接。切换到规则或全局模式后选择代理节点。")
		return
	}
	if len(groups) == 0 {
		emptyState(c, "当前配置没有可用的策略组", "前往订阅导入配置，或检查运行配置中的 proxy-groups。")
		return
	}

	selected, _ := selectedProxyGroup(groups, a.proxyGroup)
	a.proxyGroup = selected.Name
	ui.Row(c).FillWidth().Grow(1).MinHeight(0).Gap(14).Children(func() {
		ui.Column(c).Width(172).FillHeight().Gap(12).Children(func() {
			ui.Text(c, "策略组").FontSize(11.5).FontWeight(600).TextColor(pal.Faint).PaddingX(8)
			// Keep the scroll thumb and its drag target outside the group cards.
			ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Padding(0, 16, 0, 0).Label("策略组列表").Children(func() {
				ui.Column(c).FillWidth().Gap(4).Children(func() {
					for _, item := range groups {
						g := item
						on := g.Name == selected.Name
						bg := ui.Transparent
						if on {
							bg = pal.Selected
						}
						item := ui.Column(c).FillWidth().Padding(10, 12).Radius(8).Gap(4).
							Background(bg).Transition(colorMotion()).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).
							Label("策略组 " + g.Name).Tooltip(g.Name + " → " + g.Now + " · " + groupTypeLabel(g.Type)).
							OnClick(func() { a.proxyGroup = g.Name })
						if !on && item.Hovered() {
							item.Background(pal.Hover)
						}
						item.Children(func() {
							ui.Text(c, g.Name).FillWidth().MaxLines(1).Ellipsis("…").FontSize(13).FontWeight(600).TextColor(pal.Text)
							ui.Text(c, g.Now).FillWidth().MaxLines(1).Ellipsis("…").FontSize(11.5).TextColor(pal.Muted)
						})
					}
				})
			})
		})
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Gap(10).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
					ui.Text(c, selected.Name).FillWidth().MaxLines(1).Ellipsis("…").Tooltip(selected.Name).FontSize(16).FontWeight(600).TextColor(pal.Text)
					ui.Text(c, fmt.Sprintf("%s · %d 个成员", groupTypeLabel(selected.Type), len(selected.Members))).FontSize(11).TextColor(pal.Muted)
				})
				softPrimaryButton(c, "测试本组").Disabled(a.testingGroup() != "").OnClick(func() {
					a.testGroup(selected.Name, selected.Members)
				})
			})
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				searchRow(c, &a.proxyQuery, "搜索节点…").Grow(1).MinWidth(0)
				if a.proxySort == "" {
					a.proxySort = "订阅顺序"
				}
				ui.Select(c, &a.proxySort, []string{"订阅顺序", "延迟优先", "名称排序"}).Width(108)
				softSwitch(c, &a.proxyOnlyLive).Label("仅显示可用节点")
				ui.Text(c, "仅可用").FontSize(11).TextColor(pal.Muted)
			})
			if !manualGroup(selected) {
				ui.Text(c, "此组由内核自动调度，勾选表示当前出口。").FontSize(11).TextColor(pal.Muted)
			}
			members := a.sortedMembers(selected)
			if len(members) == 0 {
				emptyState(c, "没有匹配的节点", "调整搜索或关闭“仅可用”；未测试的节点不算已确认可用。")
				return
			}
			identity := selected.Name + "\x00" + a.proxyQuery + "\x00" + a.proxySort + fmt.Sprint(a.proxyOnlyLive)
			if a.listIdentity != identity {
				a.nodeList = ui.ListState{KeepOffset: true}
				a.listIdentity = identity
			}
			ui.List(c, &a.nodeList, len(members)).FillWidth().Grow(1).MinHeight(0).
				Radius(10).Background(pal.Surface).
				ItemKey(func(i int) any { return members[i] }).Rows(func(row ui.ListRow) {
				a.nodeRow(row.Context, selected, members[row.Index])
			})
			ui.Text(c, fmt.Sprintf("显示 %d / %d · 延迟反映连通性与响应时间", len(members), len(selected.Members))).
				FontSize(11).TextColor(pal.Faint).Label("节点列表统计")
		})
	})
}

func filterMembers(members []string, query string) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]string, 0, len(members))
	seen := map[string]bool{}
	for _, m := range members {
		if !seen[m] && (q == "" || strings.Contains(strings.ToLower(m), q)) {
			out = append(out, m)
			seen[m] = true
		}
	}
	return out
}

func (a *App) memberDelay(g core.Group, name string) (uint16, bool) {
	if d, ok := a.delayOf(name); ok {
		return d, true
	}
	d, ok := g.Delays[name]
	return d, ok
}

func (a *App) sortedMembers(g core.Group) []string {
	filtered := filterMembers(g.Members, a.proxyQuery)
	type entry struct {
		name  string
		delay uint16
		known bool
	}
	items := make([]entry, 0, len(filtered))
	for _, name := range filtered {
		d, known := a.memberDelay(g, name)
		if a.proxyOnlyLive && (!known || d == 0) {
			continue
		}
		items = append(items, entry{name, d, known})
	}
	if a.proxySort == "名称排序" {
		sort.SliceStable(items, func(i, j int) bool { return strings.ToLower(items[i].name) < strings.ToLower(items[j].name) })
	} else if a.proxySort == "延迟优先" {
		rank := func(e entry) int {
			if !e.known {
				return 100000
			}
			if e.delay == 0 {
				return 100001
			}
			return int(e.delay)
		}
		sort.SliceStable(items, func(i, j int) bool { return rank(items[i]) < rank(items[j]) })
	}
	for i, e := range items {
		filtered[i] = e.name
	}
	return filtered[:len(items)]
}

func (a *App) nodeRow(c *ui.Context, group core.Group, member string) {
	a.nodeRowAction(c, group, member, nil)
}

func (a *App) nodeRowAction(c *ui.Context, group core.Group, member string, selected func()) {
	pal := paletteFor(c)
	on := member == group.Now
	row := ui.Row(c).FillWidth().Height(56).Padding(8, 12).
		Background(pal.Surface).BorderWidth(0, 0, 1, 0).BorderColor(pal.Grid).AlignItems(ui.Center).Gap(12).Transition(colorMotion())
	if on {
		row.Background(pal.Selected)
	} else if row.Hovered() {
		row.Background(pal.Hover)
	}
	row.Children(func() {
		mark, color := iconCircle, pal.Faint
		if on {
			mark, color = iconCheck, pal.Accent
		}
		ui.Icon(c, mark).Size(17, 17).TextColor(color).Role(ui.RoleNone).ContentTransition(contentMotion())
		name := ui.Column(c).Grow(1).MinWidth(0).Gap(3).Tooltip(member + " · " + group.MemberTypes[member])
		if manualGroup(group) {
			name.Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label("选择 " + member).OnClick(func() {
				if a.selectProxy(group.Name, member) && selected != nil {
					selected()
				}
			})
		}
		name.Children(func() {
			ui.Text(c, member).FillWidth().MaxLines(1).Ellipsis("…").FontSize(13).FontWeight(500).TextColor(pal.Text)
			kind := group.MemberTypes[member]
			if member == "DIRECT" {
				kind = "直连"
			}
			if member == "REJECT" || member == "REJECT-DROP" {
				kind = "拦截"
			}
			if kind == "" {
				kind = "代理节点"
			}
			if on {
				ui.Text(c, "当前使用 · "+groupTypeLabel(kind)).FontSize(11).TextColor(pal.Muted)
			} else if isBuiltin(member) || groupTypeLabel(kind) != kind {
				ui.Text(c, groupTypeLabel(kind)).FontSize(11).TextColor(pal.Faint)
			}
		})
		d, known := a.memberDelay(group, member)
		label, color := "未测试", pal.Faint
		if known {
			label, color = latencyText(d), latencyColor(c, d)
		}
		if isBuiltin(member) && member != "DIRECT" {
			label = "—"
		}
		a.bmu.Lock()
		errorText := a.delayErrors[member]
		a.bmu.Unlock()
		ui.Row(c).Width(72).Justify(ui.End).Tooltip(errorText).Children(func() {
			ui.Text(c, label).FontSize(12).FontFeatures("tnum").TextColor(color).ContentTransition(contentMotion())
		})
		iconButton(c, iconRefresh, "测试 "+member).Disabled(a.testingGroup() != "" || (isBuiltin(member) && member != "DIRECT")).
			Tooltip("测试 " + member + " 的连通性与延迟").OnClick(func() { a.testGroup(member, []string{member}) })
	})
}

// selectProxy is shared by the window and tray so failures and cache updates
// behave the same when the main window has been released.
func (a *App) selectProxy(group, member string) bool {
	var old []core.Conn
	var snapshotErr error
	changed := true
	for _, g := range a.k.Groups() {
		if g.Name == group {
			changed = g.Now != member
			break
		}
	}
	if changed && a.Prefs().CloseOnSwitch {
		old, snapshotErr = a.k.ConnectionsWithError()
	}
	if err := a.k.Select(group, member); err != nil {
		a.notice = "切换节点失败: " + err.Error()
		a.invalidate()
		return false
	}
	a.notice = ""
	if snapshotErr != nil {
		a.notice = "节点已切换，但无法读取旧连接: " + snapshotErr.Error()
	}
	for _, cn := range old {
		if !containsString(cn.Chains, group) {
			continue
		}
		if err := a.k.CloseConnection(cn.ID); err != nil {
			a.notice = "节点已切换，部分旧连接未能断开: " + err.Error()
		}
	}
	a.connFetched = time.Time{}
	a.invalidate()
	return true
}

func latencyText(d uint16) string {
	if d == 0 {
		return "不可用"
	}
	return fmt.Sprintf("%d ms", d)
}

func latencyColor(c *ui.Context, d uint16) ui.Color {
	pal := paletteFor(c)
	switch {
	case d == 0:
		return pal.Danger
	case d < 200:
		return pal.Success
	case d < 500:
		return pal.Warn
	default:
		return pal.Danger
	}
}

func (a *App) delayProgress(c *ui.Context) {
	pal := paletteFor(c)
	a.bmu.Lock()
	name, done, total, ok := a.testing, a.testDone, a.testTotal, a.testOK
	summary := a.testSummary
	a.bmu.Unlock()
	if name == "" && summary == "" {
		return
	}
	ui.Column(c).FillWidth().Padding(12).Radius(10).Background(pal.Surface).Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			label := summary
			if name != "" {
				label = fmt.Sprintf("正在测试 %s · %d / %d · %d 可用", name, done, total, ok)
			}
			ui.Text(c, label).FillWidth().FontSize(11).TextColor(pal.Muted)
			if name != "" {
				softButton(c, "取消测试").OnClick(a.cancelDelayTest)
			}
		})
		if name != "" && total > 0 {
			ui.Progress(c, float64(done)/float64(total)).FillWidth().Height(4)
		}
	})
}

// testGroup measures unique members with a bounded worker pool. Cancellation
// stops both queued work and requests already running in the controller.
func (a *App) testGroup(name string, members []string) {
	var targets []string
	for _, member := range filterMembers(members, "") {
		if !isBuiltin(member) || member == "DIRECT" {
			targets = append(targets, member)
		}
	}
	if len(targets) == 0 || a.k == nil || !a.k.Running() {
		return
	}
	p := a.Prefs()
	ctx, cancel := context.WithCancel(context.Background())
	a.bmu.Lock()
	if a.testing != "" {
		a.bmu.Unlock()
		cancel()
		return
	}
	a.testRun++
	run := a.testRun
	a.testing, a.testCancel = name, cancel
	a.testDone, a.testOK, a.testTotal = 0, 0, len(targets)
	a.testSummary = ""
	a.bmu.Unlock()
	go func() {
		defer cancel()
		jobs := make(chan string)
		var wg sync.WaitGroup
		workers := p.TestConcurrency
		if workers < 1 {
			workers = 8
		}
		if workers > 32 {
			workers = 32
		}
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for member := range jobs {
					if ctx.Err() != nil {
						return
					}
					d, err := a.k.DelayContext(ctx, member, p.TestURL, p.TestTimeoutMS)
					if ctx.Err() != nil {
						return
					}
					if err != nil {
						d = 0
					}
					a.bmu.Lock()
					if a.testRun == run {
						if a.delays == nil {
							a.delays = map[string]uint16{}
						}
						if a.delayErrors == nil {
							a.delayErrors = map[string]string{}
						}
						a.delays[member] = d
						if err != nil {
							a.delayErrors[member] = err.Error()
						} else {
							delete(a.delayErrors, member)
						}
						a.testDone++
						if d > 0 {
							a.testOK++
						}
					}
					a.bmu.Unlock()
					a.invalidate()
				}
			}()
		}
	feed:
		for _, member := range targets {
			select {
			case jobs <- member:
			case <-ctx.Done():
				break feed
			}
		}
		close(jobs)
		wg.Wait()
		a.bmu.Lock()
		if a.testRun == run {
			a.testSummary = fmt.Sprintf("测试完成 · %d 个可用 / %d 个节点 · %s", a.testOK, a.testTotal, time.Now().Format("15:04"))
			a.testing, a.testCancel = "", nil
			a.testFinished = time.Now()
		}
		a.bmu.Unlock()
		a.invalidate()
	}()
}

func (a *App) cancelDelayTest() {
	a.bmu.Lock()
	if a.testCancel != nil {
		a.testCancel()
		a.testSummary = fmt.Sprintf("测试已取消 · 已完成 %d / %d", a.testDone, a.testTotal)
		a.testRun++
		a.testing, a.testCancel = "", nil
	}
	a.bmu.Unlock()
	a.invalidate()
}

func (a *App) delayOf(name string) (uint16, bool) {
	a.bmu.Lock()
	defer a.bmu.Unlock()
	d, ok := a.delays[name]
	return d, ok
}

func (a *App) testingGroup() string {
	a.bmu.Lock()
	defer a.bmu.Unlock()
	return a.testing
}
