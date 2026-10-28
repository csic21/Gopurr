package app

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"gopurr/internal/patch"
)

// Embed settingsState in App. Widget drafts are initialized lazily, after both
// preferences and overrides have loaded; App's existing light/LAN fields stay
// the bindings for those controls.
type settingsState struct {
	settingsTab     int
	settingsReady   bool
	settingsPrefs   Prefs
	appearanceIdx   int
	dockIdx         int
	restoreProxyOn  bool
	autoLaunchOn    bool
	silentStartOn   bool
	closeOnSwitchOn bool

	mixedPortDraft string
	unifiedOn      bool
	tunStackPick   string
	networkBase    patch.Overrides
	networkError   string
	networkStatus  string

	testURLDraft         string
	testTimeoutPick      string
	testConcurrencyPick  string
	testPrefsBase        Prefs
	testSettingsError    string
	testSettingsStatus   string
	generalSettingsError string
}

var settingsTimeoutOptions = []string{"3000", "6000", "10000", "15000", "30000"}
var settingsConcurrencyOptions = []string{"1", "4", "8", "16", "32"}
var settingsTunStacks = []string{"mixed", "system", "gvisor"}

func (a *App) initSettings() {
	if a.settingsReady {
		return
	}
	a.settingsReady = true
	a.syncGeneralSettings(a.Prefs())
	a.resetNetworkDraft()
	a.resetTestSettingsDraft(a.Prefs())
}

func (a *App) syncGeneralSettings(p Prefs) {
	a.settingsPrefs = p
	a.appearanceIdx = appearanceIndex(p.Appearance)
	a.dockIdx = dockVisibilityIndex(p.DockVisibility)
	a.lightOn = p.LightMode
	a.lightDelayIdx = delayIndexOf(p.LightDelay)
	a.autoIdx = autoUpdateIndex(p.AutoUpdateHours)
	a.restoreProxyOn = p.RestoreSystemProxy
	a.autoLaunchOn, a.silentStartOn, a.closeOnSwitchOn = p.AutoLaunch, p.SilentStart, p.CloseOnSwitch
}

func (a *App) resetNetworkDraft() {
	a.networkBase = a.ov
	a.mixedPortDraft = strconv.Itoa(a.mixedPort())
	a.lanOn, a.ipv6On = a.ov.AllowLAN, a.ov.IPv6
	a.unifiedOn = a.ov.Unified
	a.tunStackPick = a.ov.TunStack
	if a.tunStackPick == "" {
		a.tunStackPick = "mixed"
	}
}

func (a *App) resetTestSettingsDraft(p Prefs) {
	a.testPrefsBase = p
	a.testURLDraft = p.TestURL
	a.testTimeoutPick = strconv.Itoa(p.TestTimeoutMS)
	a.testConcurrencyPick = strconv.Itoa(p.TestConcurrency)
}

func (a *App) settingsPage(c *ui.Context) {
	pal := paletteFor(c)
	a.initSettings()
	a.reconcileSettings()
	a.pageHeader(c, "设置", "偏好自动保存；网络与测试设置确认后应用", nil)
	softSegmented(c, &a.settingsTab, "常规", "网络", "延迟测试", "维护", "DNS", "域名嗅探").AlignSelf(ui.Start).Label("设置分类")
	if a.settingsTab == 4 || a.settingsTab == 5 {
		a.featureSettingsPage(c, a.settingsTab == 4)
		a.rawConfigModal(c)
		return
	}

	ui.Scroll(c).Key(a.settingsTab).FillWidth().Grow(1).MinHeight(0).Transition(pageMotion()).Children(func() {
		ui.Column(c).FillWidth().Gap(16).Padding(0, 0, 12, 0).Children(func() {
			if a.settingsTab == 0 {
				settingsGroup(c, "外观", func() {
					settingsRow(c, "主题", "窗口与内容统一配色，切换后立即生效", func() {
						softSegmented(c, &a.appearanceIdx, "跟随系统", "浅色", "深色").Label("外观主题")
					})
					if runtime.GOOS == "darwin" {
						hairline(c)
						settingsRow(c, "显示程序坞图标", dockVisibilityDescription(a.settingsPrefs.DockVisibility), func() {
							softSegmented(c, &a.dockIdx, "隐藏", "跟随窗口", "显示").Label("程序坞图标显示方式")
						})
					}
				})
				settingsGroup(c, "启动行为", func() {
					settingsRow(c, "登录时启动 Gopurr", "登录电脑后自动运行，无需手动打开", func() {
						softSwitch(c, &a.autoLaunchOn).Label("登录自启开关")
					})
					hairline(c)
					settingsRow(c, "静默启动", "启动后仅驻留菜单栏或托盘，可随时打开窗口", func() {
						softSwitch(c, &a.silentStartOn).Label("静默启动开关")
					})
					hairline(c)
					settingsRow(c, "启动时开启系统代理", "内核就绪后，将系统网络请求交给 Gopurr", func() {
						softSwitch(c, &a.restoreProxyOn).Label("启动时系统代理开关")
					})
				})
				ui.Text(c, "内核自动运行，关窗后仍可继续代理。").Label("内核自动运行").FontSize(11).TextColor(pal.Faint).PaddingX(4)
				settingsGroup(c, "运行与更新", func() {
					settingsRow(c, "轻量模式", "关窗后释放界面资源，代理继续运行", func() {
						softSwitch(c, &a.lightOn).Label("轻量模式开关")
					})
					hairline(c)
					settingsRow(c, "界面释放等待", "仅轻量模式开启时生效", func() {
						softSegmented(c, &a.lightDelayIdx, "30 秒", "1 分钟", "5 分钟").
							Disabled(!a.lightOn).Label("界面释放等待")
					})
					hairline(c)
					settingsRow(c, "自动更新订阅", "作为订阅的默认周期，可在各订阅中单独调整", func() {
						softSegmented(c, &a.autoIdx, "关闭", "6 小时", "12 小时", "24 小时").Label("自动更新订阅")
					})
				})
			}

			if a.settingsTab == 1 {
				settingsGroup(c, "网络接入", func() {
					settingsRow(c, "混合代理端口", "HTTP 与 SOCKS 共用，范围 1–65535", func() {
						ui.TextInput(c, &a.mixedPortDraft).Width(112).Placeholder("7890").Label("编辑混合代理端口").
							OnSubmit(func() { a.saveNetworkSettings() })
					})
					hairline(c)
					settingsRow(c, "允许局域网连接", "允许同一网络中的设备使用本机代理", func() {
						softSwitch(c, &a.lanOn).Label("局域网访问开关")
					})
					hairline(c)
					settingsRow(c, "IPv6", "允许 IPv6 解析与出站连接", func() {
						softSwitch(c, &a.ipv6On).Label("IPv6 开关")
					})
				})
				settingsGroup(c, "路由与测试", func() {
					settingsRow(c, "统一延迟 · Unified", "排除握手开销，便于比较不同协议的节点", func() {
						softSwitch(c, &a.unifiedOn).Label("统一延迟")
					})
					hairline(c)
					settingsRow(c, "TUN 协议栈", "推荐 mixed；启用 TUN 时使用此设置", func() {
						ui.Select(c, &a.tunStackPick, settingsOptions(settingsTunStacks, a.tunStackPick)).
							Width(140).Label("TUN 协议栈")
					})
				})
				settingsGroup(c, "连接行为 · 即时保存", func() {
					settingsRow(c, "切换节点时断开原连接", "立即保存；只断开经过该策略组的连接", func() {
						softSwitch(c, &a.closeOnSwitchOn).Label("切换节点时断开原连接开关")
					})
				})
			}

			if a.settingsTab == 2 {
				settingsGroup(c, "延迟测试", func() {
					// The address gets its own full-width line so the settings page
					// remains usable in an 840-pixel window beside the sidebar.
					settingsRow(c, "测试地址", "通过待测节点请求此地址；建议使用返回 204 的轻量页面", nil)
					ui.Column(c).FillWidth().Padding(0, 16, 14).Children(func() {
						ui.TextInput(c, &a.testURLDraft).FillWidth().Placeholder(defaultPrefs().TestURL).
							Label("延迟测试地址").OnSubmit(a.saveTestSettings)
					})
					hairline(c)
					settingsRow(c, "单节点超时", "单位：毫秒；超时后标记为不可用", func() {
						ui.Select(c, &a.testTimeoutPick, settingsOptions(settingsTimeoutOptions, a.testTimeoutPick)).
							Width(140).Label("测试超时毫秒")
					})
					hairline(c)
					settingsRow(c, "并发测试数", "同时测试的节点数，网络较弱时建议降低", func() {
						ui.Select(c, &a.testConcurrencyPick, settingsOptions(settingsConcurrencyOptions, a.testConcurrencyPick)).
							Width(140).Label("测试并发数")
					})
				})
			}

			if a.settingsTab == 3 {
				a.backupSettings(c)
				a.softwareUpdateSettings(c)
				settingsGroup(c, "配置与维护", func() {
					settingsRow(c, "当前配置", a.configSource(), func() {
						softButton(c, "查看运行配置…").OnClick(a.openRawConfig)
					})
					hairline(c)
					settingsRow(c, "数据目录", "订阅、覆写与偏好保存在本机", func() {
						softButton(c, "打开文件夹").OnClick(a.revealDataDir)
					})
					ui.Text(c, a.dir).FillWidth().Padding(0, 16, 14).FontSize(11).
						TextColor(pal.Faint).MaxLines(2).Selectable().Tooltip(a.dir)
					hairline(c)
					settingsRow(c, "重载内核", "重新应用当前配置；无需手动启动内核", func() {
						softButton(c, "重载内核").Disabled(a.k == nil).OnClick(a.reloadSettingsKernel)
					})
				})
			}
		})
	})
	if a.generalSettingsError != "" {
		settingsFeedback(c, a.generalSettingsError, true)
	}
	if a.settingsTab == 1 {
		if a.networkError != "" {
			settingsFeedback(c, a.networkError, true)
		} else if a.networkStatus != "" && !a.networkSettingsDirty() {
			settingsFeedback(c, a.networkStatus, false)
		}
		settingsSaveBar(c, "网络设置", "保存后应用到当前内核", a.networkSettingsDirty(), "保存网络设置", func() { a.saveNetworkSettings() })
	}
	if a.settingsTab == 2 {
		if a.testSettingsError != "" {
			settingsFeedback(c, a.testSettingsError, true)
		} else if a.testSettingsStatus != "" && !a.testSettingsDirty() {
			settingsFeedback(c, a.testSettingsStatus, false)
		}
		settingsSaveBar(c, "测试设置", "用于下次节点延迟测试", a.testSettingsDirty(), "保存测试设置", a.saveTestSettings)
	}
	a.rawConfigModal(c)
}

func settingsSaveBar(c *ui.Context, title, hint string, dirty bool, label string, save func()) {
	pal := paletteFor(c)
	ui.Row(c).FillWidth().Padding(12, 16).Radius(10).Background(pal.Surface).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			if dirty {
				title += " · 有未应用更改"
			}
			ui.Text(c, title).FontSize(12).FontWeight(500).TextColor(pal.Text)
			ui.Text(c, hint).FontSize(11).TextColor(pal.Muted)
		})
		softPrimaryButton(c, label).Disabled(!dirty).OnClick(save)
	})
}

// reconcileSettings commits only fields the user actually changed. All edits
// in a frame are merged into the latest preferences and saved once, preserving
// changes made by the subscription updater and avoiding stale-copy clobbering.
func (a *App) reconcileSettings() {
	a.initSettings()
	last := a.settingsPrefs
	appearanceChanged := a.appearanceIdx != appearanceIndex(last.Appearance)
	dockChanged := a.dockIdx != dockVisibilityIndex(last.DockVisibility)
	lightChanged := a.lightOn != last.LightMode
	delayChanged := a.lightDelayIdx != delayIndexOf(last.LightDelay)
	autoChanged := a.autoIdx != autoUpdateIndex(last.AutoUpdateHours)
	restoreChanged := a.restoreProxyOn != last.RestoreSystemProxy
	startupChanged := a.autoLaunchOn != last.AutoLaunch
	silentChanged := a.silentStartOn != last.SilentStart
	cleanupChanged := a.closeOnSwitchOn != last.CloseOnSwitch
	if startupChanged || silentChanged || cleanupChanged || appearanceChanged || dockChanged || lightChanged || delayChanged || autoChanged || restoreChanged {
		err := a.changePrefs(func(p *Prefs) {
			if startupChanged {
				p.AutoLaunch = a.autoLaunchOn
			}
			if silentChanged {
				p.SilentStart = a.silentStartOn
			}
			if cleanupChanged {
				p.CloseOnSwitch = a.closeOnSwitchOn
			}
			if appearanceChanged && a.appearanceIdx >= 0 && a.appearanceIdx < len(appearances) {
				p.Appearance = appearances[a.appearanceIdx]
			}
			if dockChanged && a.dockIdx >= 0 && a.dockIdx < len(dockVisibilities) {
				p.DockVisibility = dockVisibilities[a.dockIdx]
			}
			if lightChanged {
				p.LightMode = a.lightOn
			}
			if delayChanged && a.lightDelayIdx >= 0 && a.lightDelayIdx < len(lightDelays) {
				p.LightDelay = lightDelays[a.lightDelayIdx]
			}
			if autoChanged && a.autoIdx >= 0 && a.autoIdx < len(autoUpdateHours) {
				hours := autoUpdateHours[a.autoIdx]
				if p.AutoUpdateHours == 0 && hours > 0 {
					p.LastAutoUpdate = time.Now()
				}
				p.AutoUpdateHours = hours
			}
			if restoreChanged {
				p.RestoreSystemProxy = a.restoreProxyOn
			}
		})
		a.generalSettingsError = ""
		if err != nil {
			a.generalSettingsError = "设置保存失败：" + err.Error()
			a.notice = a.generalSettingsError
		}
	}
	a.syncGeneralSettings(a.Prefs())
}

func (a *App) networkSettingsDirty() bool {
	base := a.networkBase
	port := base.MixedPort
	if port <= 0 {
		port = 7890
	}
	stack := base.TunStack
	if stack == "" {
		stack = "mixed"
	}
	return strings.TrimSpace(a.mixedPortDraft) != strconv.Itoa(port) || a.lanOn != base.AllowLAN ||
		a.ipv6On != base.IPv6 || a.unifiedOn != base.Unified || a.tunStackPick != stack
}

func parseSettingsPort(raw string) (int, error) {
	s := strings.TrimSpace(raw)
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("混合端口须为 1–65535 的整数")
		}
	}
	port, err := strconv.Atoi(s)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("混合端口须为 1–65535 的整数")
	}
	return port, nil
}

// saveNetworkSettings validates and hot-reloads before persisting a candidate.
// A failed reload or write restores the previous overrides and runtime config;
// the draft stays editable so the user can fix the error and retry.
func (a *App) saveNetworkSettings() bool {
	a.networkError, a.networkStatus = "", ""
	port, err := parseSettingsPort(a.mixedPortDraft)
	if err != nil {
		a.networkError = err.Error()
		return false
	}
	if !containsString(settingsTunStacks, a.tunStackPick) {
		a.networkError = "TUN 协议栈须为 mixed、system 或 gvisor"
		return false
	}
	if port != a.mixedPort() && !portFree(port) {
		a.networkError = fmt.Sprintf("端口 %d 已被占用，请更换端口", port)
		return false
	}
	previous, previousPort := a.ov, a.mixedPort()
	running := a.k != nil && a.k.Running()
	next := previous
	next.MixedPort = port
	next.AllowLAN, next.IPv6, next.Unified = a.lanOn, a.ipv6On, a.unifiedOn
	next.TunStack = a.tunStackPick
	a.ov = next
	if _, err := a.configBytes(); err != nil {
		a.ov = previous
		a.networkError = "网络配置校验失败：" + err.Error()
		a.notice = a.networkError
		return false
	}
	if running && !a.startKernel() {
		failure := "网络配置重载失败：" + a.notice
		a.rollbackNetworkSettings(previous, running, failure)
		return false
	}
	if err := writeSettingsFile(a.ovPath, next); err != nil {
		a.rollbackNetworkSettings(previous, running, "网络设置保存失败："+err.Error())
		return false
	}
	if a.sysApplied && port != previousPort {
		// setSystemProxy only captures the previous owner's proxy when
		// !sysApplied, so this updates our port without overwriting a.prev.
		a.setSystemProxy(true)
		if errText := a.sysErrText(); errText != "" {
			failure := "系统代理端口更新失败：" + errText
			if err := writeSettingsFile(a.ovPath, previous); err != nil {
				failure += "；恢复原网络设置文件失败：" + err.Error()
			}
			a.rollbackNetworkSettings(previous, running, failure)
			failure = a.networkError
			// The OS may have applied part of the privileged script before
			// failing. Reapply our old port after restoring the old listener.
			a.setSystemProxy(true)
			if restoreErr := a.sysErrText(); restoreErr != "" {
				failure += "；恢复原系统代理失败：" + restoreErr
			}
			a.networkError, a.notice = failure, failure
			return false
		}
	}
	a.resetNetworkDraft()
	a.networkStatus = "网络设置已保存并应用"
	if !running {
		a.networkStatus = "网络设置已保存，内核就绪后生效"
	}
	a.notice = a.networkStatus
	return true
}

func (a *App) rollbackNetworkSettings(previous patch.Overrides, running bool, failure string) {
	a.ov = previous
	if running && !a.startKernel() {
		failure += "；恢复原内核配置失败：" + a.notice
	}
	a.networkError = failure
	a.notice = failure
}

func (a *App) testSettingsDirty() bool {
	return strings.TrimSpace(a.testURLDraft) != a.testPrefsBase.TestURL ||
		a.testTimeoutPick != strconv.Itoa(a.testPrefsBase.TestTimeoutMS) ||
		a.testConcurrencyPick != strconv.Itoa(a.testPrefsBase.TestConcurrency)
}

func (a *App) saveTestSettings() {
	a.testSettingsError, a.testSettingsStatus = "", ""
	testURL := strings.TrimSpace(a.testURLDraft)
	timeout, timeoutErr := strconv.Atoi(a.testTimeoutPick)
	concurrency, concurrencyErr := strconv.Atoi(a.testConcurrencyPick)
	if timeoutErr != nil || concurrencyErr != nil {
		a.testSettingsError = "请选择有效的测试超时和并发数"
		return
	}
	if err := validateTestSettings(testURL, timeout, concurrency); err != nil {
		a.testSettingsError = err.Error()
		return
	}
	if err := a.changePrefs(func(p *Prefs) {
		p.TestURL, p.TestTimeoutMS, p.TestConcurrency = testURL, timeout, concurrency
	}); err != nil {
		a.testSettingsError = "测试设置保存失败：" + err.Error()
		a.notice = a.testSettingsError
		return
	}
	a.resetTestSettingsDraft(a.Prefs())
	a.testSettingsStatus = "测试设置已保存，下次测速生效"
	a.notice = a.testSettingsStatus
}

func (a *App) reloadSettingsKernel() {
	if a.k == nil {
		a.notice = "内核客户端未初始化"
		return
	}
	if a.startKernel() {
		a.notice = "内核已重载"
	}
}

// Keep valid custom values from an existing preference file selectable without
// silently replacing them with the first built-in option on the next frame.
func settingsOptions(options []string, current string) []string {
	if current == "" || containsString(options, current) {
		return options
	}
	return append(append([]string(nil), options...), current)
}

func settingsFeedback(c *ui.Context, message string, failed bool) {
	pal := paletteFor(c)
	color := pal.Success
	if failed {
		color = pal.Danger
	}
	ui.Text(c, message).FillWidth().Padding(10, 16).FontSize(11).TextColor(color).ContentTransition(contentMotion())
}
