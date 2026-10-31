package app

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/shortcuts"
	"gopurr/internal/sysproxy"
)

type shortcutState struct {
	shortcutManager *shortcuts.Manager
	showShortcuts   bool
	shortcutDraft   shortcuts.Config
	shortcutError   string
}

func shortcutLabel(action shortcuts.Action) string {
	switch action {
	case shortcuts.ShowWindow:
		return tr("显示主窗口")
	case shortcuts.ToggleProxy:
		return tr("切换系统代理")
	case shortcuts.ToggleTUN:
		return tr("切换 TUN")
	case shortcuts.RuleMode:
		return tr("切换到规则模式")
	case shortcuts.GlobalMode:
		return tr("切换到全局模式")
	case shortcuts.DirectMode:
		return tr("切换到直连模式")
	}
	return ""
}

// ConfigureShortcuts is called once the native application is ready. Native
// registrations outlive windows, including light-mode window destruction.
func (a *App) ConfigureShortcuts(backend shortcuts.Backend, openWindow func()) {
	a.shortcutManager = shortcuts.New(backend, func(action shortcuts.Action) {
		a.RunShortcut(action, openWindow)
	})
	x, err := a.shortcutManager.Prepare(a.Prefs().Shortcuts)
	if err != nil {
		a.shortcutError = fmt.Errorf(tr("快捷键无法启用，请检查组合是否被占用：%w"), err).Error()
		a.notice = a.shortcutError
		return
	}
	x.Commit()
}

func (a *App) CloseShortcuts() {
	if a.shortcutManager != nil {
		a.shortcutManager.Close()
	}
}

// RunShortcut uses the same network operations as the tray. A missing TUN
// service opens Overview; pressing a key never installs a service implicitly.
func (a *App) RunShortcut(action shortcuts.Action, openWindow func()) {
	if a.ShuttingDown() {
		return
	}
	if action == shortcuts.ShowWindow {
		openWindow()
		return
	}
	a.notice = ""
	_, busy, _ := a.svcState()
	if busy || a.k == nil {
		a.notice = tr("网络服务暂不可用，请稍后重试")
	} else {
		switch action {
		case shortcuts.ToggleProxy:
			if sysproxy.Available() {
				if st, err := sysproxy.Current(); err != nil {
					a.notice = tr("无法读取系统代理：") + err.Error()
				} else if st.Supported {
					a.sysApplied = ownsProxy(st, a.mixedPort())
					a.setSystemProxy(!a.sysApplied)
				}
			} else {
				a.notice = tr("此系统暂不支持系统代理")
			}
		case shortcuts.ToggleTUN:
			if !a.tunOn && !a.k.IsRoot() {
				a.notice = tr("请先在概览中安装或修复 TUN 服务")
			} else {
				a.applyTun(!a.tunOn)
			}
		case shortcuts.RuleMode:
			a.UseMode("rule")
		case shortcuts.GlobalMode:
			a.UseMode("global")
		case shortcuts.DirectMode:
			a.UseMode("direct")
		}
	}
	a.invalidate()
	if a.notice != "" || a.sysErrText() != "" || a.tunErrText() != "" {
		a.page = "overview"
		openWindow()
	}
}

func (a *App) openShortcutSettings() {
	a.shortcutDraft = a.Prefs().Shortcuts
	for action, key := range a.shortcutDraft {
		modifier := "Win+"
		if runtime.GOOS == "darwin" {
			modifier = "Cmd+"
		}
		a.shortcutDraft[action] = strings.Replace(key, "Super+", modifier, 1)
	}
	a.showShortcuts = true
}

func (a *App) saveShortcutSettings() {
	next, err := a.shortcutDraft.Normalized()
	if err != nil {
		a.shortcutError = fmt.Errorf(tr("快捷键格式无效或重复：%w"), err).Error()
		return
	}
	// Prepare even when preferences match: a startup conflict may have left
	// the saved bindings unregistered, and Save is also the retry action.
	if a.shortcutManager != nil && next == a.Prefs().Shortcuts {
		x, err := a.shortcutManager.Prepare(next)
		if err != nil {
			a.shortcutError = fmt.Errorf(tr("快捷键无法启用，请检查组合是否被占用：%w"), err).Error()
			return
		}
		x.Commit()
		a.shortcutDraft, a.shortcutError, a.showShortcuts = next, "", false
		return
	}
	if err := a.changePrefs(func(p *Prefs) { p.Shortcuts = next }); err != nil {
		a.shortcutError = err.Error()
		return
	}
	a.shortcutDraft, a.shortcutError, a.showShortcuts = next, "", false
}

func (a *App) shortcutDialog(c *ui.Context) {
	if !a.showShortcuts {
		return
	}
	pal := paletteFor(c)
	width, height := c.Size()
	ui.Overlay(c, func() {
		back := ui.Box(c.Key("global-shortcuts")).Absolute().Left(0).Top(0).Right(0).Bottom(0).Center().Modal()
		modalBackdrop(back)
		if back.OverlayShortcut(0, ui.KeyEscape) {
			a.showShortcuts = false
		}
		back.Children(func() {
			panel := ui.Column(c).Width(min(float32(580), width-64)).MaxHeight(height-64).Padding(20).Gap(12).Radius(14).Background(pal.Surface).Border(1, pal.Border).Role(ui.RoleDialog).Label(tr("全局快捷键"))
			modalPanel(c, panel)
			panel.Children(func() {
				ui.Text(c, tr("全局快捷键")).FontSize(18).FontWeight(600).TextColor(pal.Text)
				ui.Text(c, tr("在其他应用中也可使用；留空即关闭。关闭 Gopurr 后释放。")).FillWidth().FontSize(11.5).TextColor(pal.Muted)
				ui.Scroll(c).FillWidth().Shrink(1).MinHeight(0).Children(func() {
					ui.Column(c).FillWidth().Gap(8).Children(func() {
						for action := shortcuts.ShowWindow; action < shortcuts.Count; action++ {
							ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
								ui.Text(c, shortcutLabel(action)).Grow(1).FontSize(13).TextColor(pal.Text)
								ui.TextInput(c, &a.shortcutDraft[action]).Width(250).Placeholder(tr("未设置")).Label(shortcutLabel(action))
							})
						}
						ui.Text(c, tr("输入组合，如 Ctrl+Alt+P 或 CmdOrCtrl+Shift+Space。支持字母、数字、F1–F12 与 Space；Mac 可用 Cmd、Option。")).FillWidth().FontSize(11).TextColor(pal.Muted)
						if a.shortcutError != "" {
							settingsFeedback(c, a.shortcutError, true)
						}
					})
				})
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					softButton(c, tr("清空全部")).OnClick(func() { a.shortcutDraft = shortcuts.Config{}; a.shortcutError = "" })
					ui.Spacer(c)
					softButton(c, tr("取消")).OnClick(func() { a.showShortcuts = false })
					softPrimaryButton(c, tr("保存快捷键")).OnClick(a.saveShortcutSettings)
				})
			})
		})
	})
}
