package app

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/patch"
	"gopurr/internal/profile"
)

type subscriptionEditState struct {
	editSub       profile.Profile
	showEditSub   bool
	editSubHours  string
	editSubPolicy int
	editSubError  string
}

func (a *App) openSubscriptionEditor(p profile.Profile) {
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
		a.editSubError = "订阅正在更新，请稍后重试"
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
			a.editSubError = "更新间隔须为 1–8760 小时"
			return
		}
		p.UpdateHours = &hours
	}
	if err := a.store.Edit(p); err != nil {
		a.editSubError = err.Error()
		return
	}
	a.showEditSub = false
	a.notice = "订阅设置已保存；地址或 UA 更改后，点击“更新”获取新配置。"
	if p.IsFile() {
		a.notice = "本地配置名称已保存"
	}
}
func (a *App) subscriptionEditor(c *ui.Context) {
	pal := paletteFor(c)
	softModal(c, "subscription-edit", &a.showEditSub, func() {
		busy, _ := a.subState()
		ui.Column(c).Width(540).Padding(20).Gap(12).Children(func() {
			ui.Text(c, "编辑订阅").FontSize(16).Bold().TextColor(pal.Text)
			ui.Scroll(c).Height(280).FillWidth().Children(func() {
				ui.Column(c).FillWidth().Gap(10).Children(func() {
					ui.Text(c, "配置名称").FontSize(11).TextColor(pal.Muted)
					ui.TextInput(c, &a.editSub.Name).FillWidth().Label("编辑订阅名称")
					if !a.editSub.IsFile() {
						ui.Text(c, "订阅链接").FontSize(11).TextColor(pal.Muted)
						ui.TextInput(c, &a.editSub.URL).FillWidth().Label("编辑订阅链接")
						ui.Text(c, "User-Agent · 留空使用默认值").FontSize(11).TextColor(pal.Muted)
						ui.TextInput(c, &a.editSub.UserAgent).FillWidth().Placeholder("clash.meta/mihomo").Label("订阅 User-Agent")
						ui.Text(c, "自动更新").FontSize(11).TextColor(pal.Muted)
						softSegmented(c, &a.editSubPolicy, "跟随全局", "关闭", "自定义").AlignSelf(ui.Start).Label("订阅自动更新方式")
						if a.editSubPolicy == 2 {
							ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
								ui.TextInput(c, &a.editSubHours).Width(100).Label("订阅更新间隔小时")
								ui.Text(c, "小时").FontSize(12).TextColor(pal.Muted)
							})
						}
						settingsRow(c, "使用 Gopurr 代理更新", "需要内核运行；关闭时直接下载", func() { softSwitch(c, &a.editSub.UseProxy).Label("使用代理更新订阅") })
					} else {
						ui.Text(c, "本地配置只在点击“重新读取”时更新。").FontSize(11).TextColor(pal.Faint)
					}
				})
			})
			if a.editSubError != "" {
				settingsFeedback(c, a.editSubError, true)
			}
			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				softButton(c, "取消").OnClick(func() { a.showEditSub = false })
				softPrimaryButton(c, "保存订阅设置").Disabled(busy).OnClick(a.saveSubscriptionEditor)
			})
		})
	})
}
func subscriptionUpdateLabel(p profile.Profile) string {
	if p.IsFile() {
		return "手动读取"
	}
	if p.UpdateHours == nil {
		return "更新跟随全局"
	}
	if *p.UpdateHours == 0 {
		return "手动更新"
	}
	return fmt.Sprintf("每 %d 小时更新", *p.UpdateHours)
}

// refreshManagedProfile keeps both the cache and running config intact when
// a changed source downloads valid YAML that the kernel cannot accept.
func (a *App) refreshManagedProfile(id string) error {
	active := a.store.ActiveID() == id
	running := active && a.k != nil && a.k.Running()
	var previous []byte
	var err error
	if running {
		previous, err = a.configBytes()
		if err != nil {
			return err
		}
	}
	attemptedReload := false
	err = a.store.RefreshChecked(id, func(body []byte) error {
		if !active {
			return nil
		}
		candidate, err := patch.Apply(body, a.ov)
		if err != nil {
			return err
		}
		if running {
			attemptedReload = true
			return a.k.Start(candidate)
		}
		return nil
	})
	if err != nil && attemptedReload {
		return errors.Join(err, a.k.Start(previous))
	}
	if err == nil && active {
		a.cancelDelayTest()
		a.bmu.Lock()
		a.delays = map[string]uint16{}
		a.delayErrors = map[string]string{}
		a.bmu.Unlock()
	}
	return err
}
