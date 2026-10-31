package app

import (
	"github.com/egoist/mygo/ui"
	"gopurr/internal/mesh"
)

func (a *App) meshInviteDialog(c *ui.Context, snap mesh.Snapshot) {
	pal := paletteFor(c)
	width, height := c.Size()
	ui.Overlay(c, func() {
		back := ui.Box(c.Key("mesh-invite")).Absolute().Left(0).Top(0).Right(0).Bottom(0).Center().Modal()
		modalBackdrop(back)
		if back.OverlayShortcut(0, ui.KeyEscape) {
			a.meshInvite = false
		}
		back.Children(func() {
			panel := ui.Column(c).Width(min(float32(580), width-64)).MaxHeight(height-72).Padding(24).Gap(16).Radius(14).Background(pal.Surface).Border(1, pal.Border).Role(ui.RoleDialog).Label(tr("邀请组网成员"))
			modalPanel(c, panel)
			panel.Children(func() {
				ui.Text(c, tr("邀请其他账号加入网络")).FontSize(18).FontWeight(600).TextColor(pal.Text)
				ui.Scroll(c).FillWidth().Shrink(1).MinHeight(0).Children(func() {
					ui.Column(c).FillWidth().Gap(16).Children(func() {
						if snap.Status.Tailnet != "" && snap.Status.Running {
							ui.Column(c).FillWidth().Gap(4).Children(func() {
								ui.Text(c, tr("当前网络")).FontSize(11).TextColor(pal.Muted)
								ui.Text(c, snap.Status.Tailnet).FillWidth().MaxLines(2).Ellipsis("…").Tooltip(snap.Status.Tailnet).FontSize(13).FontWeight(550).TextColor(pal.Text)
							})
						} else {
							ui.Text(c, tr("组网尚未连接，请在官方控制台确认要邀请加入的网络。")).FillWidth().FontSize(11.5).TextColor(pal.Muted)
						}
						hairline(c)
						for _, step := range [][2]string{
							{tr("1 · 生成邀请"), tr("在官方控制台选择 Invite external users，填写邮箱或生成邀请链接，再分享给对方。")},
							{tr("2 · 用各自的账号加入"), tr("对方接受邀请后，在 Gopurr「连接并登录」时选择同一个网络，无需共用账号。")},
							{tr("3 · 刷新设备并连接"), tr("返回组网页面点击「刷新组网」。需要被访问的设备还需开启 SSH、远程桌面或屏幕共享等本机服务。")},
						} {
							ui.Column(c).FillWidth().Gap(5).Children(func() {
								ui.Text(c, step[0]).FontSize(13).FontWeight(550).TextColor(pal.Text)
								ui.Text(c, step[1]).FillWidth().FontSize(11.5).TextColor(pal.Muted)
							})
						}
						ui.Text(c, tr("邀请需要网络所有者或管理员权限；成员加入后仍受网络访问策略和本机服务开关限制。")).FillWidth().FontSize(11).TextColor(pal.Muted)
						if a.meshInviteError != "" {
							ui.Text(c, a.meshInviteError).FillWidth().FontSize(11.5).TextColor(pal.Warn)
						}
					})
				})
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					softButton(c, tr("复制控制台地址")).OnClick(func() { c.WriteClipboard(mesh.InviteURL) })
					ui.Box(c).Grow(1)
					softButton(c, tr("关闭")).OnClick(func() { a.meshInvite = false })
					softPrimaryButton(c, tr("打开邀请页面")).OnClick(func() {
						c.OpenURLThen(mesh.InviteURL, func(err error) {
							a.meshInviteError = ""
							if err != nil {
								a.meshInviteError = tr("未能打开浏览器，请复制控制台地址后手动访问。")
							}
						})
					})
				})
			})
		})
	})
}
