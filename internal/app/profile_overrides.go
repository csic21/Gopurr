package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/patch"
	"gopurr/internal/profile"
)

func (a *App) profileOverrides(id string) patch.ProfileOverrides {
	if a.store != nil {
		if p, err := a.store.Get(id); err == nil && p.Overrides != nil {
			return *p.Overrides
		}
	}
	return patch.ProfileOverrides{}
}

func (a *App) scopedSavedStatus(applied bool) string {
	if !applied && a.extensionScope == 1 {
		return tr("已保存 · 启用此订阅时生效")
	}
	return extensionSavedStatus(applied)
}

func (a *App) commitProfileOverrides(id string, next patch.ProfileOverrides) (bool, error) {
	if a.store == nil || id == "" {
		return false, errors.New(tr("请先选择订阅"))
	}
	if !a.beginProfileWork() {
		return false, errors.New(tr("订阅正在更新，请稍后重试"))
	}
	defer a.endProfileWork()
	raw, err := a.store.Content(id)
	if err != nil {
		return false, err
	}
	candidate, err := patch.ApplyLayers(raw, a.runtimeOverrides(), next)
	if err != nil {
		return false, fmt.Errorf(tr("订阅配置验证失败：%w"), err)
	}
	running := a.store.ActiveID() == id && a.k != nil && a.k.Running()
	var previous []byte
	if running {
		previous, err = a.configBytes()
		if err != nil {
			return false, err
		}
		if err := a.k.Start(candidate); err != nil {
			return false, errors.Join(fmt.Errorf(tr("内核未接受订阅覆写：%w"), err), a.k.Start(previous))
		}
	}
	if err := a.store.SetOverrides(id, next); err != nil {
		if running {
			err = errors.Join(err, a.k.Start(previous))
		}
		return false, fmt.Errorf(tr("保存失败，已保留原配置：%w"), err)
	}
	a.featureCacheAt = time.Time{}
	if running {
		a.cancelDelayTest()
		a.proxyGroup = ""
	}
	return running, nil
}

func (a *App) openProfileOverrides(p profile.Profile) {
	a.page, a.extensionTab, a.extensionScope = "rules", 2, 1
	a.extensionProfileID = p.ID
}

func (a *App) extensionScopeControls(c *ui.Context) {
	var items []profile.Profile
	if a.store != nil {
		items = a.store.List()
	}
	if a.extensionScope == 1 {
		found := false
		for _, p := range items {
			found = found || p.ID == a.extensionProfileID
		}
		if !found && len(items) > 0 {
			a.extensionProfileID = a.store.ActiveID()
			if a.extensionProfileID == "" {
				a.extensionProfileID = items[0].ID
			}
		} else if !found {
			a.extensionScope = 0
		}
	}
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		softSegmented(c, &a.extensionScope, tr("全局"), tr("按订阅")).Disabled(len(items) == 0).Label(tr("覆写作用范围"))
		if a.extensionScope == 1 && len(items) > 0 {
			labels := make([]string, len(items))
			selected := ""
			for i, p := range items {
				labels[i] = fmt.Sprintf("%d. %s", i+1, p.Name)
				if p.ID == a.extensionProfileID {
					selected = labels[i]
				}
			}
			ui.Select(c, &selected, labels).Grow(1).MinWidth(0).Label(tr("选择覆写订阅")).OnChange(func() {
				for i, label := range labels {
					if selected == label {
						a.extensionProfileID = items[i].ID
					}
				}
			})
		} else {
			ui.Text(c, tr("应用到所有配置")).Grow(1).MinWidth(0).FontSize(11).TextColor(paletteFor(c).Muted)
		}
		softButton(c, tr("预览配置…")).OnClick(a.previewExtensionConfig)
	})
	ui.Text(c, tr("合并顺序：原配置 → 全局覆写 → 订阅覆写 → 应用网络设置")).FillWidth().FontSize(11).TextColor(paletteFor(c).Faint)
}

func (a *App) previewExtensionConfig() {
	a.initExtensionDrafts()
	global := a.ov
	id := ""
	if a.store != nil {
		id = a.store.ActiveID()
	}
	if a.extensionScope == 1 {
		id = a.extensionProfileID
	} else {
		global.ExtraRules, global.MergeYAML = extraRuleLines(a.extraRulesDraft), a.mergeYAMLDraft
		if a.dnsReady {
			global.DNS = a.dnsDraft.settings()
		}
	}
	raw := []byte(a.cfgText)
	local := a.profileOverrides(id)
	if id != "" {
		var err error
		raw, err = a.store.Content(id)
		if err != nil {
			a.notice = err.Error()
			return
		}
	}
	if a.extensionScope == 1 {
		local.ExtraRules, local.MergeYAML = extraRuleLines(a.extraRulesDraft), a.mergeYAMLDraft
		local.DNS = a.profileDNSDraft.settings()
	}
	composed, err := patch.ApplyLayers(raw, global, local)
	if err != nil {
		a.rawError = err.Error()
		a.rawDraft = tr("# 无法生成预览，请修正覆写草稿。")
	} else {
		a.rawDraft, a.rawError = string(composed), ""
	}
	a.showRaw, a.rawReadOnly, a.rawPreview = true, true, true
}

func (a *App) resetProfileDNSDraft() {
	d := patch.DefaultDNSSettings()
	local := a.profileOverrides(a.extensionProfileID)
	if a.store != nil && a.extensionProfileID != "" {
		if raw, err := a.store.Content(a.extensionProfileID); err == nil {
			if composed, err := patch.ApplyLayers(raw, a.ov, local); err == nil {
				d, _, _ = patch.FeatureSettings(composed)
			}
		}
	}
	source := 0
	if local.DNS != nil {
		d, source = *local.DNS, 1
	}
	a.profileDNSDraft = dnsDraft{Source: source, Enable: d.Enable, IPv6: d.IPv6, Mode: d.EnhancedMode,
		Servers: strings.Join(d.Nameserver, "\n"), Bootstrap: strings.Join(d.DefaultNameserver, "\n"),
		Fallback: strings.Join(d.Fallback, "\n"), Filter: strings.Join(d.FakeIPFilter, "\n")}
	a.profileDNSBase = a.profileDNSDraft
}

func (a *App) profileDNSEditor(c *ui.Context) {
	ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(16).Children(func() {
			a.dnsFields(c, &a.profileDNSDraft, tr("仅对所选订阅生效。跟随配置时沿用原配置、全局 DNS 和订阅 YAML；自定义 DNS 最后覆盖这些字段。"))
		})
	})
	extensionFeedback(c, a.profileDNSError, a.profileDNSStatus)
	extensionSaveBar(c, a.profileDNSDraft != a.profileDNSBase, tr("保存订阅 DNS"), func() {
		a.profileDNSDraft = a.profileDNSBase
		a.profileDNSError, a.profileDNSStatus = "", ""
	}, func() {
		local := a.profileOverrides(a.extensionProfileID)
		local.DNS = a.profileDNSDraft.settings()
		applied, err := a.commitProfileOverrides(a.extensionProfileID, local)
		if err != nil {
			a.profileDNSError = err.Error()
			return
		}
		a.profileDNSBase = a.profileDNSDraft
		a.profileDNSError, a.profileDNSStatus = "", a.scopedSavedStatus(applied)
	})
}
