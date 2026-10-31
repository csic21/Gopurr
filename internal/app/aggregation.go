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

type aggregationSourceDraft struct {
	ID, Name, Prefix string
	Selected         bool
}

type aggregationState struct {
	showAggregation                        bool
	aggregationID                          string
	aggregationName                        string
	aggregationSources                     []aggregationSourceDraft
	aggregationInclude, aggregationExclude string
	aggregationDedupe                      bool
	aggregationTab                         int
	aggregationError                       string
	aggregationPreview                     string
	aggregationStats                       profile.AggregateStats
}

func (a *App) openAggregationEditor(id string) {
	a.aggregationID, a.aggregationName, a.aggregationTab = id, "", 0
	a.aggregationInclude, a.aggregationExclude, a.aggregationError, a.aggregationPreview = "", "", "", ""
	a.aggregationDedupe, a.aggregationStats = true, profile.AggregateStats{}
	a.aggregationSources = nil
	if a.store == nil {
		a.notice = tr("订阅目录不可用")
		return
	}
	items := a.store.List()
	var selected []profile.AggregateSource
	if id != "" {
		p, err := a.store.Get(id)
		if err != nil || !p.IsAggregate() || p.Aggregate == nil {
			a.notice = tr("聚合订阅不存在，请重新选择")
			return
		}
		a.aggregationName, selected = p.Name, p.Aggregate.Sources
		a.aggregationInclude, a.aggregationExclude, a.aggregationDedupe = p.Aggregate.Include, p.Aggregate.Exclude, p.Aggregate.Dedupe
	}
	seen := map[string]bool{}
	for _, source := range selected {
		name := source.ID
		for _, p := range items {
			if p.ID == source.ID {
				name = p.Name
				break
			}
		}
		a.aggregationSources = append(a.aggregationSources, aggregationSourceDraft{ID: source.ID, Name: name, Prefix: source.Prefix, Selected: true})
		seen[source.ID] = true
	}
	for _, p := range items {
		if !p.IsAggregate() && !seen[p.ID] {
			a.aggregationSources = append(a.aggregationSources, aggregationSourceDraft{ID: p.ID, Name: p.Name})
		}
	}
	a.showAggregation = true
}

func (a *App) aggregationSpec() profile.AggregateSpec {
	spec := profile.AggregateSpec{Include: strings.TrimSpace(a.aggregationInclude), Exclude: strings.TrimSpace(a.aggregationExclude), Dedupe: a.aggregationDedupe}
	for _, source := range a.aggregationSources {
		if source.Selected {
			spec.Sources = append(spec.Sources, profile.AggregateSource{ID: source.ID, Prefix: source.Prefix})
		}
	}
	return spec
}

func (a *App) previewAggregation() {
	_, stats, err := a.store.BuildAggregate(a.aggregationSpec())
	a.aggregationPreview, a.aggregationStats = "", profile.AggregateStats{}
	if err != nil {
		a.aggregationError = err.Error()
		return
	}
	a.aggregationError, a.aggregationStats = "", stats
	a.aggregationPreview = strings.Join(stats.Names, "\n")
}

func (a *App) saveAggregation() {
	if a.store == nil || !a.beginProfileWork() {
		a.aggregationError = tr("订阅正在更新，请稍后重试")
		return
	}
	defer a.endProfileWork()
	active := a.aggregationID != "" && a.store.ActiveID() == a.aggregationID
	running := active && a.k != nil && a.k.Running()
	var previous []byte
	var err error
	if running {
		previous, err = a.configBytes()
		if err != nil {
			a.aggregationError = err.Error()
			return
		}
	}
	attempted := false
	_, stats, err := a.store.SaveAggregate(a.aggregationID, a.aggregationName, a.aggregationSpec(), func(raw []byte) error {
		candidate, err := patch.ApplyLayers(raw, a.runtimeOverrides(), a.profileOverrides(a.aggregationID))
		if err != nil {
			return err
		}
		if running {
			attempted = true
			return a.k.Start(candidate)
		}
		return nil
	})
	if err != nil {
		if attempted {
			err = errors.Join(err, a.k.Start(previous))
		}
		a.aggregationError = err.Error()
		return
	}
	a.aggregationStats, a.aggregationError, a.showAggregation = stats, "", false
	a.featureCacheAt = time.Time{}
	if active {
		a.cancelDelayTest()
		a.proxyGroup = ""
	}
	a.notice = fmt.Sprintf(tr("聚合已保存，共 %d 个节点；来源配置保持不变"), stats.Output)
	a.invalidate()
}

func (a *App) moveAggregationSource(index, delta int) {
	target := index + delta
	if index < 0 || target < 0 || index >= len(a.aggregationSources) || target >= len(a.aggregationSources) {
		return
	}
	a.aggregationSources[index], a.aggregationSources[target] = a.aggregationSources[target], a.aggregationSources[index]
	a.aggregationPreview = ""
}

func (a *App) aggregationDialog(c *ui.Context) {
	if !a.showAggregation {
		return
	}
	pal := paletteFor(c)
	width, height := c.Size()
	busy, _ := a.subState()
	ui.Overlay(c, func() {
		back := ui.Box(c.Key("aggregation-editor")).Absolute().Left(0).Top(0).Right(0).Bottom(0).Center().Modal()
		modalBackdrop(back)
		if !busy && back.OverlayShortcut(0, ui.KeyEscape) {
			a.showAggregation = false
		}
		back.Children(func() {
			panel := ui.Column(c).Width(min(float32(680), width-64)).MaxHeight(height-64).Padding(20).Gap(12).Radius(14).Background(pal.Surface).Border(1, pal.Border).Role(ui.RoleDialog).Label(tr("聚合订阅"))
			modalPanel(c, panel)
			panel.Children(func() {
				ui.Text(c, tr("聚合订阅")).FontSize(18).FontWeight(600).TextColor(pal.Text)
				ui.Text(c, tr("合并已导入配置中的节点，生成独立配置。保留原来源，不继承其规则、DNS 或策略组。")).FillWidth().FontSize(11.5).TextColor(pal.Muted)
				ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Text(c, tr("配置名称")).FontSize(12).TextColor(pal.Muted)
					ui.TextInput(c, &a.aggregationName).Grow(1).MinWidth(0).Label(tr("聚合配置名称")).Placeholder(tr("为聚合配置起个名字")).Disabled(busy)
				})
				softSegmented(c, &a.aggregationTab, tr("来源与顺序"), tr("筛选与去重"), tr("结果预览")).AlignSelf(ui.Start).Label(tr("聚合编辑分类")).Disabled(busy)
				ui.Scroll(c).FillWidth().Shrink(1).MinHeight(0).Children(func() {
					ui.Column(c).FillWidth().Gap(12).Children(func() {
						if a.aggregationTab == 0 {
							a.aggregationSourceFields(c, busy)
						}
						if a.aggregationTab == 1 {
							ui.Text(c, tr("仅包含 · 正则表达式，可留空")).FontSize(12).TextColor(pal.Muted)
							ui.TextInput(c, &a.aggregationInclude).FillWidth().Label(tr("聚合包含筛选")).Placeholder(tr("例如：香港|日本")).Disabled(busy).OnChange(func() { a.aggregationPreview = "" })
							ui.Text(c, tr("排除 · 正则表达式，可留空")).FontSize(12).TextColor(pal.Muted)
							ui.TextInput(c, &a.aggregationExclude).FillWidth().Label(tr("聚合排除筛选")).Placeholder(tr("例如：到期|剩余流量")).Disabled(busy).OnChange(func() { a.aggregationPreview = "" })
							ui.Checkbox(c, &a.aggregationDedupe, tr("合并连接参数相同的重复节点")).Label(tr("聚合节点去重")).Disabled(busy).OnChange(func() { a.aggregationPreview = "" })
							ui.Text(c, tr("筛选匹配来源中的原始节点名。去重保留顺序靠前的节点与名称；链式代理节点保持独立。")).FillWidth().FontSize(11).TextColor(pal.Muted)
						}
						if a.aggregationTab == 2 {
							if a.aggregationPreview == "" {
								ui.Text(c, tr("点击“预览聚合”检查筛选结果与最终节点顺序。")).FillWidth().FontSize(12).TextColor(pal.Muted)
							} else {
								ui.Text(c, fmt.Sprintf(tr("原始 %d 个 · 保留 %d 个 · 去重 %d 个"), a.aggregationStats.Input, a.aggregationStats.Output, a.aggregationStats.Duplicates)).FillWidth().FontSize(12).TextColor(pal.Text)
								ui.Text(c, a.aggregationPreview).FillWidth().FontSize(12).TextColor(pal.Muted).Selectable()
							}
						}
						ui.Text(c, tr("仅支持直接列出 proxies 的配置，代理集合暂不支持。先更新来源订阅，聚合会随来源更新；“重新聚合”只读取本地缓存。")).FillWidth().FontSize(11).TextColor(pal.Faint)
						if a.aggregationError != "" {
							settingsFeedback(c, a.aggregationError, true)
						}
					})
				})
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					softButton(c, tr("预览聚合")).Disabled(busy).OnClick(func() { a.previewAggregation(); a.aggregationTab = 2 })
					ui.Spacer(c)
					softButton(c, tr("取消")).Disabled(busy).OnClick(func() { a.showAggregation = false })
					softPrimaryButton(c, tr("保存聚合")).Disabled(busy).OnClick(a.saveAggregation)
				})
			})
		})
	})
}

func (a *App) aggregationSourceFields(c *ui.Context, busy bool) {
	pal := paletteFor(c)
	if len(a.aggregationSources) == 0 {
		ui.Text(c, tr("请先导入至少一个包含节点的订阅或本地配置。")).FillWidth().FontSize(12).TextColor(pal.Muted)
		return
	}
	for index := range a.aggregationSources {
		source := &a.aggregationSources[index]
		ui.Column(c).FillWidth().Gap(6).PaddingY(4).Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Checkbox(c, &source.Selected, "").Label(tr("选择聚合来源 ") + source.Name).Disabled(busy).OnChange(func() { a.aggregationPreview = "" })
				ui.Text(c, source.Name).Grow(1).MinWidth(0).MaxLines(1).Ellipsis("…").Tooltip(source.Name).FontSize(13).TextColor(pal.Text)
				softButton(c, tr("上移")).Label(tr("上移聚合来源 ") + source.Name).Disabled(busy || index == 0).OnClick(func() { a.moveAggregationSource(index, -1) })
				softButton(c, tr("下移")).Label(tr("下移聚合来源 ") + source.Name).Disabled(busy || index == len(a.aggregationSources)-1).OnClick(func() { a.moveAggregationSource(index, 1) })
			})
			if source.Selected {
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, tr("节点名前缀")).FontSize(11).TextColor(pal.Muted)
					ui.TextInput(c, &source.Prefix).Grow(1).MinWidth(0).Label(tr("来源前缀 ") + source.Name).Placeholder(tr("可留空，例如：工作 | ")).Disabled(busy).OnChange(func() { a.aggregationPreview = "" })
				})
			}
		})
		if index < len(a.aggregationSources)-1 {
			hairline(c)
		}
	}
}
