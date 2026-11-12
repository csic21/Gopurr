package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	ui "gopurr/internal/appui"
	"gopurr/internal/patch"
	"gopurr/internal/profile"
	"gopurr/internal/workflow"
)

type subscriptionEditState struct {
	editSub       profile.Profile
	showEditSub   bool
	editSubHours  string
	editSubPolicy int
	editSubError  string
}

func (a *App) openSubscriptionEditor(p profile.Profile) {
	if p.IsAggregate() {
		a.openAggregationEditor(p.ID)
		return
	}
	a.editSub = p
	a.showEditSub = true
	a.editSubError = ""
	a.editSubPolicy = 0
	a.editSubHours = "24"
	if p.UpdateHours != nil {
		if *p.UpdateHours == 0 {
			a.editSubPolicy = 1
		} else {
			a.editSubPolicy = 2
			a.editSubHours = strconv.Itoa(*p.UpdateHours)
		}
	}
}
func (a *App) saveSubscriptionEditor() {
	if a.store == nil || !a.beginProfileWork() {
		a.editSubError = tr("订阅正在更新，请稍后重试")
		return
	}
	defer a.endProfileWork()
	p := a.editSub
	p.Name, p.URL, p.UserAgent = strings.TrimSpace(p.Name), strings.TrimSpace(p.URL), strings.TrimSpace(p.UserAgent)
	p.UpdateHours = nil
	if a.editSubPolicy == 1 {
		hours := 0
		p.UpdateHours = &hours
	}
	if a.editSubPolicy == 2 {
		hours, err := strconv.Atoi(a.editSubHours)
		if err != nil || hours < 1 || hours > 8760 {
			a.editSubError = tr("更新间隔须为 1–8760 小时")
			return
		}
		p.UpdateHours = &hours
	}
	if err := a.store.Edit(p); err != nil {
		a.editSubError = err.Error()
		return
	}
	a.showEditSub = false
	a.successNotice(tr("订阅设置已保存；地址或 UA 更改后，点击“更新”获取新配置。"))
	if p.IsFile() {
		a.successNotice(tr("本地配置名称已保存"))
	}
}
func (a *App) subscriptionEditor(c *ui.Context) {
	pal := paletteFor(c)
	softModal(c, "subscription-edit", &a.showEditSub, func() {
		busy, _ := a.subState()
		ui.Column(c).Width(540).Padding(20).Gap(12).Children(func() {
			ui.Text(c, tr("编辑订阅")).FontSize(16).Bold().TextColor(pal.Text)
			ui.Scroll(c).Height(280).FillWidth().Children(func() {
				ui.Column(c).FillWidth().Gap(10).Children(func() {
					ui.Text(c, tr("配置名称")).FontSize(11).TextColor(pal.Muted)
					ui.TextInput(c, &a.editSub.Name).FillWidth().Label(tr("编辑订阅名称"))
					if !a.editSub.IsFile() {
						ui.Text(c, tr("订阅链接")).FontSize(11).TextColor(pal.Muted)
						ui.TextInput(c, &a.editSub.URL).FillWidth().Label(tr("编辑订阅链接"))
						ui.Text(c, tr("User-Agent · 留空使用默认值")).FontSize(11).TextColor(pal.Muted)
						ui.TextInput(c, &a.editSub.UserAgent).FillWidth().Placeholder("clash.meta/mihomo").Label(tr("订阅 User-Agent"))
						ui.Text(c, tr("自动更新")).FontSize(11).TextColor(pal.Muted)
						softSegmented(c, &a.editSubPolicy, tr("跟随全局"), tr("已关闭"), tr("自定义")).AlignSelf(ui.Start).Label(tr("订阅自动更新方式"))
						if a.editSubPolicy == 2 {
							ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
								ui.TextInput(c, &a.editSubHours).Width(100).Label(tr("订阅更新间隔小时"))
								ui.Text(c, tr("小时")).FontSize(12).TextColor(pal.Muted)
							})
						}
						settingsRow(c, tr("使用 Gopurr 代理更新"), tr("需要内核运行；关闭时直接下载"), func() { softSwitch(c, &a.editSub.UseProxy).Label(tr("使用代理更新订阅")) })
					} else {
						ui.Text(c, tr("本地配置只在点击“重新读取”时更新。")).FontSize(11).TextColor(pal.Faint)
					}
				})
			})
			if a.editSubError != "" {
				settingsFeedback(c, a.editSubError, true)
			}
			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				softButton(c, tr("取消")).OnClick(func() { a.showEditSub = false })
				softPrimaryButton(c, tr("保存订阅设置")).Disabled(busy).OnClick(a.saveSubscriptionEditor)
			})
		})
	})
}
func subscriptionUpdateLabel(p profile.Profile, globalHours ...int) string {
	if p.IsAggregate() {
		return tr("来源更新后自动重新聚合")
	}
	if p.IsFile() {
		return tr("手动读取")
	}
	if p.UpdateHours == nil {
		hours := 0
		if len(globalHours) > 0 {
			hours = globalHours[0]
		}
		if hours == 0 {
			return tr("跟随全局 · 自动更新已关闭")
		}
		return fmt.Sprintf(tr("跟随全局 · 每 %d 小时更新"), hours)
	}
	if *p.UpdateHours == 0 {
		return tr("手动更新")
	}
	return fmt.Sprintf(tr("每 %d 小时更新"), *p.UpdateHours)
}

// refreshManagedProfile keeps both the cache and running config intact when
// a changed source downloads valid YAML that the kernel cannot accept.
func (a *App) refreshManagedProfile(id string) error {
	return a.refreshManagedProfileContext(context.Background(), id)
}
func (a *App) refreshManagedProfileContext(ctx context.Context, id string) error {
	if err := a.refreshManagedProfileOnlyContext(ctx, id); err != nil {
		return err
	}
	return a.refreshDependentAggregatesContext(context.WithoutCancel(ctx), id)
}
func (a *App) refreshDependentAggregates(id string) error {
	return a.refreshDependentAggregatesContext(context.Background(), id)
}
func (a *App) refreshDependentAggregatesContext(ctx context.Context, id string) error {
	var failures []error
	for _, dependent := range a.store.DependentAggregates(id) {
		if err := a.refreshManagedProfileOnlyContext(ctx, dependent.ID); err != nil {
			failures = append(failures, fmt.Errorf(tr("来源已更新；聚合配置 %s 保留原版本：%w"), dependent.Name, err))
		}
	}
	return errors.Join(failures...)
}
func (a *App) refreshManagedProfileOnly(id string) error {
	return a.refreshManagedProfileOnlyContext(context.Background(), id)
}
func (a *App) refreshManagedProfileOnlyContext(ctx context.Context, id string) error {
	active := a.store.ActiveID() == id
	running := active && a.k != nil && a.k.RunningForChange()
	var previous []byte
	if running {
		var err error
		previous, err = a.configBytes()
		if err != nil {
			previous, err = a.lastAcceptedConfig()
		}
		if err != nil {
			return err
		}
	}
	_, err := workflow.RunProfileChange(ctx, workflow.ProfileChange{
		Persist: func(accept func(profile.Profile, []byte) error) (profile.Profile, error) {
			err := workflow.Refresh(ctx, a.store, id, func(raw []byte) error {
				item, err := a.store.Get(id)
				if err != nil {
					return err
				}
				return accept(item, raw)
			})
			if err != nil {
				return profile.Profile{}, err
			}
			return a.store.Get(id)
		},
		Prepare: func(_ context.Context, item profile.Profile, raw []byte) (workflow.ProfilePlan, error) {
			local := patch.ProfileOverrides{}
			if item.Overrides != nil {
				local = *item.Overrides
			}
			global := a.runtimeOverrides()
			if !active {
				global.ExtraRules, global.AppRules, global.TunEnable = nil, nil, false
			}
			candidate, err := patch.ApplyLayers(raw, global, local)
			return workflow.ProfilePlan{Config: candidate, Apply: running}, err
		},
		Apply:   func(_ context.Context, raw []byte) error { return a.k.Start(raw) },
		Restore: func(context.Context) error { return a.k.Start(previous) },
	})
	if err == nil && active {
		a.cancelDelayTest()
		a.bmu.Lock()
		a.delays = map[string]uint16{}
		a.delayErrors = map[string]string{}
		a.bmu.Unlock()
	}
	return err
}
