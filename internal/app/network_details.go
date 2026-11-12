package app

import (
	"fmt"
	"net"
	"strconv"

	ui "gopurr/internal/appui"
)

func busyNetwork(a *App) bool {
	a.bmu.Lock()
	defer a.bmu.Unlock()
	return a.kernelBusy || a.svcBusy || a.sysBusy || a.tunChanging || a.proxySelecting || a.proxySelectionActive.Load() > 0
}

func (a *App) networkDetails(c *ui.Context) {
	softModal(c, "network-details", &a.showNetworkDetails, func() {
		pal := paletteFor(c)
		width, height := c.Size()
		ui.Column(c).Width(min(float32(560), width-64)).MaxHeight(height - 72).Padding(20).Gap(12).Children(func() {
			ui.Text(c, tr("网络详情")).FontSize(18).FontWeight(600).TextColor(pal.Text)
			ui.Text(c, tr("本地就绪状态不代表外部节点或互联网一定可用；可在设置中运行连通诊断。")).FillWidth().FontSize(11.5).TextColor(pal.Muted)
			ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).MaxHeight(340).Children(func() {
				ui.Column(c).FillWidth().Gap(12).Children(func() {
					settingsGroup(c, tr("系统代理"), func() {
						networkDetail(c, tr("状态"), a.sysProxyHint())
						networkDetail(c, tr("本地入口"), net.JoinHostPort("127.0.0.1", strconv.Itoa(a.mixedPort())))
						current := tr("未设置手动代理")
						if a.sysState.Enabled {
							current = net.JoinHostPort(a.sysState.Server, strconv.Itoa(a.sysState.Port))
							if a.sysState.RawServer != "" {
								current = a.sysState.RawServer
							}
						}
						networkDetail(c, tr("系统当前指向"), current)
						if a.sysState.Service != "" {
							networkDetail(c, tr("作用范围"), a.sysState.Service)
						}
						if a.sysErrText() != "" {
							networkDetail(c, tr("错误原因"), a.sysErrText())
						}
						if a.proxyReadErr != "" {
							networkDetail(c, tr("错误原因"), a.proxyReadErr)
						}
					})
					settingsGroup(c, tr("内核与 TUN"), func() {
						if installed, _, _ := a.svcState(); installed {
							networkDetail(c, tr("权限管理"), tr("系统代理与 TUN 共用已授权的系统服务"))
							if st, err := a.mgr.Status(); err == nil && st.Version != "" {
								networkDetail(c, tr("服务版本"), st.Version)
							}
							if a.mgr.ServiceVersionSkew() {
								networkDetail(c, tr("服务更新"), tr("当前服务兼容，可继续使用；更新服务需要管理员授权"))
							}
						}
						observed := tr("等待检查")
						if a.networkFresh() {
							observed = tr("本地代理握手未通过")
							if a.networkRuntime.ProxyReady {
								observed = tr("本地代理握手通过")
							}
						} else if a.networkErr != "" {
							observed = a.networkErr
						}
						networkDetail(c, tr("实际检测"), observed)
						networkDetail(c, tr("TUN 状态"), a.tunHint())
						if a.networkFresh() && a.networkRuntime.TunEnabled {
							networkDetail(c, tr("虚拟网卡"), a.networkRuntime.TunDevice)
							networkDetail(c, tr("协议栈"), a.networkRuntime.TunStack)
							networkDetail(c, tr("自动路由"), featureEnabledLabel(a.networkRuntime.AutoRoute))
							networkDetail(c, tr("内置 DNS"), featureEnabledLabel(a.networkRuntime.DNSReady))
						}
						_, _, svcErr := a.svcState()
						for _, problem := range []string{a.tunErrText(), svcErr, a.networkRuntime.ProxyError, a.networkRuntime.TunError} {
							if problem != "" {
								networkDetail(c, tr("错误原因"), problem)
							}
						}
					})
				})
			})
			if installed, _, _ := a.svcState(); installed {
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					if a.mgr.ServiceNeedsUpdate() || a.mgr.ServiceVersionSkew() {
						softButton(c, tr("更新服务…")).Disabled(busyNetwork(a)).OnClick(func() { a.showNetworkDetails = false; a.confirmInstall = true })
					}
					softButton(c, tr("重启服务")).Disabled(busyNetwork(a)).OnClick(a.restartService)
					softButton(c, tr("卸载服务")).Disabled(busyNetwork(a)).OnClick(func() { a.showNetworkDetails = false; a.confirmUninstall = true })
				})
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				softButton(c, tr("重新检测")).Disabled(busyNetwork(a)).OnClick(a.refreshNetworkState)
				softPrimaryButton(c, tr("关闭")).OnClick(func() { a.showNetworkDetails = false })
			})
		})
	})
}

func featureEnabledLabel(enabled bool) string {
	if enabled {
		return tr("已开启")
	}
	return tr("未开启")
}

func networkDetail(c *ui.Context, label, value string) {
	pal := paletteFor(c)
	ui.Row(c).FillWidth().MinHeight(32).Padding(8, 12).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).Width(92).FontSize(11).TextColor(pal.Muted)
		ui.Text(c, fmt.Sprint(value)).Grow(1).MinWidth(0).FontSize(12).TextColor(pal.Text).Selectable()
	})
}
