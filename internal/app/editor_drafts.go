package app

import (
	"encoding/json"
	"github.com/egoist/mygo/ui"
)

func (a *App) proxyEditorFingerprint() string {
	groups := make(map[string]proxyGroupForm, len(a.proxyEditorGroups))
	for key, form := range a.proxyEditorGroups {
		copy := *form
		copy.MemberPick = ""
		groups[key] = copy
	}
	raw, _ := json.Marshal(struct {
		Nodes                 map[string]*proxyNodeForm
		Groups                map[string]proxyGroupForm
		NodeOrder, GroupOrder []string
	}{a.proxyEditorNodes, groups, a.proxyEditorNodeOrder, a.proxyEditorGroupOrder})
	return string(raw)
}
func (a *App) requestCloseProxyEditor() {
	if busyNetwork(a) {
		return
	}
	if a.proxyEditorFingerprint() != a.proxyEditorSaved {
		a.proxyEditorDiscard = true
		return
	}
	a.showProxyEditor = false
}
func confirmDiscardDraft(c *ui.Context, open *bool) bool {
	// Keep editing is the safe default for Enter and Escape.
	return softAlertDialog(c, open, tr("放弃未保存的修改？"), tr("关闭后将丢弃本次草稿，已保存的配置不受影响。"), tr("放弃修改"), tr("继续编辑")) == 0
}
