package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"gopurr/internal/core"
	"gopurr/internal/service"
)

type overviewState struct {
	showOverviewNodes bool
	overviewQuery     string
	overviewList      ui.ListState
	overviewIdentity  string
}

// Use the same group in the overview, quick picker and full proxy page. A
// remembered group must still be visible in the current routing mode.
func selectedProxyGroup(groups []core.Group, name string) (core.Group, bool) {
	for _, g := range groups {
		if g.Name == name {
			return g, true
		}
	}
	for _, g := range groups {
		if manualGroup(g) {
			return g, true
		}
	}
	if len(groups) > 0 {
		return groups[0], true
	}
	return core.Group{}, false
}

// A selected member can itself be a group. Only show a leaf and its delay
// after resolving the chain; cycles and groups without a current member do
// not imply a working exit. Load-balancing groups have no single exit.
func resolveGroupExit(groups []core.Group, group core.Group) (core.Group, string, bool) {
	byName := make(map[string]core.Group, len(groups))
	for _, g := range groups {
		byName[g.Name] = g
	}
	seen := map[string]bool{}
	for {
		kind := strings.ToLower(group.Type)
		if seen[group.Name] || group.Now == "" || kind == "loadbalance" || kind == "load-balance" || kind == "relay" {
			return group, "", false
		}
		seen[group.Name] = true
		next, nested := byName[group.Now]
		if !nested {
			return group, group.Now, true
		}
		group = next
	}
}

func (a *App) overviewPage(c *ui.Context) {
	pal := paletteFor(c)
	snap, hist := a.snapshot()
	if snap.TrafficStale {
		hist = nil
	}
	running := a.k.RunningCached()
	_, height := c.Size()
	compact := height < 660
	all := a.k.Groups()
	group, hasGroup := selectedProxyGroup(visibleGroups(all, a.modeToken), a.proxyGroup)
	if a.modeToken == "direct" {
		hasGroup = false
	}
	if hasGroup {
		a.proxyGroup = group.Name
	}
	a.pageHeader(c, tr("概览"), tr("连接状态与实时网络活动"), func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			a.modeControl(c)
			if !running {
				a.kernelButton(c, false)
			}
		})
	})
	if a.sysOn != a.sysApplied {
		a.setSystemProxy(a.sysOn)
	}
	ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			ui.Row(c).FillWidth().Radius(12).Background(pal.Surface).Children(func() {
				width := float32(292)
				if windowWidth, _ := c.Size(); windowWidth < 960 {
					width = 248
				}
				ui.Column(c).Width(width).AlignSelf(ui.Stretch).Padding(16).Gap(12).
					BorderWidth(0, 1, 0, 0).BorderColor(pal.Grid).Children(func() {
					a.overviewRoute(c, all, group, hasGroup, running)
				})
				traffic := ui.Column(c).Grow(1).MinWidth(0).Padding(16).Gap(8)
				if trafficPeak(hist) > 0 {
					traffic.Draw(func(p *ui.Painter, r ui.Rect) {
						p.FillGradient(r, ui.LinearGradient{From: pal.Up.Alpha(0.02), To: pal.Down.Alpha(0.06), Angle: 135}, 12)
					})
				}
				traffic.Children(func() {
					peak := tr("等待采样")
					if len(hist) > 0 {
						peak = tr("峰值 ") + fmtRate(trafficPeak(hist))
					}
					if snap.TrafficStale && !snap.TrafficAt.IsZero() {
						peak = tr("数据已暂停")
					}
					sectionHeading(c, tr("实时流量"), peak)
					ui.Row(c).FillWidth().Gap(20).Children(func() {
						down, up := fmtRate(snap.DownNow), fmtRate(snap.UpNow)
						if snap.TrafficStale {
							down, up = "—", "—"
						}
						overviewRate(c, iconDown, tr("下行"), down, pal.Down)
						overviewRate(c, iconUp, tr("上行"), up, pal.Up)
					})
					a.chart(c, hist)
					ui.Row(c).FillWidth().Children(func() {
						start := tr("实时更新")
						if snap.TrafficStale {
							start = peak
						}
						if len(hist) > 1 {
							start = fmt.Sprintf(tr("%d 秒前"), len(hist)-1)
						}
						ui.Text(c, start).FontSize(11).TextColor(pal.Faint)
						ui.Spacer(c)
						end := tr("现在")
						if snap.TrafficStale {
							end = tr("等待新数据")
						}
						ui.Text(c, end).FontSize(11).TextColor(pal.Faint)
					})
				})
			})
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				sectionHeading(c, tr("网络控制"), labelFor(a.modeToken)+tr("模式"))
				settingsGroup(c, "", func() { a.overviewNetworkControls(c) })
			})
			ui.Row(c).FillWidth().Gap(16).Children(func() {
				connections := fmt.Sprintf(tr("活跃连接 %d"), snap.Conns)
				if snap.ConnectionsStale {
					connections = tr("活跃连接 —")
				}
				for _, item := range []string{
					connections,
					fmt.Sprintf(tr("生效规则 %d"), len(a.k.Rules())),
					tr("累计下行 ") + fmtBytes(snap.DownTotal),
					tr("累计上行 ") + fmtBytes(snap.UpTotal),
				} {
					ui.Text(c, item).FontSize(11).FontFeatures("tnum").TextColor(pal.Faint)
				}
			})
		})
	})
	if a.confirmInstall {
		title, action := tr("安装系统服务"), tr("安装")
		if installed, _, _ := a.svcState(); installed {
			title, action = tr("修复系统服务"), tr("修复")
			if a.mgr.ServiceNeedsUpdate() || a.mgr.ServiceVersionSkew() {
				title, action = tr("更新系统服务"), tr("更新")
			}
		}
		if softAlertDialog(c, &a.confirmInstall, title, service.InstallDescription(), tr("取消"), action) == 1 {
			a.installService()
		}
	}
	if a.confirmUninstall {
		if softAlertDialog(c, &a.confirmUninstall, tr("卸载系统服务"), service.UninstallDescription(), tr("取消"), tr("卸载")) == 1 {
			a.uninstallService()
		}
	}
	a.overviewNodePicker(c, compact)
	a.networkDetails(c)
}

func (a *App) overviewRoute(c *ui.Context, groups []core.Group, group core.Group, hasGroup, running bool) {
	pal := paletteFor(c)
	status, color := tr("系统代理未开启"), pal.Muted
	switch {
	case busyNetwork(a):
		status, color = tr("正在应用网络设置"), pal.Muted
	case !running:
		status, color = tr("内核尚未就绪"), pal.Warn
	case a.sysErrText() != "":
		status, color = tr("系统代理需要处理"), pal.Danger
	case a.healthResumePending():
		status, color = tr("系统代理等待内核恢复"), pal.Warn
	case a.proxyReadErr != "":
		status, color = tr("网络接管状态未知"), pal.Warn
	case (a.tunOn || a.sysApplied) && !a.networkFresh():
		status, color = tr("正在确认网络接管状态"), pal.Warn
	case a.tunOn && !a.networkRuntime.TunReady:
		status, color = tr("TUN 接管需要处理"), pal.Danger
	case a.sysApplied && (!a.networkRuntime.ProxyReady || a.networkRuntime.MixedPort != a.mixedPort()):
		status, color = tr("本地代理需要处理"), pal.Danger
	case a.tunOn && a.sysApplied:
		status, color = tr("系统代理与 TUN 已开启"), pal.Success
	case a.tunOn:
		status, color = tr("TUN 已开启"), pal.Success
	case a.sysApplied:
		status, color = tr("系统代理已开启"), pal.Success
	}
	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(color).Transition(colorMotion())
		ui.Text(c, status).FontSize(12).TextColor(color).ContentTransition(contentMotion())
	})
	name, caption, detail := tr("未配置代理"), tr("添加订阅后选择代理节点"), tr("暂无延迟记录")
	exitGroup, exit, resolved := resolveGroupExit(groups, group)
	if hasGroup {
		name, caption = group.Now, group.Name+" · "+groupTypeLabel(group.Type)
		if name == "" || !resolved {
			name = groupTypeLabel(group.Type)
		}
		if resolved {
			name = exit
			if d, known := a.memberDelay(exitGroup, exit); known {
				detail = latencyText(d) + tr(" · 最近测试")
			}
			if isBuiltin(exit) {
				detail = tr("当前策略出口")
			}
		}
	}
	if a.modeToken == "direct" {
		name, caption, detail = tr("直连"), tr("所有流量直接访问网络"), tr("不经过代理节点")
	}
	if !running {
		name, caption, detail = tr("等待内核就绪"), tr("就绪后显示当前策略出口"), tr("节点状态暂不可用")
	}
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		// A preference change discards outgoing visual copies as well as the
		// current entrance, so reduced motion updates the exit immediately.
		ui.Column(c).Key(c.Preferences().ReduceMotion).FillWidth().Height(34).Clip().Children(func() {
			ui.Row(c).FillWidth().Height(34).Key(name).Transition(routeMotion()).Children(func() {
				ui.Text(c, name).FillWidth().SingleLine().Ellipsis("…").Tooltip(name).
					FontSize(27).FontWeight(600).TextColor(pal.Text).Label(tr("当前出口 ") + name)
			})
		})
		ui.Text(c, caption).FillWidth().SingleLine().Ellipsis("…").Tooltip(caption).
			FontSize(11).TextColor(pal.Muted).ContentTransition(contentMotion())
	})
	ui.Spacer(c)
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, detail).Grow(1).MinWidth(0).MaxLines(2).FontSize(11).FontFeatures("tnum").TextColor(pal.Muted).ContentTransition(contentMotion())
		if hasGroup && running {
			label := tr("切换节点")
			if !manualGroup(group) {
				label = tr("查看节点")
			}
			softPrimaryButton(c, label).OnClick(func() {
				a.overviewQuery, a.showOverviewNodes, a.proxySelectionError = "", true, ""
			})
		} else if a.modeToken == "direct" || !running {
			softButton(c, tr("查看代理")).OnClick(func() { a.page = "proxies" })
		} else {
			softPrimaryButton(c, tr("添加订阅")).OnClick(func() { a.page, a.showAddSub = "subs", true })
		}
	})
}

func overviewRate(c *ui.Context, icon *ui.SVG, label, value string, accent ui.Color) {
	pal := paletteFor(c)
	ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
		ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).Size(14, 14).TextColor(accent).Role(ui.RoleNone)
			ui.Text(c, label).FontSize(11).TextColor(pal.Muted)
		})
		number, unit := value, ""
		if i := strings.LastIndex(value, " "); i > 0 {
			number, unit = value[:i], value[i+1:]
		}
		// Keep each number with its own unit while crossfading across a unit
		// boundary, instead of sliding the old unit into the new number.
		ui.Row(c).Key(value).AlignItems(ui.Baseline).Gap(4).Transition(ui.ElementTransition{
			Animation: &gentleText, Colors: true, Enter: &ui.Motion{Opacity: 0}, Exit: &ui.Motion{Opacity: 0},
		}).Children(func() {
			ui.Text(c, number).FontSize(27).FontWeight(600).FontFeatures("tnum").TextColor(accent)
			ui.Text(c, unit).FontSize(11).TextColor(pal.Muted)
		})
	})
}

func (a *App) overviewNetworkControls(c *ui.Context) {
	pal := paletteFor(c)
	// While the watchdog holds a resume, the switch shows the user's intent
	// (on) and flipping it off cancels the resume instead of starting a core.
	if a.healthResumePending() && !a.sysResumeSwitch {
		a.cancelHealthResume()
	}
	proxyColor := pal.Muted
	if a.sysErrText() != "" {
		proxyColor = pal.Danger
	}
	overviewControlRow(c, tr("系统代理"), a.sysProxyHint(), proxyColor, func() {
		softButton(c, tr("详情")).Label(tr("系统代理详情")).OnClick(func() { a.showNetworkDetails = true; a.refreshNetworkState() })
		if a.healthResumePending() {
			softSwitch(c, &a.sysResumeSwitch).Disabled(busyNetwork(a)).Label(tr("系统代理开关"))
		} else {
			softSwitch(c, &a.sysOn).Disabled(busyNetwork(a)).Label(tr("系统代理开关"))
		}
	})
	hairline(c)
	installed, busy, svcErr := a.svcState()
	root := a.k.IsRoot()
	if a.tunWanted && root {
		a.tunWanted = false
		a.requestTun(true)
		root = a.k.IsRoot()
	}
	tunColor := pal.Muted
	if svcErr != "" || a.tunErrText() != "" || (a.ov.TunEnable && a.networkFresh() && !a.networkRuntime.TunReady) {
		tunColor = pal.Danger
	}
	overviewControlRow(c, tr("TUN 模式"), a.tunHint(), tunColor, func() {
		switch {
		case busy:
			ui.Text(c, tr("正在处理…")).FontSize(11).TextColor(pal.Muted)
		case !service.Available():
		case !installed:
			softButton(c, tr("安装服务…")).OnClick(func() { a.confirmInstall = true })
		case a.mgr.ServiceNeedsUpdate():
			softButton(c, tr("更新服务…")).Disabled(busyNetwork(a)).OnClick(func() { a.confirmInstall = true })
		case !root || svcErr != "":
			softButton(c, tr("修复服务…")).Disabled(busyNetwork(a)).OnClick(func() { a.confirmInstall = true })
		default:
			softButton(c, tr("详情")).OnClick(func() { a.showNetworkDetails = true })
		}
		softSwitch(c, &a.tunOn).Disabled((!root && !a.tunOn) || busyNetwork(a)).Label(tr("TUN 模式"))
		if a.tunOn != a.ov.TunEnable {
			a.requestTun(a.tunOn)
		}
	})
	hairline(c)
	a.overviewFeatures(c)
}

func (a *App) overviewNodePicker(c *ui.Context, compact bool) {
	pal := paletteFor(c)
	softModal(c, "overview-nodes", &a.showOverviewNodes, func() {
		groups := visibleGroups(a.k.Groups(), a.modeToken)
		group, ok := selectedProxyGroup(groups, a.proxyGroup)
		if !ok || a.modeToken == "direct" {
			a.showOverviewNodes = false
			return
		}
		a.proxyGroup = group.Name
		listHeight := float32(280)
		if compact {
			listHeight = 224 // Four complete rows in the minimum window.
		}
		ui.Column(c).Width(560).Padding(20).Gap(12).Children(func() {
			ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
				label := tr("切换节点")
				if !manualGroup(group) {
					label = tr("查看节点")
				}
				ui.Text(c, label).Grow(1).FontSize(16).FontWeight(600).TextColor(pal.Text)
				names := make([]string, len(groups))
				for i, g := range groups {
					names[i] = g.Name
				}
				ui.Select(c, &a.proxyGroup, names).Width(230).Label(tr("概览策略组"))
			})
			searchRow(c, &a.overviewQuery, tr("搜索要切换的节点…"))
			if !manualGroup(group) {
				ui.Text(c, tr("此组由内核自动调度，勾选表示当前出口。")).FontSize(11).TextColor(pal.Muted)
			}
			if a.proxySelectionError != "" {
				settingsFeedback(c, a.proxySelectionError, true)
			}
			members := filterMembers(group.Members, a.overviewQuery)
			identity := group.Name + "\x00" + a.overviewQuery
			if identity != a.overviewIdentity {
				a.overviewIdentity, a.overviewList = identity, ui.ListState{KeepOffset: true}
			}
			if len(members) == 0 {
				emptyState(c, iconSearch, tr("没有匹配的节点"), tr("试试更短的名称或切换策略组。"))
			} else {
				ui.List(c, &a.overviewList, len(members)).FillWidth().Height(listHeight).Radius(10).Background(pal.Surface).
					ItemKey(func(i int) any { return members[i] }).Rows(func(row ui.ListRow) {
					a.nodeRowAction(row.Context, group, members[row.Index], func() { a.showOverviewNodes = false })
				})
			}
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				softButton(c, tr("管理代理…")).OnClick(func() { a.showOverviewNodes, a.page = false, "proxies" })
				ui.Spacer(c)
				softButton(c, tr("关闭")).OnClick(func() { a.showOverviewNodes = false })
			})
		})
	})
}
