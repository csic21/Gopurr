package app

import (
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"gopurr/internal/core"
	"gopurr/internal/profile"
)

// ---------- subscriptions ----------

func (a *App) subsPage(c *ui.Context) {
	pal := paletteFor(c)
	busy, errText := a.subState()

	var items []profile.Profile
	activeName := "未选择"
	if a.store != nil {
		items = a.store.List()
		active := a.store.ActiveID()
		for _, p := range items {
			if p.ID == active {
				activeName = p.Name
			}
		}
	}

	running := a.k.Running()
	a.pageHeader(c, "订阅", fmt.Sprintf("%d 个配置 · 当前 %s", len(items), activeName), func() {
		a.kernelButton(c, running)
	})

	ui.Column(c).FillWidth().Padding(16).Radius(12).Background(pal.Surface).
		Border(1, pal.Border).Gap(10).Children(func() {
		sectionLabel(c, "添加配置")
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
				ui.Text(c, "订阅链接").FontSize(11).TextColor(pal.Muted)
				ui.TextInput(c, &a.subURL).FillWidth().Label("订阅链接").
					Placeholder("https://example.com/subscribe")
			})
			ui.Column(c).Width(150).Gap(6).Children(func() {
				ui.Text(c, "备注名 · 可选").FontSize(11).TextColor(pal.Muted)
				ui.TextInput(c, &a.subName).FillWidth().Label("订阅备注名").Placeholder("为配置起个名字")
			})
		})
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			if errText != "" {
				ui.Text(c, errText).FontSize(11).TextColor(pal.Danger)
			} else {
				ui.Text(c, "订阅从链接下载，本地文件直接读取；都不会上传。").
					FontSize(11).TextColor(pal.Faint)
			}
			ui.Spacer(c)
			ui.Button(c, "从文件导入…").Disabled(busy).OnClick(a.importLocalFile)
			label := "添加"
			if busy {
				label = "下载中…"
			}
			ui.PrimaryButton(c, label).Disabled(busy).OnClick(a.addSubscription)
		})
	})

	ui.Scroll(c).FillWidth().FillHeight().Children(func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			if len(items) == 0 {
				emptyState(c, "还没有订阅", "把订阅链接粘贴到上面，点“添加”即可导入。")
				return
			}
			active := ""
			if a.store != nil {
				active = a.store.ActiveID()
			}
			for _, p := range items {
				a.subRow(c, p, p.ID == active, busy)
			}
		})
	})
}

func (a *App) subRow(c *ui.Context, p profile.Profile, active, busy bool) {
	pal := paletteFor(c)
	item := p

	border := pal.Border
	if active {
		border = pal.SelectedBorder
	}
	ui.Column(c).FillWidth().Padding(16).Radius(12).Background(pal.Surface).
		Border(1, border).Gap(12).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
			icon := iconSubs
			if item.IsFile() {
				icon = iconFile
			}
			controlIcon(c, icon)
			ui.Text(c, item.Name).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").Tooltip(item.Name).
				FontSize(13).FontWeight(600).TextColor(pal.Text)
			if item.IsFile() {
				pill(c, "本地", pal.Muted, pal.Muted.Alpha(0.10))
			}
			if active {
				ui.Badge(c, "已选中")
			}
			ui.Text(c, "更新于 "+item.Updated.Format("01-02 15:04")).
				FontSize(11).TextColor(pal.Faint)
		})

		source := subscriptionSource(item.URL)
		if item.IsFile() {
			source = item.Path
		}
		ui.Text(c, source).FillWidth().SingleLine().Ellipsis("…").Tooltip(source).FontSize(11).TextColor(pal.Faint)

		if item.Info.Total > 0 {
			used := float64(item.Info.Used()) / float64(item.Info.Total)
			ui.Progress(c, used).Height(4)
			ui.Text(c, fmt.Sprintf("已用 %s / %s%s", humanBytes(item.Info.Used()),
				humanBytes(item.Info.Total), expiry(item.Info.Expire))).
				FontSize(11).TextColor(pal.Muted)
		}

		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			if !active {
				ui.Button(c, "启用").Disabled(busy).OnClick(func() { a.useSubscription(item.ID) })
			}
			label := "更新"
			if item.IsFile() {
				label = "重新读取"
			}
			if busy {
				label = "…"
			}
			ui.Button(c, label).Disabled(busy).OnClick(func() { a.refreshSubscription(item.ID) })
			ui.Spacer(c)
			ui.Button(c, "删除").Disabled(busy || active).Tooltip("请先启用其他配置，再删除当前订阅").OnClick(func() {
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
	return u.String() + " · 访问凭据已隐藏"
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
	v := float64(n)
	i := -1
	for v >= unit && i < len(units)-1 {
		v /= unit
		i++
	}
	if i < 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

// ---------- rules ----------

type ruleRow struct {
	number int
	rule   core.Rule
}

func filterRules(rules []core.Rule, query, adapter string) []ruleRow {
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]ruleRow, 0, len(rules))

	for i, r := range rules {
		if adapter != "" && adapter != allAdapters && r.Adapter != adapter {
			continue
		}
		if q != "" {
			hay := strings.ToLower(r.Type + " " + r.Payload + " " + r.Adapter)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		out = append(out, ruleRow{number: i + 1, rule: r})
	}
	return out
}

func ruleAdapters(rules []core.Rule) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rules {
		if r.Adapter == "" || seen[r.Adapter] {
			continue
		}
		seen[r.Adapter] = true
		out = append(out, r.Adapter)
	}
	sort.Strings(out)
	return out
}

func (a *App) rulesPage(c *ui.Context) {
	a.initExtensionDrafts()
	var rules []core.Rule
	if a.k != nil {
		rules = a.k.Rules()
	}

	a.pageHeader(c, "规则", fmt.Sprintf("%d 条生效 · 按顺序匹配，命中即停", len(rules)), nil)
	ui.Segmented(c, &a.extensionTab, "生效规则", "额外规则", "YAML 覆写").AlignSelf(ui.Start).Label("规则视图")

	// Separate keys keep the two editors' focus, selection and undo state
	// from being reused for the other draft when switching tabs.
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Gap(12).Key(fmt.Sprintf("rules-tab-%d", a.extensionTab)).
		Children(func() {
			switch a.extensionTab {
			case 1:
				a.extraRulesEditor(c)
			case 2:
				a.mergeYAMLEditor(c)
			default:
				a.effectiveRules(c, rules)
			}
		})
}

func (a *App) effectiveRules(c *ui.Context, rules []core.Rule) {
	pal := paletteFor(c)
	options := append([]string{allAdapters}, ruleAdapters(rules)...)
	if !containsString(options, a.ruleAdapter) {
		a.ruleAdapter = allAdapters
	}

	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		searchRow(c, &a.ruleQuery, "搜索域名、IP 或规则类型…").Grow(1).MinWidth(0)
		ui.Select(c, &a.ruleAdapter, options).Width(170).Label("筛选规则出口")
	})

	if len(rules) == 0 {
		emptyState(c, "暂无生效规则", "内核运行后会显示实际规则；额外规则和 YAML 覆写可先编辑并保存。")
		return
	}

	shown := filterRules(rules, a.ruleQuery, a.ruleAdapter)
	ui.Text(c, fmt.Sprintf("显示 %d / %d 条 · 出口是规则命中后交给的策略", len(shown), len(rules))).
		FontSize(11).TextColor(pal.Muted)
	if len(shown) == 0 {
		emptyState(c, "没有匹配的规则", "换个关键词，或把出口切回“全部出口”。")
		return
	}

	ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Radius(12).Background(pal.Surface).
			Border(1, pal.Border).Children(func() {
			for i, row := range shown {
				if i > 0 {
					hairline(c)
				}
				r := row.rule
				n := row.number
				ui.Row(c).FillWidth().Padding(9, 14).Gap(10).AlignItems(ui.Center).
					Children(func() {
						ui.Text(c, fmt.Sprintf("%d", n)).Width(34).
							FontSize(11).TextColor(pal.Faint)
						ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
							payload := r.Payload
							if payload == "" {
								payload = "全部流量"
							}
							ui.Text(c, payload).FillWidth().SingleLine().Tooltip(payload).
								FontSize(13).FontWeight(500).TextColor(pal.Text)
							ui.Text(c, r.Type).FontSize(11).TextColor(pal.Muted)
						})
						ui.Row(c).Width(150).Justify(ui.End).Children(func() {
							pill(c, r.Adapter, adapterColor(c, r.Adapter), adapterColor(c, r.Adapter).Alpha(0.10)).Tooltip(r.Adapter)
						})
					})
			}
		})
	})
}

func adapterColor(c *ui.Context, adapter string) ui.Color {
	pal := paletteFor(c)
	switch adapter {
	case "DIRECT":
		return pal.Muted
	case "REJECT":
		return pal.Danger
	default:
		return pal.Accent
	}
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// ---------- logs ----------

var logLevelLabels = []string{"全部", "信息", "警告", "错误"}

func levelRank(level string) int {
	switch level {
	case "error":
		return 3
	case "warning":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

func filterLogs(lines []core.LogLine, levelIdx int) []core.LogLine {
	min := 0
	switch levelIdx {
	case 1:
		min = 1
	case 2:
		min = 2
	case 3:
		min = 3
	}
	if min == 0 {
		return lines
	}
	out := make([]core.LogLine, 0, len(lines))
	for _, l := range lines {
		if levelRank(l.Level) >= min {
			out = append(out, l)
		}
	}
	return out
}

func (a *App) logsPage(c *ui.Context) {
	pal := paletteFor(c)
	live := a.k.Logs()

	lines := live
	if a.logPaused {
		if a.logFrozen == nil {
			a.logFrozen = live
		}
		lines = a.logFrozen
	}

	pending := 0
	if a.logPaused {
		pending = len(live) - len(a.logFrozen)
	}

	shown := filterLogs(lines, a.logLevelIdx)

	a.pageHeader(c, "日志", fmt.Sprintf("%d 行 · 内核输出", len(shown)), func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Segmented(c, &a.logLevelIdx, logLevelLabels...)
			if a.logPaused {
				ui.PrimaryButton(c, "继续").OnClick(func() {
					a.logPaused = false
					a.logFrozen = nil
				})
			} else {
				ui.Button(c, "暂停").OnClick(func() {
					a.logPaused = true
					a.logFrozen = live
				})
			}
			ui.Button(c, "清空").OnClick(func() {
				a.k.ClearLogs()
				a.logPaused = false
				a.logFrozen = nil
			})
		})
	})

	if len(shown) == 0 {
		emptyState(c, "没有日志", "启动内核并访问网络后，这里会实时刷新。")
		return
	}

	var max float32 = 200
	for _, l := range shown {
		if f := float32(len(l.Text)); f > max {
			max = f
		}
	}

	// Stick to the newest line unless the user scrolled away from the end.
	if !a.logPaused && a.logScroll.Y >= a.logScroll.MaxY-18 {
		a.logScroll.Y = math.MaxFloat32
	}

	scrollable := a.logScroll.MaxY > 1
	away := a.logScroll.Y < a.logScroll.MaxY-18

	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		if a.logPaused {
			label := "已暂停"
			if pending > 0 {
				label = fmt.Sprintf("已暂停 · 新 %d 行", pending)
			}
			pill(c, label, pal.Warn, pal.Warn.Alpha(0.10))
		}
		ui.Spacer(c)
		if scrollable && away {
			ui.Button(c, "回到底部").OnClick(func() {
				a.logScroll.ScrollTo(0, math.MaxFloat32, ui.Smooth)
			})
		}
	})

	ui.Scroll(c).FillWidth().FillHeight().TrackScroll(&a.logScroll).Children(func() {
		ui.Column(c).FillWidth().Padding(10, 12).Radius(12).Background(pal.Surface).
			Border(1, pal.Border).Gap(1).Children(func() {
			for _, line := range shown {
				l := line
				ui.Row(c).FillWidth().Gap(10).PaddingX(4).PaddingY(2).Children(func() {
					ui.Text(c, l.At.Format("15:04:05")).Width(62).
						FontSize(11).TextColor(pal.Faint)
					ui.Text(c, strings.ToUpper(l.Level)).Width(58).
						FontSize(11).TextColor(levelColor(c, l.Level))
					ui.Text(c, truncate(l.Text, 150)).Grow(1).
						FontSize(11).TextColor(pal.Muted)
				})
			}
		})
	})
}

func levelColor(c *ui.Context, level string) ui.Color {
	pal := paletteFor(c)
	switch level {
	case "error":
		return pal.Danger
	case "warning":
		return pal.Warn
	case "debug":
		return pal.Faint
	default:
		return pal.Accent
	}
}
