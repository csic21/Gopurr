package app

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/mesh"
	"gopurr/internal/service"
)

func (a *App) meshAccessReady() bool {
	if a.k == nil || !a.tunOn || !a.ov.TunEnable || !a.ov.TunAutoRout || a.modeToken != "rule" || a.meshRouteError != "" {
		return false
	}
	// Rendering several controls must not repeat the same controller request.
	if time.Since(a.meshKernelChecked) > time.Second {
		a.meshKernelRunning, a.meshKernelChecked = a.k.Running(), time.Now()
	}
	return a.meshKernelRunning
}

func (a *App) beginMeshConnect() {
	a.meshOpenAuth, a.meshAuthError, a.meshOpenedAuthURL = true, "", ""
	a.meshClient.Do("connect")
}

func (a *App) openMeshAuth(c *ui.Context, raw string) {
	if !mesh.ValidAuthURL(raw) {
		a.meshAuthError = "登录地址未通过验证，请重新连接"
		return
	}
	a.meshOpenedAuthURL, a.meshOpenAuth = raw, false
	c.OpenURLThen(raw, func(err error) {
		if err != nil {
			a.meshAuthError = "未能打开浏览器，请重新点击登录按钮重试"
		} else {
			a.meshAuthError = ""
		}
	})
}

func (a *App) meshMaybeOpenAuth(c *ui.Context, s mesh.Snapshot) {
	if a.meshOpenAuth && !s.Busy && s.Managed && s.Status.ShieldsKnown && s.Status.ShieldsUp && !s.Status.Running && s.Status.AuthURL != "" && s.Status.AuthURL != a.meshOpenedAuthURL {
		a.openMeshAuth(c, s.Status.AuthURL)
	}
}

func (a *App) requestMeshPreparation() {
	a.meshPrepareReturn = ""
	if a.meshGuide {
		a.meshPrepareReturn = "guide"
		a.meshGuide = false
	}
	if a.meshQuick {
		a.meshPrepareReturn = "quick"
		a.meshQuick = false
	}
	a.meshPrepareConfirm, a.meshPrepareError = true, ""
}

func (a *App) returnFromMeshPreparation() {
	switch a.meshPrepareReturn {
	case "guide":
		a.meshGuide = true
	case "quick":
		a.meshQuick = true
	}
	a.meshPrepareReturn = ""
}

func (a *App) reconcileMeshPreparation() {
	if !a.meshPreparePending || a.shutdownPending.Load() {
		return
	}
	installed, busy, svcErr := a.svcState()
	if busy {
		return
	}
	a.meshPreparePending = false
	if svcErr != "" {
		a.meshPrepareError = svcErr
		return
	}
	if !installed || a.k == nil || !a.k.IsRoot() {
		a.meshPrepareError = "系统服务尚未就绪，请重试准备访问"
		return
	}
	a.applyMeshPreparation()
}

// Prepare only outbound networking. Service authorization has a separate gate.
func (a *App) applyMeshPreparation() bool {
	a.meshPrepareError = ""
	if a.k == nil || !a.k.IsRoot() {
		a.meshPrepareError = "需要先安装 Gopurr 系统服务"
		return false
	}
	previous := a.ov
	next := previous
	next.Mode, next.TunEnable, next.TunAutoRout = "rule", true, true
	a.ov = next
	if !a.startKernel() {
		a.ov = previous
		a.meshPrepareError = "网络准备失败：" + a.notice
		return false
	}
	if err := a.saveOverridesAtomic(); err != nil {
		a.ov = previous
		a.meshPrepareError = "网络设置保存失败：" + err.Error()
		if !a.startKernel() {
			a.ov = next
			a.meshPrepareError += "；恢复原内核配置失败，请重试"
		}
		a.tunOn, a.modeToken = a.ov.TunEnable, a.ov.Mode
		a.modePick, a.modeIdx = labelFor(a.modeToken), modeIndex(a.modeToken)
		return false
	}
	a.tunOn, a.modeToken = true, "rule"
	a.meshKernelRunning, a.meshKernelChecked = true, time.Now()
	a.modePick, a.modeIdx = labelFor("rule"), modeIndex("rule")
	ips, addr, password := a.meshRuntimeRoutes()
	a.meshRouteKey, a.meshRouteError = strings.Join(ips, ",")+addr+password, ""
	a.setTunErr("")
	return true
}

func (a *App) meshAccessPreparation(c *ui.Context, detailed bool) {
	pal := paletteFor(c)
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			hint, label := "准备本机网络后，即可用普通应用访问其他设备", "准备访问"
			_, busy, _ := a.svcState()
			if a.meshPreparePending || busy {
				hint, label = "等待系统授权与服务启动…", "正在准备…"
			}
			ui.Text(c, hint).Grow(1).FontSize(11).TextColor(pal.Muted)
			softButton(c, label).Disabled(busy || a.meshPreparePending).OnClick(a.requestMeshPreparation)
		})
		if a.meshPrepareError != "" {
			ui.Text(c, a.meshPrepareError).FillWidth().MaxLines(3).Tooltip(a.meshPrepareError).FontSize(11.5).TextColor(pal.Danger)
		}
		if detailed {
			ui.Text(c, "自动完成 TUN、规则模式与所需系统服务设置；如果只让其他设备访问本机，可稍后再准备。").FillWidth().FontSize(11).TextColor(pal.Muted)
		}
	})
}

func (a *App) meshPreparationDialog(c *ui.Context) {
	if !a.meshPrepareConfirm {
		return
	}
	needsService := a.k == nil || !a.k.IsRoot()
	description := "将启用 Gopurr TUN 自动路由并切换到规则模式，使普通应用能够访问组网 IP。本机服务仍由访问开关单独控制。"
	button := "启用并准备"
	if needsService {
		description = "将安装 Gopurr 自带的系统服务，启用 TUN 自动路由并切换到规则模式。系统会请求管理员授权，无需另装 Tailscale。本机服务仍由访问开关单独控制。"
		button = "安装并准备"
	}
	choice := softAlertDialog(c, &a.meshPrepareConfirm, "准备访问其他设备", description, "取消", button)
	if choice == 1 {
		if needsService {
			if !service.Available() {
				a.meshPrepareError = "当前平台不支持 TUN 系统服务"
			} else {
				a.meshPreparePending = true
				a.installService()
			}
		} else {
			a.applyMeshPreparation()
		}
		a.returnFromMeshPreparation()
	} else if !a.meshPrepareConfirm {
		a.returnFromMeshPreparation()
	}
}

func (a *App) meshGuideDialog(c *ui.Context, snap mesh.Snapshot) {
	if !a.meshGuide {
		return
	}
	pal := paletteFor(c)
	width, height := c.Size()
	ui.Overlay(c, func() {
		back := ui.Box(c.Key("mesh-guide")).Absolute().Left(0).Top(0).Right(0).Bottom(0).Center().Modal()
		modalBackdrop(back)
		if back.OverlayShortcut(0, ui.KeyEscape) {
			a.meshGuide = false
		}
		back.Children(func() {
			panel := ui.Column(c).Width(min(float32(580), width-64)).MaxHeight(height-72).Padding(24).Gap(16).Radius(14).Background(pal.Surface).Border(1, pal.Border).Role(ui.RoleDialog).Label("组网连接设置")
			modalPanel(c, panel)
			panel.Children(func() {
				ui.Text(c, "连接准备").FontSize(18).FontWeight(600).TextColor(pal.Text)
				ui.Scroll(c).FillWidth().Shrink(1).MinHeight(0).Children(func() {
					ui.Column(c).FillWidth().Gap(16).Children(func() {
						if snap.Error != "" {
							ui.Text(c, snap.Error).FillWidth().FontSize(11.5).TextColor(pal.Danger)
						}
						if a.meshAuthError != "" {
							ui.Text(c, a.meshAuthError).FillWidth().FontSize(11.5).TextColor(pal.Warn)
						}
						ui.Column(c).FillWidth().Gap(6).Children(func() {
							ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
								ui.Text(c, "1 · 加入同一个网络").Grow(1).FontSize(13).FontWeight(550).TextColor(pal.Text)
								if snap.Status.Running {
									ui.Text(c, "已连接").FontSize(12).TextColor(pal.Success)
								} else {
									label := "连接并登录"
									if snap.Status.AuthURL != "" {
										label = "浏览器登录"
									}
									if snap.Busy && snap.Action == "connect" {
										label = "正在连接…"
										softButton(c, "取消连接").OnClick(func() {
											a.meshOpenAuth = false
											a.meshClient.Do("disconnect")
										})
									}
									softButton(c, label).Disabled(snap.Busy).OnClick(func() {
										if snap.Status.AuthURL != "" && snap.Managed && snap.Status.ShieldsKnown && snap.Status.ShieldsUp {
											a.openMeshAuth(c, snap.Status.AuthURL)
										} else {
											a.beginMeshConnect()
										}
									})
								}
							})
							ui.Text(c, "两台设备分别用官方账号加入同一网络，无需互填配对码。首次登录在浏览器完成。").FillWidth().FontSize(11.5).TextColor(pal.Muted)
							if snap.Busy && snap.Action == "connect" {
								ui.Text(c, "正在获取官方登录链接，成功后会自动打开浏览器；最多等待 30 秒。").FillWidth().FontSize(11.5).TextColor(pal.Muted)
							}
							if snap.Status.State == "NeedsMachineAuth" {
								ui.Text(c, "正在等待网络管理员批准此设备").FillWidth().FontSize(11.5).TextColor(pal.Warn)
							}
						})
						hairline(c)
						ui.Column(c).FillWidth().Gap(6).Children(func() {
							ui.Text(c, "2 · 访问其他设备").FontSize(13).FontWeight(550).TextColor(pal.Text)
							if a.meshAccessReady() {
								ui.Text(c, "本机网络已准备好，可从设备列表直接连接").FillWidth().FontSize(11.5).TextColor(pal.Success)
							} else {
								a.meshAccessPreparation(c, true)
							}
						})
						hairline(c)
						ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
							ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
								ui.Text(c, "3 · 让对方访问本机（可选）").FontSize(13).FontWeight(550).TextColor(pal.Text)
								description, color := "选择 SSH、远程桌面或屏幕共享；仅开放你确认的端口。", pal.Muted
								if snap.Status.ShieldsKnown && !snap.Status.ShieldsUp {
									description, color = "已开放："+mesh.FormatServices(snap.Status.Services), pal.Warn
								}
								ui.Text(c, description).FillWidth().FontSize(11.5).TextColor(color)
							})
							softButton(c, "选择本机服务").Disabled(!snap.Status.Running || !snap.Status.ShieldsKnown || snap.Busy).OnClick(func() { a.meshGuide = false; a.meshGuideAfterServices = true; a.openMeshServices(snap, true) })
						})
						hairline(c)
						auto := snap.AutoConnect
						ui.Checkbox(c, &auto, "启动 Gopurr 时自动连接组网").Label("组网启动自动连接")
						if auto != snap.AutoConnect {
							a.meshClient.SetAutoConnect(auto)
						}
						ui.Text(c, "自动连接只恢复网络，不会自动开放本机服务；授权失效时仍需重新登录。").FillWidth().FontSize(11).TextColor(pal.Muted)
					})
				})
				ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
					softButton(c, "稍后设置").OnClick(func() { a.meshGuide = false })
					softPrimaryButton(c, "完成设置").Disabled(!snap.Status.Running).OnClick(func() { a.meshGuide = false })
				})
			})
		})
	})
}
