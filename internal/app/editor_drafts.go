package app

import (
	"encoding/json"
	ui "gopurr/internal/appui"
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
func (a *App) aggregationFingerprint() string {
	raw, _ := json.Marshal(struct {
		Name             string
		Sources          []aggregationSourceDraft
		Include, Exclude string
		Dedupe           bool
	}{a.aggregationName, a.aggregationSources, a.aggregationInclude, a.aggregationExclude, a.aggregationDedupe})
	return string(raw)
}
func (a *App) requestCloseAggregation() {
	if busy, _ := a.subState(); busy {
		return
	}
	if a.aggregationFingerprint() != a.aggregationSaved {
		a.aggregationDiscard = true
		return
	}
	a.showAggregation = false
}
func confirmDiscardDraft(c *ui.Context, open *bool) bool {
	// Keep editing is the safe default for Enter and Escape.
	return softAlertDialog(c, open, tr("放弃未保存的修改？"), tr("关闭后将丢弃本次草稿，已保存的配置不受影响。"), tr("放弃修改"), tr("继续编辑")) == 0
}

func (a *App) requestSaveProxyEditor() {
	edits, err := a.proxyEditorEdits()
	if err != nil {
		a.proxyEditorError = err.Error()
		return
	}
	id := a.proxyEditorID
	local := a.profileOverrides(id)
	local.ProxyEdits = edits
	a.proxyEditorError = ""
	a.requestOverrideChange(id, a.ov, local, func(_ bool, err error) {
		if err != nil {
			a.proxyEditorError = err.Error()
			return
		}
		a.showProxyEditor = false
		a.successNotice(tr("节点与策略组修改已保存"))
	})
}
