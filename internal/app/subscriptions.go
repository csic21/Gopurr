package app

import (
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/profile"
)

func (a *App) subsPage(c *ui.Context) {
	pal := paletteFor(c)
	busy, errText := a.subState()
	if a.subAddPending && !busy {
		a.subAddPending = false
		if errText == "" {
			a.showAddSub = false
		}
	}
	var items []profile.Profile
	active := ""
	if a.store != nil {
		items, active = a.store.List(), a.store.ActiveID()
	}
	if len(items) == 0 {
		a.showAddSub = false
	}
	running := a.k.Running()
	a.pageHeader(c, "订阅", fmt.Sprintf("%d 个配置 · 管理来源与更新", len(items)), func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if len(items) > 0 {
				softButton(c, "从文件导入…").Disabled(busy).OnClick(a.importLocalFile)
				add := ui.Button
				if running {
					add = ui.PrimaryButton
				}
				add(c, "添加订阅").Disabled(busy).OnClick(func() {
					a.showAddSub, a.subAddPending = true, false
					a.setSubErr("")
				})
			}
			if !running {
				a.kernelButton(c, false)
			}
		})
	})
	if errText != "" && len(items) > 0 && !a.showAddSub {
		settingsFeedback(c, errText, true)
	}
	ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(20).Children(func() {
			if len(items) == 0 {
				card(c).Gap(16).Children(func() {
					sectionHeading(c, "添加第一个配置", "订阅链接或本地文件")
					a.subscriptionAddForm(c, busy, errText, false)
				})
				return
			}
			for _, p := range items {
				if p.ID == active {
					ui.Column(c).FillWidth().Gap(10).Children(func() {
						sectionHeading(c, "当前配置", "更新后自动应用")
						a.subRow(c, p, true, busy)
					})
					break
				}
			}
			other := 0
			for _, p := range items {
				if p.ID != active {
					other++
				}
			}
			if other > 0 {
				ui.Column(c).FillWidth().Gap(10).Children(func() {
					sectionHeading(c, "其他配置", fmt.Sprintf("%d 个可切换", other))
					for _, p := range items {
						if p.ID != active {
							a.subRow(c, p, false, busy)
						}
					}
				})
			}
			ui.Text(c, "订阅更新保留额外规则与 YAML 覆写。").FontSize(11).TextColor(pal.Faint)
		})
	})
	softModal(c, "subscription-add", &a.showAddSub, func() {
		ui.Column(c).Width(520).Padding(20).Gap(16).Children(func() {
			ui.Text(c, "添加订阅").FontSize(16).FontWeight(600).TextColor(pal.Text)
			a.subscriptionAddForm(c, busy, errText, true)
		})
	})
	a.subscriptionEditor(c)
}

func (a *App) subscriptionAddForm(c *ui.Context, busy bool, errText string, modal bool) {
	pal := paletteFor(c)
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Text(c, "订阅链接").FontSize(12).TextColor(pal.Muted)
		ui.TextInput(c, &a.subURL).FillWidth().Label("订阅链接").Placeholder("https://example.com/subscribe").Disabled(busy)
	})
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Text(c, "备注名 · 可选").FontSize(12).TextColor(pal.Muted)
		ui.TextInput(c, &a.subName).FillWidth().Label("订阅备注名").Placeholder("为配置起个名字").Disabled(busy)
	})
	if errText != "" {
		settingsFeedback(c, errText, true)
	} else {
		ui.Text(c, "订阅从链接下载；本地文件直接读取，不会上传。").FillWidth().FontSize(11).TextColor(pal.Faint)
	}
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		softButton(c, "从文件导入…").Disabled(busy).OnClick(func() {
			a.showAddSub, a.subAddPending = false, false
			a.importLocalFile()
		})
		ui.Spacer(c)
		if modal {
			softButton(c, "取消").Disabled(busy).OnClick(func() { a.showAddSub, a.subAddPending = false, false })
		}
		label := "添加"
		if busy {
			label = "下载中…"
		}
		softPrimaryButton(c, label).Disabled(busy).OnClick(func() {
			a.subAddPending = true
			a.addSubscription()
		})
	})
}

func (a *App) subRow(c *ui.Context, item profile.Profile, active, busy bool) {
	pal := paletteFor(c)
	row := ui.Column(c).FillWidth().Padding(16).Radius(12).Background(pal.Surface).Gap(12)
	if active {
		row.BorderWidth(0, 0, 0, 3).BorderColor(pal.Accent)
	}
	row.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
			icon := iconSubs
			if item.IsFile() {
				icon = iconFile
			}
			controlIcon(c, icon)
			ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
				ui.Text(c, item.Name).FillWidth().SingleLine().Ellipsis("…").Tooltip(item.Name).
					FontSize(15).FontWeight(600).TextColor(pal.Text)
				source, full := subscriptionSource(item.URL), subscriptionSource(item.URL)
				if item.IsFile() {
					full = item.Path
					source = "本地文件 · " + filepath.Base(item.Path)
					if item.Path == "" {
						source, full = "备份中的本地配置", "备份中的本地配置"
					}
				}
				ui.Text(c, source).FillWidth().SingleLine().Ellipsis("…").Tooltip(full).FontSize(11).TextColor(pal.Muted)
			})
			if active {
				ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconCheck).Size(15, 15).TextColor(pal.Accent).Role(ui.RoleNone)
					ui.Text(c, "使用中").FontSize(11).TextColor(pal.Accent)
				})
			} else {
				softButton(c, "启用").Disabled(busy).OnClick(func() { a.useSubscription(item.ID) })
			}
		})
		if item.Info.Total > 0 {
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				ui.Progress(c, float64(item.Info.Used())/float64(item.Info.Total)).FillWidth().Height(4)
				ui.Text(c, fmt.Sprintf("已用 %s / %s%s", humanBytes(item.Info.Used()), humanBytes(item.Info.Total), expiry(item.Info.Expire))).
					FontSize(11).TextColor(pal.Muted)
			})
		}
		hairline(c)
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				updated := "尚未更新"
				if !item.Updated.IsZero() {
					updated = "更新于 " + item.Updated.Format("01-02 15:04")
				}
				ui.Text(c, updated).FontSize(11).TextColor(pal.Muted)
				ui.Text(c, subscriptionUpdateLabel(item)).FontSize(11).TextColor(pal.Faint)
			})
			label := "更新"
			if item.IsFile() {
				label = "重新读取"
			}
			if busy {
				label = "更新中…"
			}
			softButton(c, label).Disabled(busy).OnClick(func() { a.refreshSubscription(item.ID) })
			softButton(c, "编辑…").Disabled(busy).Label("编辑订阅 " + item.Name).OnClick(func() { a.openSubscriptionEditor(item) })
			deleteHint := "删除此配置"
			if active {
				deleteHint = "请先启用其他配置，再删除当前订阅"
			}
			softButton(c, "删除").Disabled(busy || active).Tooltip(deleteHint).OnClick(func() {
				a.deleteSubID, a.deleteSubName, a.showDeleteSub = item.ID, item.Name, true
			})
		})
	})
}

func subscriptionSource(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "订阅链接"
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	u.Path, u.RawPath = "", ""
	return u.String()
}

func expiry(unix int64) string {
	if unix <= 0 {
		return ""
	}
	return " · 到期 " + time.Unix(unix, 0).Format("2006-01-02")
}

func humanBytes(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	v, i := float64(n), -1
	for v >= unit && i < len(units)-1 {
		v /= unit
		i++
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}
