package app

import (
	"fmt"
	"strings"

	"gopurr/internal/core"
	"gopurr/internal/service"
	"gopurr/internal/sysproxy"
)

// TrayMenu takes a new snapshot when the menu opens. It does not depend on
// window services, which may have been released by light mode.
func (a *App) TrayMenu(openWindow func()) *Menu {
	s := a.ReadTrayKernel()
	if !a.ShuttingDown() {
		// Opening the menu is a fresh look: adopt what the core and the OS
		// report, as the window would.
		if s.Running {
			a.modeToken = s.Mode
			a.modePick, a.modeIdx = labelFor(a.modeToken), modeIndex(a.modeToken)
			a.ov.Mode = a.modeToken
		}
		if !busyNetwork(a) {
			if st, err := a.currentSystemProxy(); err == nil && st.Supported {
				a.sysState = st
				a.sysApplied = ownsProxy(st, a.mixedPort())
				a.sysOn = a.sysApplied
			}
		}
	}
	m, _ := a.TrayMenuFor(s, openWindow)
	return m
}

// TrayKernel is what the tray menu reads from the core. Reading it may wait
// on the controller, so a poller fetches it off the UI thread.
type TrayKernel struct {
	Running bool
	Mode    string
	Groups  []core.Group
}

// ReadTrayKernel probes the core for the tray; safe off the UI thread.
func (a *App) ReadTrayKernel() TrayKernel {
	if a.k == nil || a.ShuttingDown() {
		return TrayKernel{}
	}
	s := TrayKernel{Running: a.k.RunningCached()}
	if s.Running {
		s.Mode, s.Groups = a.k.Mode(), a.k.Groups()
	}
	return s
}

// TrayMenuFor builds the tray menu on the UI thread from kernel state read
// beforehand and the app's current state, without changing either: a
// poller's reading may be a moment old. The signature describes everything
// the menu shows, so a caller that must keep a menu attached (Linux panels
// open it themselves) can replace it only when something visible changed.
func (a *App) TrayMenuFor(s TrayKernel, openWindow func()) (*Menu, string) {
	if a.ShuttingDown() {
		items := []*MenuItem{{Label: tr("正在结束当前会话…"), Disabled: true}}
		return NewMenu(items), trayMenuSignature(items)
	}
	items := a.trayItems(s.Running, s.Groups, openWindow)
	return NewMenu(items), trayMenuSignature(items)
}

func trayMenuSignature(items []*MenuItem) string {
	var b strings.Builder
	var walk func([]*MenuItem)
	walk = func(items []*MenuItem) {
		for _, it := range items {
			fmt.Fprintf(&b, "%s\x1f%s\x1f%s\x1f%t%t%t\x1f%s\x1e", it.ID, it.Label, fmt.Sprint(it.Type), it.Checked, it.Disabled, it.Hidden, it.ToolTip)
			if len(it.Submenu) > 0 {
				b.WriteString("[")
				walk(it.Submenu)
				b.WriteString("]")
			}
		}
	}
	walk(items)
	return b.String()
}

func (a *App) trayItems(running bool, groups []core.Group, openWindow func()) []*MenuItem {
	openPage := func(page string) {
		a.page = page
		openWindow()
		a.invalidate()
	}
	_, svcBusy, _ := a.svcState()
	kernelBusy := a.kernelBusyState()
	tunReady := a.k != nil && a.k.IsRoot()
	tunEnabled := (tunReady || a.tunOn) && !svcBusy && !kernelBusy && a.k != nil

	proxyLabel, resumePending := tr("系统代理"), a.healthResumePending()
	if resumePending {
		proxyLabel = tr("系统代理（等待内核恢复）")
	}
	items := []*MenuItem{
		{ID: "show-window", Label: tr("显示窗口"), Click: func(*MenuItem, *Window) { openWindow() }},
		Separator(),
		{
			ID: "system-proxy", Label: proxyLabel, Type: MenuItemCheckbox,
			Checked: a.sysApplied || resumePending, Disabled: !sysproxy.Available() || a.k == nil || svcBusy || kernelBusy,
			Click: func(item *MenuItem, _ *Window) {
				if a.healthResumePending() {
					// Shown on while the watchdog waits: unchecking cancels.
					a.cancelHealthResume()
					item.SetChecked(false)
					a.invalidate()
					return
				}
				a.notice = ""
				if busyNetwork(a) {
					item.SetChecked(a.sysApplied)
					return
				}
				a.requestSystemProxy(item.IsChecked(), true, true, func(err error) {
					item.SetChecked(a.sysApplied)
					if err != nil || a.notice != "" {
						openPage("overview")
					}
				})
			},
		},
		{
			ID: "tun", Label: tr("TUN 模式"), Type: MenuItemCheckbox,
			Checked: a.tunOn, Disabled: !tunEnabled, ToolTip: a.tunHint(),
			Click: func(item *MenuItem, _ *Window) {
				a.requestTun(item.IsChecked(), func() {
					item.SetChecked(a.tunOn)
					if a.tunErrText() != "" {
						openPage("overview")
					}
				})
			},
		},
	}
	if service.Available() && (!tunReady || svcBusy) {
		label := tr("设置 TUN 服务…")
		if svcBusy {
			label = tr("TUN 服务正在变更…")
		}
		items = append(items, &MenuItem{
			ID: "tun-service", Label: label, Disabled: svcBusy || a.k == nil,
			Click: func(*MenuItem, *Window) { openPage("overview") },
		})
	}

	var modes []*MenuItem
	for _, token := range routeTokens {
		modes = append(modes, &MenuItem{
			ID: "mode-" + token, Label: modeLabelFor(token), Type: MenuItemRadio,
			Checked: a.modeToken == token, Disabled: a.k == nil || svcBusy || kernelBusy,
			Click: func(*MenuItem, *Window) {
				a.UseMode(token)
				for i, mode := range modes {
					mode.SetChecked(a.modeToken == routeTokens[i])
				}
				a.invalidate()
				if a.notice != "" {
					openPage("overview")
				}
			},
		})
	}
	nodes := a.trayNodeItems(running, groups, svcBusy || kernelBusy, openPage)
	reloadLabel := tr("重新加载配置")
	switch {
	case kernelBusy:
		reloadLabel = tr("正在启动…")
	case !running:
		reloadLabel = tr("启动内核")
	}
	items = append(items,
		Separator(),
		&MenuItem{ID: "routing-mode", Label: tr("路由模式"), Submenu: modes},
		&MenuItem{ID: "select-node", Label: tr("选择节点"), Submenu: nodes},
		&MenuItem{ID: "mesh", Label: tr("设备组网"), Submenu: a.meshTrayItems(openPage)},
		Separator(),
		&MenuItem{
			ID: "reload", Label: reloadLabel, Disabled: a.k == nil || svcBusy || kernelBusy,
			Click: func(*MenuItem, *Window) {
				a.Reload()
				a.invalidate()
				if a.notice != "" {
					openPage("overview")
				}
			},
		},
		a.checkUpdatesMenuItem(openWindow),
		Separator(),
		&MenuItem{ID: "quit", Label: tr("退出 Gopurr"), Click: func(*MenuItem, *Window) { a.requestQuit(false) }},
	)
	return items
}

func (a *App) trayNodeItems(running bool, groups []core.Group, busy bool, openPage func(string)) []*MenuItem {
	var items []*MenuItem
	switch {
	case !running:
		items = append(items, &MenuItem{Label: tr("启动内核后可选择节点"), Disabled: true})
	case a.modeToken == "direct":
		items = append(items, &MenuItem{Label: tr("直连模式无需选择节点"), Disabled: true})
	default:
		for _, group := range visibleGroups(groups, a.modeToken) {
			members := filterMembers(group.Members, "")
			var nodes []*MenuItem
			if !manualGroup(group) {
				nodes = append(nodes,
					&MenuItem{Label: groupTypeLabel(group.Type) + tr(" · 由内核自动调度"), Disabled: true},
					Separator(),
				)
			}
			for _, member := range members {
				nodes = append(nodes, &MenuItem{
					Label: trayLabel(member, 32), ToolTip: member, Type: MenuItemRadio,
					Checked: member == group.Now, Disabled: busy || !manualGroup(group),
					Click: func(*MenuItem, *Window) {
						a.requestProxySelection(group.Name, member, func(ok bool) {
							if !ok {
								openPage("proxies")
							}
						})
					},
				})
			}
			if len(members) == 0 {
				nodes = append(nodes, &MenuItem{Label: tr("此策略组暂无节点"), Disabled: true})
			}
			label := trayLabel(group.Name, 18)
			if group.Now != "" {
				label += " · " + trayLabel(group.Now, 18)
			}
			items = append(items, &MenuItem{
				Label: label, ToolTip: group.Name + " → " + group.Now, Submenu: nodes,
			})
		}
		if len(items) == 0 {
			items = append(items, &MenuItem{Label: tr("暂无可选策略组"), Disabled: true})
		}
	}
	return append(items, Separator(), &MenuItem{
		ID: "open-proxies", Label: tr("打开代理页面…"),
		Click: func(*MenuItem, *Window) { openPage("proxies") },
	})
}

// Bound native menu widths without changing the names sent to the controller.
func trayLabel(name string, limit int) string {
	name = strings.Join(strings.Fields(name), " ")
	runes := []rune(name)
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return name
}
