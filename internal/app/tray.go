package app

import (
	"strings"

	"github.com/egoist/mygo"

	"gopurr/internal/core"
	"gopurr/internal/service"
	"gopurr/internal/sysproxy"
)

// TrayMenu takes a new snapshot when the menu opens. It does not depend on
// window services, which may have been released by light mode.
func (a *App) TrayMenu(openWindow func()) *mygo.Menu {
	if a.ShuttingDown() {
		return mygo.NewMenu([]*mygo.MenuItem{{Label: "正在结束当前会话…", Disabled: true}})
	}
	running := a.k != nil && a.k.Running()
	var groups []core.Group
	if running {
		a.modeToken = a.k.Mode()
		a.modePick, a.modeIdx = labelFor(a.modeToken), modeIndex(a.modeToken)
		a.ov.Mode = a.modeToken
		groups = a.k.Groups()
	}
	if st, err := sysproxy.Current(); err == nil && st.Supported {
		a.sysState = st
		a.sysApplied = ownsProxy(st, a.mixedPort())
		a.sysOn = a.sysApplied
	}
	return mygo.NewMenu(a.trayItems(running, groups, openWindow))
}

func (a *App) trayItems(running bool, groups []core.Group, openWindow func()) []*mygo.MenuItem {
	openPage := func(page string) {
		a.page = page
		openWindow()
		a.invalidate()
	}
	_, svcBusy, _ := a.svcState()
	tunReady := a.k != nil && a.k.IsRoot()
	tunEnabled := (tunReady || a.tunOn) && !svcBusy && a.k != nil

	items := []*mygo.MenuItem{
		{ID: "show-window", Label: "显示窗口", Click: func(*mygo.MenuItem, *mygo.Window) { openWindow() }},
		mygo.Separator(),
		{
			ID: "system-proxy", Label: "系统代理", Type: mygo.MenuItemCheckbox,
			Checked: a.sysApplied, Disabled: !sysproxy.Available() || a.k == nil || svcBusy,
			Click: func(item *mygo.MenuItem, _ *mygo.Window) {
				a.notice = ""
				a.setSystemProxy(item.IsChecked())
				item.SetChecked(a.sysApplied)
				a.invalidate()
				if a.sysErrText() != "" || a.notice != "" {
					openPage("overview")
				}
			},
		},
		{
			ID: "tun", Label: "TUN 模式", Type: mygo.MenuItemCheckbox,
			Checked: a.tunOn, Disabled: !tunEnabled, ToolTip: a.tunHint(),
			Click: func(item *mygo.MenuItem, _ *mygo.Window) {
				a.applyTun(item.IsChecked())
				item.SetChecked(a.tunOn)
				a.invalidate()
				if a.tunErrText() != "" {
					openPage("overview")
				}
			},
		},
	}
	if service.Available() && (!tunReady || svcBusy) {
		label := "设置 TUN 服务…"
		if svcBusy {
			label = "TUN 服务正在变更…"
		}
		items = append(items, &mygo.MenuItem{
			ID: "tun-service", Label: label, Disabled: svcBusy || a.k == nil,
			Click: func(*mygo.MenuItem, *mygo.Window) { openPage("overview") },
		})
	}

	var modes []*mygo.MenuItem
	for _, token := range routeTokens {
		modes = append(modes, &mygo.MenuItem{
			ID: "mode-" + token, Label: labelFor(token) + "模式", Type: mygo.MenuItemRadio,
			Checked: a.modeToken == token, Disabled: a.k == nil || svcBusy,
			Click: func(*mygo.MenuItem, *mygo.Window) {
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
	nodes := a.trayNodeItems(running, groups, svcBusy, openPage)
	reloadLabel := "重新加载配置"
	if !running {
		reloadLabel = "启动内核"
	}
	items = append(items,
		mygo.Separator(),
		&mygo.MenuItem{ID: "routing-mode", Label: "路由模式", Submenu: modes},
		&mygo.MenuItem{ID: "select-node", Label: "选择节点", Submenu: nodes},
		&mygo.MenuItem{ID: "mesh", Label: "设备组网", Submenu: a.meshTrayItems(openPage)},
		mygo.Separator(),
		&mygo.MenuItem{
			ID: "reload", Label: reloadLabel, Disabled: a.k == nil || svcBusy,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.Reload()
				a.invalidate()
				if a.notice != "" {
					openPage("overview")
				}
			},
		},
		&mygo.MenuItem{ID: "check-updates", Label: "检查软件更新…", Click: func(*mygo.MenuItem, *mygo.Window) {
			a.initSoftwareUpdates()
			openPage("settings")
			a.settingsTab = 3
			a.softwareUpdates.Check(true)
		}},
		mygo.Separator(),
		&mygo.MenuItem{ID: "quit", Label: "退出 Gopurr", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	)
	return items
}

func (a *App) trayNodeItems(running bool, groups []core.Group, busy bool, openPage func(string)) []*mygo.MenuItem {
	var items []*mygo.MenuItem
	switch {
	case !running:
		items = append(items, &mygo.MenuItem{Label: "启动内核后可选择节点", Disabled: true})
	case a.modeToken == "direct":
		items = append(items, &mygo.MenuItem{Label: "直连模式无需选择节点", Disabled: true})
	default:
		for _, group := range visibleGroups(groups, a.modeToken) {
			members := filterMembers(group.Members, "")
			var nodes []*mygo.MenuItem
			if !manualGroup(group) {
				nodes = append(nodes,
					&mygo.MenuItem{Label: groupTypeLabel(group.Type) + " · 由内核自动调度", Disabled: true},
					mygo.Separator(),
				)
			}
			for _, member := range members {
				nodes = append(nodes, &mygo.MenuItem{
					Label: trayLabel(member, 32), ToolTip: member, Type: mygo.MenuItemRadio,
					Checked: member == group.Now, Disabled: busy || !manualGroup(group),
					Click: func(*mygo.MenuItem, *mygo.Window) {
						if !a.selectProxy(group.Name, member) {
							for _, node := range nodes {
								node.SetChecked(node.ToolTip == group.Now)
							}
							openPage("proxies")
						}
					},
				})
			}
			if len(members) == 0 {
				nodes = append(nodes, &mygo.MenuItem{Label: "此策略组暂无节点", Disabled: true})
			}
			label := trayLabel(group.Name, 18)
			if group.Now != "" {
				label += " · " + trayLabel(group.Now, 18)
			}
			items = append(items, &mygo.MenuItem{
				Label: label, ToolTip: group.Name + " → " + group.Now, Submenu: nodes,
			})
		}
		if len(items) == 0 {
			items = append(items, &mygo.MenuItem{Label: "暂无可选策略组", Disabled: true})
		}
	}
	return append(items, mygo.Separator(), &mygo.MenuItem{
		ID: "open-proxies", Label: "打开代理页面…",
		Click: func(*mygo.MenuItem, *mygo.Window) { openPage("proxies") },
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
