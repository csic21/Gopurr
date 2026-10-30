package app

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"gopurr/internal/mesh"
)

type meshClient interface {
	Snapshot() mesh.Snapshot
	Start()
	Refresh()
	Do(string)
	Shutdown() error
	ClearHistory()
	SetServices(string, bool)
	SetDeviceBlocked(string, bool)
	SetAutoConnect(bool)
	Probe(context.Context, string, string, uint16) mesh.ProbeResult
}

type meshState struct {
	meshClient                                                    meshClient
	meshTab                                                       int
	meshQuery                                                     string
	meshList                                                      ui.ListState
	meshSelected                                                  mesh.Device
	meshShowDevice, meshConfirmAllow                              bool
	meshRouteKey, meshRouteError                                  string
	meshReservedPort                                              atomic.Uint32
	meshServicesDraft, meshServicesError                          string
	meshEditServices                                              bool
	meshAdvancedServices                                          bool
	meshGuide, meshGuideAfterServices                             bool
	meshInvite                                                    bool
	meshInviteError                                               string
	meshPrepareConfirm, meshPreparePending                        bool
	meshPrepareError, meshAuthError, meshOpenedAuthURL            string
	meshPrepareReturn                                             string
	meshOpenAuth                                                  bool
	meshShortcutsReady                                            bool
	meshShortcuts                                                 map[string]mesh.Shortcut
	meshQuick                                                     bool
	meshQuickDevice                                               mesh.Device
	meshQuickKind                                                 int
	meshQuickPort, meshQuickUser, meshQuickError, meshQuickNotice string
	meshLaunchGeneration                                          uint64
	meshQuickRemember                                             bool
	meshProbeMu                                                   sync.Mutex
	meshProbeBusy                                                 bool
	meshProbeSerial                                               uint64
	meshProbeCancel                                               context.CancelFunc
	meshProbeResult                                               mesh.ProbeResult
	meshKernelChecked                                             time.Time
	meshSSHLaunch                                                 func(mesh.Device, mesh.Shortcut) error
	meshKernelRunning                                             bool
}

func (a *App) meshRuntimeRoutes() ([]string, string, string) {
	if a.meshClient == nil {
		return nil, "", ""
	}
	s := a.meshClient.Snapshot()
	if !s.Managed || !s.Status.Official || !s.Status.Running {
		return nil, "", ""
	}
	var ips []string
	for _, d := range s.Status.Peers {
		for _, ip := range d.IPs {
			ips = append(ips, ip.String())
		}
	}
	sort.Strings(ips)
	return ips, s.Status.ProxyAddress, s.Status.ProxyPassword
}

func (a *App) reconcileMeshRoutes() {
	if a.shutdownPending.Load() || a.meshClient == nil || a.k == nil || !a.k.Running() {
		return
	}
	if s := a.meshClient.Snapshot(); !s.Started || s.Busy {
		return
	}
	ips, addr, password := a.meshRuntimeRoutes()
	key := strings.Join(ips, ",") + addr + password
	if key == a.meshRouteKey {
		return
	}
	if a.startKernel() {
		a.meshRouteKey, a.meshRouteError = key, ""
	} else {
		a.meshRouteError = "内置组网路由更新失败：" + a.notice
	}
}

func (a *App) meshTrayItems(openPage func(string)) []*mygo.MenuItem {
	items := []*mygo.MenuItem{{ID: "mesh-open", Label: "打开组网", Click: func(*mygo.MenuItem, *mygo.Window) { openPage("mesh") }}}
	if a.meshClient == nil {
		return items
	}
	s := a.meshClient.Snapshot()
	if !s.Status.Installed {
		return items
	}
	status := "入站防护待确认"
	if s.Status.ShieldsKnown {
		if s.Status.ShieldsUp {
			status = "本机入站访问已关闭"
		} else {
			status = "本机入站访问已开放"
		}
	}
	items = append(items,
		&mygo.MenuItem{Label: status, Disabled: true},
		&mygo.MenuItem{ID: "mesh-block", Label: "关闭本机组网访问", Click: func(*mygo.MenuItem, *mygo.Window) { a.meshClient.Do("block") }},
		&mygo.MenuItem{ID: "mesh-disconnect", Label: "断开组网", Click: func(*mygo.MenuItem, *mygo.Window) { a.meshClient.Do("disconnect") }})
	if !s.Status.Running {
		label := "连接组网"
		if s.Status.AuthURL != "" {
			label = "继续登录组网…"
		}
		items = append(items, &mygo.MenuItem{ID: "mesh-connect", Label: label, Disabled: s.Busy, Click: func(*mygo.MenuItem, *mygo.Window) {
			openPage("mesh")
			if s.Status.AuthURL == "" {
				a.beginMeshConnect()
			}
		}})
	}
	return items
}

func meshOwner(d mesh.Device) string {
	if d.Login != "" {
		if d.Owner != "" && d.Owner != d.Login {
			return d.Owner + " · " + d.Login
		}
		return d.Login
	}
	if d.Owner != "" {
		return d.Owner
	}
	return "账号身份未知"
}

func meshLabel(s mesh.Status) string {
	switch {
	case !s.Installed:
		return "内置组网尚未连接"
	case s.State == "NeedsMachineAuth":
		return "等待管理员批准设备"
	case s.State == "NeedsLogin":
		return "等待登录 Tailscale"
	case s.State == "Starting":
		return "正在连接"
	case s.Running && !s.Official:
		return "当前使用其他组网服务"
	case s.Running:
		return "已连接 Tailscale"
	case s.State == "Stopped":
		return "组网已断开"
	default:
		return "内置组网已就绪"
	}
}

func (a *App) meshPage(c *ui.Context) {
	if a.meshClient == nil {
		return
	}
	a.meshClient.Start()
	a.loadMeshShortcuts()
	a.reconcileMeshPreparation()
	snap := a.meshClient.Snapshot()
	s := snap.Status
	pal := paletteFor(c)
	a.pageHeader(c, "组网", "通过 Tailscale 连接自己的电脑、手机和服务器", func() {
		softButton(c, "邀请成员").OnClick(func() { a.meshInvite, a.meshInviteError = true, "" })
		softButton(c, "连接设置").OnClick(func() { a.meshGuide = true })
		softButton(c, "刷新组网").Disabled(snap.Busy).OnClick(a.meshClient.Refresh)
	})
	a.meshMaybeOpenAuth(c, snap)
	if a.meshAuthError != "" {
		ui.Text(c, a.meshAuthError).FillWidth().FontSize(11.5).TextColor(pal.Warn)
	}
	if snap.Error != "" {
		ui.Text(c, snap.Error).FillWidth().MaxLines(2).FontSize(11.5).TextColor(pal.Danger).Tooltip(snap.Error)
	}
	if a.meshRouteError != "" {
		ui.Text(c, a.meshRouteError).FillWidth().MaxLines(2).FontSize(11.5).TextColor(pal.Danger).Tooltip(a.meshRouteError)
	}
	if (!s.Installed || (!snap.Managed && s.State == "Stopped")) && !snap.Busy {
		a.meshSetup(c)
		return
	}
	ui.Column(c).FillWidth().Radius(12).Background(pal.Surface).Children(func() {
		ui.Row(c).FillWidth().Padding(14, 16).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconNetwork).Size(24, 24).TextColor(pal.Accent).Role(ui.RoleNone)
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				label := meshLabel(s)
				if snap.Error != "" && !s.ShieldsKnown {
					label = "组网状态待确认"
				}
				if snap.Busy && snap.Action != "refresh" {
					label = "正在更新组网设置…"
					if text := map[string]string{"connect": "正在安全连接…", "allow": "正在开放访问…", "block": "正在关闭访问…", "disconnect": "正在断开组网…"}[snap.Action]; text != "" {
						label = text
					}
				}
				ui.Text(c, label).FontSize(15).FontWeight(600).TextColor(pal.Text).SingleLine()
				caption := "先关闭本机入站访问，再连接官方网络"
				if s.Self.IP() != "" {
					caption = s.Self.Name + " · " + s.Self.IP()
				}
				ui.Text(c, caption).FillWidth().SingleLine().Ellipsis("…").Tooltip(caption).FontSize(11.5).TextColor(pal.Muted)
			})
			if s.Running || s.AuthURL != "" || s.State == "Starting" || s.State == "NeedsMachineAuth" || (snap.Busy && snap.Action == "connect") {
				softButton(c, "断开组网").Disabled(snap.Busy && snap.Action == "disconnect").OnClick(func() { a.meshClient.Do("disconnect") })
			} else if s.AuthURL == "" {
				softPrimaryButton(c, "连接组网").Disabled(snap.Busy || !s.ShieldsKnown || !s.Official).OnClick(func() { a.beginMeshConnect() })
			}
		})
		hairline(c)
		ui.Row(c).FillWidth().Padding(12, 16).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				ui.Text(c, "允许组网设备访问本机").FontSize(13).FontWeight(550).TextColor(pal.Text)
				hint, color := "防护状态待确认，请刷新或断开组网", pal.Warn
				if s.ShieldsKnown && s.ShieldsUp {
					hint, color = "入站访问已关闭；仍可主动访问其他设备", pal.Muted
				}
				if s.ShieldsKnown && !s.ShieldsUp {
					hint, color = "已开放 · 仅限授权端口："+mesh.FormatServices(s.Services), pal.Warn
				}
				if s.State == "Stopped" {
					hint, color = "组网已断开；再次连接时会先关闭入站访问", pal.Muted
				}
				ui.Text(c, hint).FillWidth().FontSize(11).TextColor(color)
			})
			allow := s.ShieldsKnown && !s.ShieldsUp
			softSwitch(c, &allow).Label("允许组网访问本机").Disabled(snap.Busy || !s.ShieldsKnown || (!s.Running && !allow) || (!s.Official && !allow))
			if allow != (s.ShieldsKnown && !s.ShieldsUp) {
				if allow {
					a.openMeshServices(snap, true)
				} else {
					a.meshClient.Do("block")
				}
			}
		})
	})
	if s.Running && !a.meshAccessReady() {
		a.meshAccessPreparation(c, false)
	}
	if s.AuthURL != "" && mesh.ValidAuthURL(s.AuthURL) && !s.Running {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "请在浏览器完成官方账号授权").Grow(1).FontSize(12).TextColor(pal.Muted)
			softPrimaryButton(c, "继续登录 Tailscale").Disabled(snap.Busy).OnClick(func() {
				if snap.Managed && s.ShieldsKnown && s.ShieldsUp {
					a.openMeshAuth(c, s.AuthURL)
				} else {
					a.meshGuide = true
				}
			})
		})
	}
	softSegmented(c, &a.meshTab, "设备", "本机访问", "安全设置").AlignSelf(ui.Start).Label("组网分类")
	switch a.meshTab {
	case 1:
		a.meshVisits(c, snap)
	case 2:
		a.meshSecurity(c, snap)
	default:
		a.meshDevices(c, snap)
	}
	a.meshDialogs(c, snap)
}

func (a *App) meshSetup(c *ui.Context) {
	pal := paletteFor(c)
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Radius(12).Background(pal.Surface).Padding(24).Gap(16).Children(func() {
		ui.Icon(c, iconNetwork).Size(36, 36).TextColor(pal.Accent).Role(ui.RoleNone)
		ui.Text(c, "把分散的设备连接起来").FontSize(18).FontWeight(600).TextColor(pal.Text)
		ui.Text(c, "Tailscale 组网内核已内置。登录官方账号即可加入网络，无需另装客户端或自建服务器。").FillWidth().FontSize(13).TextColor(pal.Muted)
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			for i, hint := range []string{"点击连接，在浏览器授权 Tailscale 官方账号", "其他设备登录同一网络，支持同时连接多台设备", "开启 Gopurr TUN 后，普通应用可通过虚拟 IP 访问设备"} {
				ui.Text(c, fmt.Sprintf("%d. %s", i+1, hint)).FillWidth().FontSize(12).TextColor(pal.Text)
			}
		})
		ui.Text(c, "首次连接关闭入站访问。需要远程访问这台电脑时，再由你主动开放。").FillWidth().FontSize(11.5).TextColor(pal.Muted)
		ui.Row(c).Gap(8).Children(func() {
			softPrimaryButton(c, "连接内置组网").OnClick(func() { a.meshGuide = true })
		})
	})
	a.meshDialogs(c, a.meshClient.Snapshot())
}

func (a *App) meshDevices(c *ui.Context, snap mesh.Snapshot) {
	pal := paletteFor(c)
	searchRow(c, &a.meshQuery, "搜索设备、账号或虚拟 IP").Label("搜索组网设备")
	var rows []mesh.Device
	q := strings.ToLower(strings.TrimSpace(a.meshQuery))
	if snap.Status.Self.ID != "" {
		rows = append(rows, snap.Status.Self)
	}
	rows = append(rows, snap.Status.Peers...)
	filtered := rows[:0]
	for _, d := range rows {
		if q == "" || strings.Contains(strings.ToLower(d.Name+" "+meshOwner(d)+" "+d.IP()+" "+d.DNSName), q) {
			filtered = append(filtered, d)
		}
	}
	rows = filtered
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].ID == snap.Status.Self.ID {
			return true
		}
		if rows[j].ID == snap.Status.Self.ID {
			return false
		}
		left, right := a.meshShortcuts[rows[i].ID].LastUsed, a.meshShortcuts[rows[j].ID].LastUsed
		if !left.Equal(right) {
			return left.After(right)
		}
		if rows[i].Online != rows[j].Online {
			return rows[i].Online
		}
		return rows[i].Name < rows[j].Name
	})
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Radius(12).Background(pal.Surface).Children(func() {
		ui.Row(c).FillWidth().Padding(12).Children(func() {
			ui.Text(c, fmt.Sprintf("%d 台设备 · 最近连接优先", len(rows))).Grow(1).FontSize(11).TextColor(pal.Faint)
			ui.Text(c, "状态").Width(52).FontSize(11).TextColor(pal.Faint)
			ui.Text(c, "快捷操作").Width(222).FontSize(11).TextColor(pal.Faint)
		})
		hairline(c)
		if len(rows) == 0 {
			emptyState(c, "没有可显示的设备", "完成登录后，显示当前账号有权限看见的组网设备。")
		} else {
			ui.List(c, &a.meshList, len(rows)).FillWidth().Grow(1).MinHeight(0).ItemKey(func(i int) any { return rows[i].ID }).Rows(func(row ui.ListRow) {
				d, c := rows[row.Index], row.Context
				ui.Row(c).FillWidth().Padding(12).Gap(12).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(pal.Grid).Children(func() {
					ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
						name := d.Name
						if d.ID == snap.Status.Self.ID {
							name += " · 本机"
						}
						if slices.Contains(snap.Status.Blocked, d.ID) {
							name += " · 入站已封禁"
						}
						ui.Text(c, name).FillWidth().FontSize(13).FontWeight(550).SingleLine().Ellipsis("…").Tooltip(name).TextColor(pal.Text)
						caption := meshOwner(d) + " · " + d.IP()
						ui.Text(c, caption).FillWidth().FontSize(11).SingleLine().Ellipsis("…").Tooltip(caption).TextColor(pal.Muted)
					})
					state, color := "离线", pal.Faint
					if d.Online {
						state, color = "在线", pal.Success
					}
					ui.Text(c, state).Width(52).FontSize(11.5).TextColor(color)
					ui.Row(c).Width(222).Gap(6).Justify(ui.End).Children(func() {
						if d.ID != snap.Status.Self.ID {
							shortcut, saved := a.meshShortcuts[d.ID]
							label := "连接"
							if saved && shortcut.Validate(d) == nil {
								label = shortcut.Label()
							} else {
								saved = false
							}
							softButton(c, label).Label("快捷连接 " + d.ID).Disabled(!snap.Status.Running || snap.Busy || d.IP() == "").OnClick(func() {
								if saved && a.meshAccessReady() && d.Online {
									a.launchMeshShortcut(c, d, shortcut, true)
								} else {
									a.openMeshQuick(d)
								}
							})
						}
						softButton(c, "复制 IP").Label("复制设备 IP " + d.ID).Disabled(d.IP() == "").OnClick(func() { c.WriteClipboard(d.IP()) })
						softButton(c, "详情").Label("组网设备详情 " + d.ID).OnClick(func() { a.meshSelected, a.meshShowDevice = d, true })
					})
				})
			})
		}
	})
	ui.Text(c, "在线不等于服务可用，可在连接窗口检测；记住连接方式后可直接打开客户端，详情中可修改。").FillWidth().FontSize(11).TextColor(pal.Faint)
}

func (a *App) meshVisits(c *ui.Context, snap mesh.Snapshot) {
	pal := paletteFor(c)
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "入站 TCP / UDP 连接 · 最近 100 条").Grow(1).FontSize(12).TextColor(pal.Muted)
		softButton(c, "清除已结束记录").Disabled(snap.Busy || len(snap.Visits) == 0).OnClick(a.meshClient.ClearHistory)
	})
	if snap.MonitorError != "" {
		ui.Text(c, snap.MonitorError).FillWidth().FontSize(11.5).TextColor(pal.Warn)
	}
	if snap.MonitorScope != "" {
		ui.Text(c, snap.MonitorScope).FillWidth().FontSize(11).TextColor(pal.Muted)
	}
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Radius(12).Background(pal.Surface).Children(func() {
		if len(snap.Visits) == 0 {
			title := "暂未观察到本机访问"
			if snap.MonitorError != "" {
				title = "当前访问状态未知"
			}
			emptyState(c, title, "设备在线或有组网流量，并不表示它正在访问本机服务。")
		} else {
			ui.List(c, &a.meshList, len(snap.Visits)).FillWidth().Grow(1).MinHeight(0).ItemKey(func(i int) any { return snap.Visits[i].Key + snap.Visits[i].FirstSeen.String() }).Rows(func(row ui.ListRow) {
				v, c := snap.Visits[row.Index], row.Context
				ui.Row(c).FillWidth().Padding(12).Gap(12).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(pal.Grid).Children(func() {
					ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
						ui.Text(c, v.Device).FillWidth().SingleLine().Ellipsis("…").Tooltip(v.Device).FontSize(13).FontWeight(550).TextColor(pal.Text)
						owner := v.Owner
						if v.Login != "" {
							owner += " · " + v.Login
						}
						ui.Text(c, owner).FillWidth().SingleLine().Ellipsis("…").Tooltip(owner).FontSize(11).TextColor(pal.Muted)
						endpoints := v.Remote + " → " + v.Local
						if v.Protocol != "" {
							endpoints = strings.ToUpper(v.Protocol) + " · " + endpoints
						}
						ui.Text(c, endpoints).FillWidth().SingleLine().Ellipsis("…").Tooltip(endpoints).FontSize(11).TextColor(pal.Faint)
					})
					ui.Column(c).Width(92).Gap(4).AlignItems(ui.End).Children(func() {
						state, color := "已结束", pal.Faint
						if v.Active {
							state, color = "连接中", pal.Warn
						}
						if v.Active && snap.Status.ShieldsKnown && snap.Status.ShieldsUp {
							state, color = "入站已阻断", pal.Muted
						}
						if v.Active && (snap.MonitorError != "" || !snap.Status.ShieldsKnown) {
							state, color = "状态待确认", pal.Warn
						}
						ui.Text(c, state).FontSize(11.5).TextColor(color)
						ui.Text(c, v.LastSeen.Local().Format("01-02 15:04:05")).FontSize(10.5).TextColor(pal.Faint).Tooltip("首次观察 " + v.FirstSeen.Local().Format(time.DateTime))
					})
				})
			})
		}
	})
	ui.Text(c, "记录内置组网实际接收的服务连接，含短连接；不含网络层已拦截的尝试。账号为设备所属身份。").FillWidth().FontSize(11).TextColor(pal.Faint)
}

func (a *App) meshSecurity(c *ui.Context, snap mesh.Snapshot) {
	pal := paletteFor(c)
	ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			settingsGroup(c, "授权本机服务", func() {
				ports := mesh.FormatServices(snap.Status.Services)
				if ports == "" {
					ports = "尚未授权任何端口，所有本机服务保持关闭"
				}
				settingsRow(c, "允许访问的端口", ports, func() {
					softButton(c, "编辑端口").Disabled(snap.Busy).OnClick(func() {
						a.openMeshServices(snap, false)
					})
				})
			})
			settingsGroup(c, "网络权限", func() {
				settingsRow(c, "谁能访问哪些服务", "在官方控制台按人员、设备和端口授权；新网络的初始策略可能允许所有成员互访", func() {
					softButton(c, "访问控制").OnClick(func() { c.OpenURL(mesh.PolicyURL) })
				})
				hairline(c)
				settingsRow(c, "新设备审批", "建议启用审批；批准后设备才能收发组网流量", func() {
					softButton(c, "设备审批").OnClick(func() { c.OpenURL(mesh.ApprovalURL) })
				})
				hairline(c)
				settingsRow(c, "移除陌生或丢失的设备", "在官方控制台核对设备和账号，并撤销不再信任的设备", func() {
					softButton(c, "管理设备").OnClick(func() { c.OpenURL(mesh.DevicesURL) })
				})
			})
			settingsGroup(c, "本机保护", func() {
				settingsRow(c, "关闭所有组网入站访问", "立即停止本机服务转发，并终止已有访问连接", func() {
					softButton(c, "关闭本机访问").Disabled(!snap.Status.Installed || (snap.Busy && snap.Action == "block")).OnClick(func() { a.meshClient.Do("block") })
				})
				hairline(c)
				settingsRow(c, "紧急断开", "停止这台电脑的全部 Tailscale 网络连接", func() {
					softButton(c, "立即断开组网").Disabled(!snap.Status.Installed || (snap.Busy && snap.Action == "disconnect")).OnClick(func() { a.meshClient.Do("disconnect") })
				})
			})
			ui.Text(c, "内核随 Gopurr 运行，每次连接先关闭入站访问，不接管 DNS、不发布子网或出口路由。关闭窗口后继续运行；退出或进程终止后组网和服务转发随之停止。").FillWidth().FontSize(11).TextColor(pal.Muted)
			ui.Text(c, "访问记录保存在本机，最多 100 条，不上传也不包含通信内容。局域网与公网的其他访问仍由系统防火墙和各服务管理。").FillWidth().FontSize(11).TextColor(pal.Faint)
			ui.Text(c, "通过规则模式与 Gopurr TUN 访问设备的虚拟 IP，支持 TCP / UDP。内嵌节点不向系统分配虚拟网卡 IP；不提供广播发现、子网路由或系统 ICMP 转发。授权的本机端口转发至 127.0.0.1 的同端口服务，服务仍需自己的登录验证。").FillWidth().FontSize(11).TextColor(pal.Faint)
		})
	})
}

func (a *App) meshDialogs(c *ui.Context, snap mesh.Snapshot) {
	if a.meshInvite {
		a.meshInviteDialog(c, snap)
		return
	}
	a.meshPreparationDialog(c)
	if a.meshPrepareConfirm {
		return
	}
	a.meshGuideDialog(c, snap)
	a.meshQuickDialog(c, snap)
	a.meshServiceDialog(c, snap)
	if !a.meshShowDevice {
		return
	}
	d, pal := a.meshSelected, paletteFor(c)
	width, height := c.Size()
	ui.Overlay(c, func() {
		back := ui.Box(c.Key("mesh-device-detail")).Absolute().Left(0).Top(0).Right(0).Bottom(0).Center().Modal()
		modalBackdrop(back)
		if back.OverlayShortcut(0, ui.KeyEscape) {
			a.meshShowDevice = false
		}
		back.Children(func() {
			panel := ui.Column(c).Width(min(float32(560), width-64)).MaxHeight(height-72).Padding(24).Gap(16).Radius(14).Background(pal.Surface).Border(1, pal.Border).Role(ui.RoleDialog).Label("组网设备详情")
			modalPanel(c, panel)
			panel.Children(func() {
				ui.Text(c, d.Name).FillWidth().MaxLines(2).Tooltip(d.Name).FontSize(18).FontWeight(600).TextColor(pal.Text)
				ui.Scroll(c).FillWidth().Shrink(1).MinHeight(0).Children(func() {
					ui.Column(c).FillWidth().Gap(12).Children(func() {
						for _, field := range [][2]string{{"所属账号", meshOwner(d)}, {"系统", d.OS}, {"虚拟 IP", d.IP()}, {"设备域名", d.DNSName}, {"设备标识", d.ID}, {"当前链路", d.Path}, {"标签", strings.Join(d.Tags, ", ")}} {
							if field[1] == "" {
								continue
							}
							ui.Column(c).FillWidth().Gap(4).Children(func() {
								ui.Text(c, field[0]).FontSize(11).TextColor(pal.Faint)
								ui.Text(c, field[1]).FillWidth().FontSize(12.5).TextColor(pal.Text).Selectable()
							})
						}
						ui.Text(c, "设备身份用于识别账号，不能证明实际操作者。此处在线状态不代表对方正在访问本机。").FillWidth().FontSize(11).TextColor(pal.Muted)
						if d.ID != snap.Status.Self.ID {
							blocked := slices.Contains(snap.Status.Blocked, d.ID)
							label, hint := "禁止此设备访问本机", "禁止后立即终止此设备的已有访问，只影响这台电脑的组网服务。"
							if blocked {
								label, hint = "解除本机封禁", "此设备已被本机封禁；官方网络权限与授权端口仍共同生效。"
							}
							ui.Text(c, hint).FillWidth().FontSize(11).TextColor(pal.Muted)
							softButton(c, label).Disabled(snap.Busy).OnClick(func() { a.meshClient.SetDeviceBlocked(d.ID, !blocked) })
						}
					})
				})
				ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
					if d.ID != snap.Status.Self.ID {
						softButton(c, "连接设置").OnClick(func() { a.meshShowDevice = false; a.openMeshQuick(d) })
					}
					softButton(c, "复制虚拟 IP").Disabled(d.IP() == "").OnClick(func() { c.WriteClipboard(d.IP()) })
					softButton(c, "在控制台管理").OnClick(func() { c.OpenURL(mesh.DevicesURL) })
					softPrimaryButton(c, "完成").OnClick(func() { a.meshShowDevice = false })
				})
			})
		})
	})
}

func (a *App) meshServiceDialog(c *ui.Context, snap mesh.Snapshot) {
	if !a.meshConfirmAllow && !a.meshEditServices {
		return
	}
	pal := paletteFor(c)
	width, height := c.Size()
	ui.Overlay(c, func() {
		back := ui.Box(c.Key("mesh-services-dialog")).Absolute().Left(0).Top(0).Right(0).Bottom(0).Center().Modal()
		modalBackdrop(back)
		closeDialog := func() {
			a.meshConfirmAllow, a.meshEditServices = false, false
			if a.meshGuideAfterServices {
				a.meshGuideAfterServices = false
				a.meshGuide = true
			}
		}
		if back.OverlayShortcut(0, ui.KeyEscape) {
			closeDialog()
		}
		back.Children(func() {
			panel := ui.Column(c).Width(min(float32(540), width-64)).MaxHeight(height-72).Padding(24).Gap(16).Radius(14).Background(pal.Surface).Border(1, pal.Border).Role(ui.RoleDialog).Label("授权本机组网服务")
			modalPanel(c, panel)
			panel.Children(func() {
				title := "允许组网设备访问这台电脑？"
				if a.meshEditServices {
					title = "授权本机服务端口"
				}
				ui.Text(c, title).FillWidth().FontSize(18).FontWeight(600).TextColor(pal.Text)
				ui.Scroll(c).FillWidth().Shrink(1).MinHeight(0).Children(func() {
					ui.Column(c).FillWidth().Gap(12).Children(func() {
						ui.Text(c, "只开放下列端口，其他本机服务保持关闭。对方还需通过官方网络权限检查，且未被本机封禁。").FillWidth().FontSize(12).TextColor(pal.Muted)
						a.meshServicePresets(c)
						ui.Text(c, "请只开放你认识且有登录保护的服务。Gopurr 不会自动启动 SSH、远程桌面或共享服务；修改端口后需要重新开放访问。").FillWidth().FontSize(11).TextColor(pal.Muted)
						if a.meshServicesError != "" {
							ui.Text(c, a.meshServicesError).FillWidth().FontSize(11).TextColor(pal.Danger)
						}
					})
				})
				ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
					softButton(c, "取消").OnClick(closeDialog)
					label := "允许访问本机"
					if a.meshEditServices {
						label = "保存并关闭访问"
					}
					softPrimaryButton(c, label).Disabled(snap.Busy).OnClick(func() {
						ports, err := mesh.ParseServices(a.meshServicesDraft)
						if err != nil {
							a.meshServicesError = err.Error()
							return
						}
						if a.meshConfirmAllow && len(ports) == 0 {
							a.meshServicesError = "请明确填写要开放的服务端口"
							return
						}
						a.meshClient.SetServices(mesh.FormatServices(ports), a.meshConfirmAllow)
						closeDialog()
					})
				})
			})
		})
	})
}
