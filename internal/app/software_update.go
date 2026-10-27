package app

import (
	"context"
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gopurr/internal/buildinfo"
	"gopurr/internal/update"
)

func (a *App) initSoftwareUpdates() {
	if a.softwareUpdates == nil {
		a.softwareUpdates = update.New(update.Native{}, a.invalidate)
	}
}

func (a *App) startSoftwareUpdates() {
	a.initSoftwareUpdates()
	ctx, cancel := context.WithCancel(context.Background())
	a.stopSoftwareUpdates = cancel
	go a.softwareUpdates.Run(ctx, func() bool { return a.Prefs().CheckAppUpdates })
}

func softwareVersion() string {
	if version := mygo.App.Version(); version != "" {
		return version
	}
	return buildinfo.Version
}

func (a *App) softwareUpdateSettings(c *ui.Context) {
	a.initSoftwareUpdates()
	s := a.softwareUpdates.Snapshot()
	settingsGroup(c, "软件更新", func() {
		settingsRow(c, "Gopurr "+softwareVersion(), "mygo 原生界面 · mihomo 内核", nil)
		hairline(c)
		on := a.Prefs().CheckAppUpdates
		settingsRow(c, "自动检查更新", "每天检查新版本；下载与安装前由你确认", func() {
			ui.Switch(c, &on).Label("自动检查软件更新")
		})
		if on != a.Prefs().CheckAppUpdates {
			if err := a.changePrefs(func(p *Prefs) { p.CheckAppUpdates = on }); err != nil {
				a.generalSettingsError = "设置保存失败：" + err.Error()
			}
		}
		hairline(c)
		label, hint := "检查更新", "从 GitHub Releases 获取经过签名校验的更新"
		switch s.Phase {
		case update.Checking:
			label, hint = "正在检查…", "正在连接发布服务器"
		case update.Available:
			label, hint = "查看更新", "发现新版本 "+s.Version
		case update.Downloading:
			label, hint = "查看进度", "正在下载版本 "+s.Version
		case update.Ready:
			label, hint = "重启并更新", "新版本已准备好，重启后生效"
		case update.Current:
			hint = "当前已是最新版本"
		}
		if !s.Enabled {
			hint = "安装正式发布包后可在应用内更新"
		}
		settingsRow(c, "版本检查", hint, func() {
			ui.Button(c, label).Disabled(s.Phase == update.Checking).OnClick(func() {
				if s.Phase == update.Available || s.Phase == update.Downloading || s.Phase == update.Ready {
					a.softwareUpdates.Show()
				} else {
					a.softwareUpdates.Check(true)
				}
			})
		})
		if a.generalSettingsError != "" {
			settingsFeedback(c, a.generalSettingsError, true)
		}
	})
}

func (a *App) softwareUpdateModal(c *ui.Context) {
	if a.softwareUpdates == nil {
		return
	}
	s := a.softwareUpdates.Snapshot()
	if !s.Visible {
		return
	}
	pal := paletteFor(c)
	width, height := c.Size()
	visible := true
	ui.Modal(c, &visible, func() {
		ui.Column(c).Width(min(float32(560), width-64)).Padding(24).Radius(14).
			Background(pal.Surface).Border(1, pal.Border).Gap(14).Children(func() {
			title, message := "软件更新", "当前版本 "+softwareVersion()
			switch s.Phase {
			case update.Checking:
				title, message = "正在检查更新", "正在连接 GitHub Releases…"
			case update.Available:
				title, message = "发现新版本", fmt.Sprintf("Gopurr %s · 当前版本 %s", s.Version, softwareVersion())
			case update.Current:
				title, message = "已是最新版本", "你正在使用 Gopurr "+softwareVersion()
			case update.Downloading:
				title, message = "正在下载更新", "签名校验完成后安装；代理会继续运行"
			case update.Ready:
				title, message = "更新已准备好", "重启 Gopurr 后使用新版本；订阅与设置会保留。"
			case update.Failed:
				title, message = "暂时无法检查更新", "请检查网络连接后重试。"
			}
			ui.Text(c, title).FontSize(18).Bold().TextColor(pal.Text)
			ui.Text(c, message).FillWidth().FontSize(12).TextColor(pal.Muted)
			if s.Phase == update.Available && s.Notes != "" {
				ui.Scroll(c).FillWidth().Height(min(float32(200), height-310)).Children(func() {
					ui.Text(c, s.Notes).FillWidth().FontSize(12).TextColor(pal.Text).Selectable()
				})
			}
			if s.Phase == update.Checking {
				ui.Progress(c, -1).FillWidth()
			}
			if s.Phase == update.Downloading {
				progress := float64(-1)
				if s.Total > 0 {
					progress = min(1, float64(s.Downloaded)/float64(s.Total))
				}
				ui.Progress(c, progress).FillWidth()
				ui.Text(c, fmt.Sprintf("%s / %s", fmtBytes(s.Downloaded), fmtBytes(s.Total))).FontSize(11).TextColor(pal.Faint)
			}
			if s.Error != "" {
				ui.Text(c, s.Error).FillWidth().MaxLines(4).FontSize(11).TextColor(pal.Danger).Tooltip(s.Error)
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				switch s.Phase {
				case update.Checking, update.Downloading:
					ui.Button(c, "取消").OnClick(func() { a.softwareUpdates.Cancel(); a.softwareUpdates.Hide() })
				case update.Available:
					ui.Button(c, "稍后").OnClick(a.softwareUpdates.Hide)
					ui.PrimaryButton(c, "下载并安装").OnClick(a.softwareUpdates.Install)
				case update.Ready:
					ui.Button(c, "稍后重启").OnClick(a.softwareUpdates.Hide)
					ui.PrimaryButton(c, "立即重启").OnClick(mygo.App.Relaunch)
				default:
					ui.Button(c, "关闭").OnClick(a.softwareUpdates.Hide)
					if s.Enabled {
						ui.PrimaryButton(c, "重新检查").OnClick(func() { a.softwareUpdates.Check(true) })
					}
				}
			})
		})
	})
	if !visible {
		a.softwareUpdates.Hide()
	}
}
