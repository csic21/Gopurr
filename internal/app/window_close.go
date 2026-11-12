package app

import ui "gopurr/internal/appui"

type windowCloseState struct {
	show  bool
	close func()
	error string
}

// The first close explains the tray lifecycle before hiding the only window.
func (a *App) RequestWindowClose(close func()) {
	if a.Prefs().CloseToTrayAcknowledged {
		close()
		return
	}
	a.windowClose = windowCloseState{show: true, close: close}
	a.invalidate()
}
func (a *App) windowCloseDialog(c *ui.Context) {
	message := tr("关闭窗口后，Gopurr 会继续在菜单栏或托盘运行，代理保持当前状态。要停止并退出，请使用菜单中的“退出 Gopurr”。")
	if a.windowClose.error != "" {
		message += "\n" + a.windowClose.error
	}
	if softAlertDialog(c, &a.windowClose.show, tr("关闭窗口后继续代理"), message, tr("取消"), tr("继续后台运行")) == 1 {
		if err := a.changePrefs(func(p *Prefs) { p.CloseToTrayAcknowledged = true }); err != nil {
			a.windowClose.error, a.windowClose.show = err.Error(), true
			return
		}
		close := a.windowClose.close
		a.windowClose = windowCloseState{}
		if close != nil {
			close()
		}
	}
}
