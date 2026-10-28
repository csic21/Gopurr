package app

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"

	"gopurr/internal/core"
)

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
	softSegmented(c, &a.extensionTab, "生效规则", "应用分流", "额外规则", "YAML 覆写").AlignSelf(ui.Start).Label("规则视图")

	// Separate keys keep the two editors' focus, selection and undo state
	// from being reused for the other draft when switching tabs.
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Gap(12).Key(fmt.Sprintf("rules-tab-%d", a.extensionTab)).
		Children(func() {
			switch a.extensionTab {
			case 1:
				a.applicationRulesPage(c)
			case 2:
				a.extraRulesEditor(c)
			case 3:
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

	identity := a.ruleQuery + "\x00" + a.ruleAdapter
	if identity != a.ruleIdentity {
		a.ruleList, a.ruleIdentity = ui.ListState{}, identity
	}
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Radius(12).Background(pal.Surface).Children(func() {
		ui.Row(c).FillWidth().Padding(12, 14).Gap(10).Children(func() {
			ui.Text(c, "顺序").Width(34).FontSize(11).TextColor(pal.Faint)
			ui.Text(c, "匹配条件").Grow(1).MinWidth(0).FontSize(11).TextColor(pal.Faint)
			ui.Text(c, "类型").Width(112).FontSize(11).TextColor(pal.Faint)
			ui.Text(c, "出口").Width(128).FontSize(11).TextColor(pal.Faint)
		})
		hairline(c)
		ui.List(c, &a.ruleList, len(shown)).FillWidth().Grow(1).MinHeight(0).
			ItemKey(func(i int) any { return shown[i].number }).Rows(func(item ui.ListRow) {
			c := item.Context
			row := shown[item.Index]
			r := row.rule
			n := row.number
			line := ui.Row(c).FillWidth().Height(44).Padding(10, 14).Gap(10).AlignItems(ui.Center).
				BorderWidth(0, 0, 1, 0).BorderColor(pal.Grid)
			if line.Hovered() {
				line.Background(pal.Hover)
			}
			line.Children(func() {
				ui.Text(c, fmt.Sprintf("%d", n)).Width(34).
					FontSize(11).FontFeatures("tnum").TextColor(pal.Faint)
				ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
					payload := r.Payload
					if payload == "" {
						payload = "全部流量"
					}
					ui.Text(c, payload).FillWidth().SingleLine().Ellipsis("…").Tooltip(payload).
						FontSize(13).TextColor(pal.Text).Selectable()
				})
				ui.Text(c, r.Type).Width(112).SingleLine().Ellipsis("…").Tooltip(r.Type).FontSize(11).TextColor(pal.Muted)
				ui.Text(c, r.Adapter).Width(128).SingleLine().Ellipsis("…").Tooltip(r.Adapter).FontSize(12).TextColor(adapterColor(c, r.Adapter))
			})
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

	shown := searchLogs(filterLogs(lines, a.logLevelIdx), a.logQuery)

	a.pageHeader(c, "日志", fmt.Sprintf("%d 行 · 内核输出", len(shown)), func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			softButton(c, "复制日志").Disabled(len(shown) == 0).OnClick(func() { a.services().WriteClipboard(logText(shown)) })
			if a.logPaused {
				softPrimaryButton(c, "继续").OnClick(func() {
					a.logPaused = false
					a.logFrozen = nil
				})
			} else {
				softButton(c, "暂停").OnClick(func() {
					a.logPaused = true
					a.logFrozen = live
				})
			}
			softButton(c, "清空").OnClick(func() {
				a.k.ClearLogs()
				a.logPaused = false
				a.logFrozen = nil
			})
		})
	})

	ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
		searchRow(c, &a.logQuery, "搜索日志…").Grow(1).MinWidth(0)
		softSegmented(c, &a.logLevelIdx, logLevelLabels...).Label("日志级别")
	})
	if len(shown) == 0 {
		if a.logQuery != "" || a.logLevelIdx != 0 {
			emptyState(c, "没有匹配的日志", "调整关键词或日志级别后重试。")
		} else {
			emptyState(c, "没有日志", "启动内核并访问网络后，这里会实时刷新。")
		}
		return
	}

	// Stick to the newest line unless the user scrolled away from the end.
	if !a.logPaused && a.logScroll.Y >= a.logScroll.MaxY-18 {
		a.logScroll.Y = math.MaxFloat32
	}

	scrollable := a.logScroll.MaxY > 1
	away := a.logScroll.Y < a.logScroll.MaxY-18

	if a.logPaused || (scrollable && away) {
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
				softButton(c, "回到底部").OnClick(func() {
					a.logScroll.ScrollTo(0, math.MaxFloat32, ui.Smooth)
				})
			}
		})
	}

	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Radius(12).Background(pal.Surface).Children(func() {
		ui.Row(c).FillWidth().Padding(12, 16).Gap(12).Children(func() {
			ui.Text(c, "时间").Width(64).FontSize(11).TextColor(pal.Faint)
			ui.Text(c, "级别").Width(56).FontSize(11).TextColor(pal.Faint)
			ui.Text(c, "消息").FontSize(11).TextColor(pal.Faint)
		})
		hairline(c)
		ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).TrackScroll(&a.logScroll).Children(func() {
			ui.Column(c).FillWidth().Padding(4, 16).Children(func() {
				for _, line := range shown {
					l := line
					ui.Row(c).FillWidth().Gap(12).PaddingY(10).AlignItems(ui.Start).BorderWidth(0, 0, 1, 0).BorderColor(pal.Grid).Children(func() {
						ui.Text(c, l.At.Format("15:04:05")).Width(64).Font("monospace").FontSize(11).TextColor(pal.Faint)
						ui.Text(c, strings.ToUpper(l.Level)).Width(56).Font("monospace").FontSize(11).TextColor(levelColor(c, l.Level))
						ui.Text(c, l.Text).Grow(1).MinWidth(0).Font("monospace").FontSize(12).TextColor(pal.Text).Selectable()
					})
				}
			})
		})
	})
}

func searchLogs(lines []core.LogLine, query string) []core.LogLine {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return lines
	}
	var out []core.LogLine
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line.Text+" "+line.Level), query) {
			out = append(out, line)
		}
	}
	return out
}

func logText(lines []core.LogLine) string {
	var out strings.Builder
	for _, line := range lines {
		fmt.Fprintf(&out, "%s [%s] %s\n", line.At.Format("15:04:05"), strings.ToUpper(line.Level), line.Text)
	}
	return out.String()
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
		return pal.Muted
	}
}
