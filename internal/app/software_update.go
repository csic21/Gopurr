package app

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"go.yaml.in/yaml/v3"
	"gopurr/internal/buildinfo"
	"gopurr/internal/sysproxy"
	"gopurr/internal/update"
)

// UpdateProxyURL reads persisted/runtime state instead of UI-owned fields,
// because update downloads run on a worker goroutine.
func (a *App) UpdateProxyURL() *url.URL {
	proxyURL := func(host string, port int) *url.URL {
		if host == "" || port < 1 || port > 65535 {
			return nil
		}
		return &url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(port))}
	}
	if state, err := sysproxy.Current(); err == nil && state.Enabled {
		if proxy := proxyURL(state.Server, state.Port); proxy != nil {
			return proxy
		}
	}
	return a.SubscriptionProxyURL()
}

// SubscriptionProxyURL deliberately uses this application's running listener,
// rather than another client's system proxy or proxy environment variables.
func (a *App) SubscriptionProxyURL() *url.URL {
	if a.mgr == nil || !a.mgr.Running() {
		return nil
	}
	raw, err := os.ReadFile(a.mgr.ConfigPath())
	if err != nil {
		return nil
	}
	var config struct {
		Port int `yaml:"mixed-port"`
	}
	if yaml.Unmarshal(raw, &config) != nil || config.Port < 1 || config.Port > 65535 {
		return nil
	}
	return &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(config.Port))}
}

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
	settingsGroup(c, tr("软件更新"), func() {
		settingsRow(c, "Gopurr "+softwareVersion(), tr("让连接简单一点 · mygo 原生界面 · mihomo 内核"), nil)
		hairline(c)
		on := a.Prefs().CheckAppUpdates
		settingsRow(c, tr("自动检查更新"), tr("每天检查新版本；下载与安装前由你确认"), func() {
			softSwitch(c, &on).Label(tr("自动检查软件更新"))
		})
		if on != a.Prefs().CheckAppUpdates {
			if err := a.changePrefs(func(p *Prefs) { p.CheckAppUpdates = on }); err != nil {
				a.generalSettingsError = tr("设置保存失败：") + err.Error()
			}
		}
		hairline(c)
		label, hint := tr("检查更新"), tr("从 GitHub Releases 获取经过签名校验的更新")
		switch s.Phase {
		case update.Checking:
			label, hint = tr("正在检查…"), tr("正在连接发布服务器")
		case update.Available:
			label, hint = tr("查看更新"), tr("发现新版本 ")+s.Version
		case update.Downloading:
			label, hint = tr("查看进度"), tr("正在下载版本 ")+s.Version
		case update.Ready:
			label, hint = tr("重启并更新"), tr("新版本已准备好，重启后生效")
		case update.Current:
			hint = tr("当前已是最新版本")
		}
		if !s.Enabled {
			hint = tr("安装正式发布包后可在应用内更新")
		}
		settingsRow(c, tr("版本检查"), hint, func() {
			softButton(c, label).Disabled(s.Phase == update.Checking).OnClick(func() {
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
	softModal(c, "software-update", &visible, func() {
		ui.Column(c).Width(min(float32(560), width-64)).Padding(24).Radius(14).
			Background(pal.Surface).Border(1, pal.Border).Gap(14).Children(func() {
			title, message := tr("软件更新"), tr("当前版本 ")+softwareVersion()
			switch s.Phase {
			case update.Checking:
				title, message = tr("正在检查更新"), tr("正在连接 GitHub Releases…")
			case update.Available:
				title, message = tr("发现新版本"), fmt.Sprintf(tr("Gopurr %s · 当前版本 %s"), s.Version, softwareVersion())
			case update.Current:
				title, message = tr("已是最新版本"), tr("你正在使用 Gopurr ")+softwareVersion()
			case update.Downloading:
				title, message = tr("正在下载更新"), tr("签名校验完成后安装；代理会继续运行")
			case update.Ready:
				title, message = tr("更新已准备好"), tr("重启 Gopurr 后使用新版本；订阅与设置会保留。")
			case update.Failed:
				title, message = tr("暂时无法检查更新"), tr("请检查网络连接后重试。")
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
					softButton(c, tr("取消")).OnClick(func() { a.softwareUpdates.Cancel(); a.softwareUpdates.Hide() })
				case update.Available:
					softButton(c, tr("稍后")).OnClick(a.softwareUpdates.Hide)
					softPrimaryButton(c, tr("下载并安装")).OnClick(a.softwareUpdates.Install)
				case update.Ready:
					softButton(c, tr("稍后重启")).OnClick(a.softwareUpdates.Hide)
					softPrimaryButton(c, tr("立即重启")).OnClick(func() { a.requestQuit(true) })
				default:
					softButton(c, tr("关闭")).OnClick(a.softwareUpdates.Hide)
					if s.Enabled {
						softPrimaryButton(c, tr("重新检查")).OnClick(func() { a.softwareUpdates.Check(true) })
					}
				}
			})
		})
	})
	if !visible {
		a.softwareUpdates.Hide()
	}
}
