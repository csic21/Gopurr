package app

import "gopurr/internal/profile"

// OpenSubscriptionURL stages an external request in the regular editor. It
// never downloads, replaces a draft or changes the active profile by itself.
func (a *App) OpenSubscriptionURL(raw string) {
	if a.ShuttingDown() {
		return
	}
	link, err := profile.ParseImportLink(raw)
	if err != nil {
		a.notice = err.Error()
		a.invalidate()
		return
	}
	busy, _ := a.subState()
	if busy || a.showEditSub || a.showRestore || a.showDeleteSub || a.subURL != "" || a.subName != "" {
		a.notice = tr("已有未完成的订阅操作，请完成或清空后重新打开导入链接")
		a.invalidate()
		return
	}
	a.page, a.showAddSub, a.subAddPending = "subs", true, false
	a.subURL, a.subName = link.URL, link.Name
	a.setSubErr("")
	a.notice = tr("已收到订阅链接，请确认来源后点击添加")
	a.invalidate()
}
