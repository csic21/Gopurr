package app

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"go.yaml.in/yaml/v3"

	"gopurr/internal/applications"
	"gopurr/internal/patch"
)

type applicationScanResult struct {
	apps []applications.Application
	err  error
}

type applicationRulesState struct {
	appRulesReady    bool
	appRulesDraft    []patch.AppRule
	appRulesError    string
	appRulesStatus   string
	appRulesQuery    string
	appRulesList     ui.ListState
	appRulesIdentity string

	appCatalog         []applications.Application
	appCatalogReady    bool
	appCatalogBusy     bool
	appCatalogError    string
	appCatalogQuery    string
	appCatalogList     ui.ListState
	appCatalogIdentity string
	appCatalogResult   chan applicationScanResult
	appFileResult      chan applicationScanResult
	showAppPicker      bool
	showAppEditor      bool
	appEdit            patch.AppRule
	appEditKind        int
	appEditGroup       string
	appEditGroups      []string
	appEditError       string
}

func (a *App) initApplicationRules() {
	if !a.appRulesReady {
		a.appRulesReady = true
		a.appRulesDraft = slices.Clone(a.ov.AppRules)
	}
}

func (a *App) saveApplicationRules() error {
	next := a.ov
	next.AppRules = slices.Clone(a.appRulesDraft)
	a.appRulesStatus = ""
	applied, err := a.commitExtensionOverrides(next)
	if err != nil {
		a.appRulesError = err.Error()
		return err
	}
	a.appRulesError = ""
	a.appRulesStatus = extensionSavedStatus(applied)
	return nil
}

func appRuleIndex(routes []patch.AppRule, path string) int {
	for i, route := range routes {
		if strings.EqualFold(route.Application.Path, path) {
			return i
		}
	}
	return -1
}

func filterApplications(apps []applications.Application, query string) []applications.Application {
	query = strings.ToLower(strings.TrimSpace(query))
	var found []applications.Application
	for _, app := range apps {
		if strings.Contains(strings.ToLower(app.Name+" "+app.Path), query) {
			found = append(found, app)
		}
	}
	return found
}

func (a *App) startApplicationScan() {
	if a.appCatalogBusy {
		return
	}
	a.appCatalogBusy, a.appCatalogError = true, ""
	result := make(chan applicationScanResult, 1)
	a.appCatalogResult = result
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		apps, err := applications.Discover(ctx)
		result <- applicationScanResult{apps, err}
		a.invalidate()
	}()
}

// Workers only publish immutable results. All draft and dialog state remains
// owned by the UI goroutine, including when the user switches away mid-scan.
func (a *App) pollApplicationResults() {
	if a.appCatalogResult != nil {
		select {
		case result := <-a.appCatalogResult:
			a.appCatalogResult = nil
			a.appCatalog, a.appCatalogBusy, a.appCatalogReady = result.apps, false, true
			if result.err != nil {
				a.appCatalogError = tr("部分应用未能读取，可重试或手动选择。")
			}
		default:
		}
	}
	if a.appFileResult != nil {
		select {
		case result := <-a.appFileResult:
			a.appFileResult = nil
			if !a.showAppPicker {
				return
			}
			if result.err != nil {
				a.appCatalogError = result.err.Error()
			} else if len(result.apps) > 0 {
				a.openApplicationEditor(result.apps[0])
			}
		default:
		}
	}
}

func (a *App) openApplicationPicker() {
	a.showAppPicker, a.appCatalogQuery = true, ""
	a.appCatalogList = ui.ListState{}
	if !a.appCatalogReady {
		a.startApplicationScan()
	}
}

func (a *App) chooseApplicationFile() {
	if a.appFileResult != nil {
		return
	}
	result := make(chan applicationScanResult, 1)
	a.appFileResult = result
	go func() {
		opts := mygo.OpenDialogOptions{Title: tr("选择应用或 Helper 程序"), ButtonLabel: tr("选择")}
		if runtime.GOOS == "darwin" {
			opts.DefaultPath = "/Applications"
		} else if runtime.GOOS == "windows" {
			opts.Filters = []mygo.FileFilter{{Name: tr("可执行程序"), Extensions: []string{"exe"}}}
		}
		paths, err := mygo.Dialog.Open(opts)
		var apps []applications.Application
		if err == nil && len(paths) > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var app applications.Application
			app, err = applications.Inspect(ctx, paths[0])
			if err == nil {
				apps = append(apps, app)
			}
		}
		result <- applicationScanResult{apps, err}
		a.invalidate()
	}()
}

// Read groups from the composed profile even when the kernel is stopped. Old
// application policies must not prevent choosing a replacement for a deleted
// group, so they are omitted solely for this read.
func (a *App) applicationPolicyGroups() []string {
	raw := []byte(a.cfgText)
	if a.store != nil && a.store.ActiveID() != "" {
		if content, err := a.store.Content(a.store.ActiveID()); err == nil {
			raw = content
		}
	}
	ov := a.ov
	ov.AppRules, ov.ExtraRules = nil, nil
	var groups []string
	if body, err := patch.Apply(raw, ov); err == nil {
		var doc struct {
			Groups []struct{ Name string } `yaml:"proxy-groups"`
		}
		if yaml.Unmarshal(body, &doc) == nil {
			for _, group := range doc.Groups {
				if group.Name != "" && !containsString(groups, group.Name) {
					groups = append(groups, group.Name)
				}
			}
			return groups
		}
	}
	if a.k != nil {
		for _, group := range a.k.Groups() {
			if group.Name != "GLOBAL" && !containsString(groups, group.Name) {
				groups = append(groups, group.Name)
			}
		}
	}
	return groups
}

func (a *App) openApplicationEditor(app applications.Application) {
	a.initApplicationRules()
	a.appEdit = patch.AppRule{Application: app, Target: "DIRECT", IncludeHelpers: app.IsBundle()}
	if index := appRuleIndex(a.appRulesDraft, app.Path); index >= 0 {
		a.appEdit = a.appRulesDraft[index]
		// A fresh selection can refresh a changed main executable's metadata.
		a.appEdit.Application = app
	}
	a.appEditGroups = a.applicationPolicyGroups()
	a.appEditKind, a.appEditGroup, a.appEditError = 0, "", ""
	if len(a.appEditGroups) > 0 {
		a.appEditGroup = a.appEditGroups[0]
	}
	switch a.appEdit.Target {
	case "DIRECT":
	case "REJECT":
		a.appEditKind = 1
	default:
		a.appEditKind, a.appEditGroup = 2, a.appEdit.Target
	}
	a.showAppPicker, a.showAppEditor = false, true
}

func (a *App) stageApplicationRule() {
	route := a.appEdit
	switch a.appEditKind {
	case 0:
		route.Target = "DIRECT"
	case 1:
		route.Target = "REJECT"
	case 2:
		if !containsString(a.appEditGroups, a.appEditGroup) {
			a.appEditError = tr("请从当前配置中选择可用的代理组")
			return
		}
		route.Target = a.appEditGroup
	}
	if _, err := patch.AppRuleText(route); err != nil {
		a.appEditError = err.Error()
		return
	}
	if index := appRuleIndex(a.appRulesDraft, route.Application.Path); index >= 0 {
		a.appRulesDraft[index] = route
	} else {
		a.appRulesDraft = append(a.appRulesDraft, route)
	}
	a.appRulesError, a.appRulesStatus = "", ""
	a.showAppEditor = false
}

func appPolicyLabel(target string) string {
	switch target {
	case "DIRECT":
		return tr("直连")
	case "REJECT":
		return tr("拦截")
	default:
		return target
	}
}

func (a *App) applicationRulesPage(c *ui.Context) {
	a.initApplicationRules()
	a.pollApplicationResults()
	pal := paletteFor(c)
	ui.Text(c, tr("应用分流优先于额外规则与订阅规则，订阅更新后仍会保留。")).FillWidth().FontSize(11.5).TextColor(pal.Muted)
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		searchRow(c, &a.appRulesQuery, tr("搜索已添加的应用…")).Grow(1).MinWidth(0).Label(tr("搜索应用分流"))
		softButton(c, tr("添加应用…")).OnClick(a.openApplicationPicker)
	})
	var shown []int
	query := strings.ToLower(strings.TrimSpace(a.appRulesQuery))
	if a.appRulesIdentity != query {
		a.appRulesIdentity, a.appRulesList = query, ui.ListState{}
	}
	for i, route := range a.appRulesDraft {
		if strings.Contains(strings.ToLower(route.Application.Name+" "+route.Application.Path+" "+appPolicyLabel(route.Target)), query) {
			shown = append(shown, i)
		}
	}
	if len(shown) == 0 {
		ui.Column(c).FillWidth().Grow(1).MinHeight(0).Justify(ui.Center).Children(func() {
			if len(a.appRulesDraft) == 0 {
				emptyState(c, tr("为应用选择专属出口"), tr("添加本机应用，选择直连、拦截或代理组。"))
			} else {
				emptyState(c, tr("没有匹配的应用"), tr("试试应用名称、路径或策略名称。"))
			}
		})
	} else {
		ui.List(c, &a.appRulesList, len(shown)).FillWidth().Grow(1).MinHeight(0).Radius(12).Background(pal.Surface).
			ItemKey(func(i int) any { return a.appRulesDraft[shown[i]].Application.Path }).Rows(func(row ui.ListRow) {
			c := row.Context
			index := shown[row.Index]
			route := a.appRulesDraft[index]
			line := ui.Row(c).FillWidth().Height(76).Padding(12, 14).Gap(12).AlignItems(ui.Center).
				BorderWidth(0, 0, 1, 0).BorderColor(pal.Grid).Transition(colorMotion())
			if line.Hovered() {
				line.Background(pal.Hover)
			}
			line.Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
					ui.Text(c, route.Application.Name).FillWidth().SingleLine().Ellipsis("…").Tooltip(route.Application.Name).FontSize(13).TextColor(pal.Text)
					ui.Text(c, route.Application.Path).FillWidth().SingleLine().Ellipsis("…").Tooltip(route.Application.Path).FontSize(11).TextColor(pal.Muted)
				})
				ui.Column(c).Width(132).Gap(5).Children(func() {
					ui.Text(c, appPolicyLabel(route.Target)).FillWidth().SingleLine().Ellipsis("…").Tooltip(route.Target).FontSize(12).TextColor(adapterColor(c, route.Target))
					scope := tr("仅主程序")
					if route.IncludeHelpers {
						scope = tr("含包内 Helper")
					}
					ui.Text(c, scope).FontSize(11).TextColor(pal.Faint)
				})
				softButton(c, tr("编辑")).Label(tr("编辑应用分流 ") + route.Application.Name).OnClick(func() { a.openApplicationEditor(route.Application) })
				softButton(c, tr("移除")).Label(tr("移除应用分流 ") + route.Application.Name).OnClick(func() {
					a.appRulesDraft = append(a.appRulesDraft[:index:index], a.appRulesDraft[index+1:]...)
					a.appRulesError, a.appRulesStatus = "", ""
				})
			})
		})
	}
	modeHint := tr("在规则模式下生效；不遵循系统代理的应用可通过 TUN 接管。")
	color := pal.Muted
	if a.ov.Mode != "" && a.ov.Mode != "rule" {
		modeHint, color = tr("当前为")+labelFor(a.ov.Mode)+tr("模式，切回规则模式后应用分流才会生效。"), pal.Warn
	}
	ui.Text(c, modeHint).FillWidth().FontSize(11).TextColor(color)
	extensionFeedback(c, a.appRulesError, a.appRulesStatus)
	extensionSaveBar(c, !slices.Equal(a.appRulesDraft, a.ov.AppRules), tr("保存应用分流"), func() {
		a.appRulesDraft = slices.Clone(a.ov.AppRules)
		a.appRulesError, a.appRulesStatus = "", ""
	}, func() { _ = a.saveApplicationRules() })
	a.applicationPicker(c)
	a.applicationRuleEditor(c)
}

func (a *App) applicationPicker(c *ui.Context) {
	pal := paletteFor(c)
	softModal(c, "application-picker", &a.showAppPicker, func() {
		_, height := c.Size()
		listHeight := float32(280)
		if height < 660 {
			listHeight = 190
		}
		ui.Column(c).Width(590).Padding(20).Gap(12).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, tr("选择本机应用")).Grow(1).FontSize(16).Bold().TextColor(pal.Text)
				ui.Text(c, fmt.Sprintf(tr("%d 个应用"), len(a.appCatalog))).FontSize(11).TextColor(pal.Muted)
			})
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				searchRow(c, &a.appCatalogQuery, tr("搜索应用名称或路径…")).Grow(1).MinWidth(0).Label(tr("搜索本机应用"))
				softButton(c, tr("重新扫描")).Disabled(a.appCatalogBusy).OnClick(a.startApplicationScan)
			})
			shown := filterApplications(a.appCatalog, a.appCatalogQuery)
			if a.appCatalogIdentity != a.appCatalogQuery {
				a.appCatalogIdentity, a.appCatalogList = a.appCatalogQuery, ui.ListState{}
			}
			ui.Column(c).FillWidth().Height(listHeight).Radius(10).Background(pal.Surface).Children(func() {
				if len(shown) == 0 {
					ui.Column(c).Fill().Justify(ui.Center).Children(func() {
						title, hint := tr("没有找到应用"), tr("可更换搜索词，或从其他位置选择应用。")
						if a.appCatalogBusy {
							title, hint = tr("正在读取本机应用…"), tr("仅读取应用信息，不会启动应用。")
						}
						emptyState(c, title, hint)
					})
					return
				}
				ui.List(c, &a.appCatalogList, len(shown)).Fill().ItemKey(func(i int) any { return shown[i].Path }).Rows(func(row ui.ListRow) {
					c, app := row.Context, shown[row.Index]
					selected := appRuleIndex(a.appRulesDraft, app.Path) >= 0
					line := ui.Row(c).FillWidth().Height(64).Padding(10, 12).Gap(12).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(pal.Grid)
					if selected {
						line.Background(pal.Selected)
					} else if line.Hovered() {
						line.Background(pal.Hover)
					}
					line.Children(func() {
						ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
							ui.Text(c, app.Name).FillWidth().SingleLine().Ellipsis("…").Tooltip(app.Name).FontSize(13).TextColor(pal.Text)
							ui.Text(c, app.Path).FillWidth().SingleLine().Ellipsis("…").Tooltip(app.Path).FontSize(11).TextColor(pal.Muted)
						})
						if selected {
							ui.Text(c, tr("已添加")).FontSize(11).TextColor(pal.Accent)
						}
						softButton(c, tr("选择")).Label(tr("选择应用 ") + app.Name).OnClick(func() { a.openApplicationEditor(app) })
					})
				})
			})
			if a.appCatalogError != "" {
				ui.Text(c, a.appCatalogError).FillWidth().MaxHeight(36).FontSize(11).TextColor(pal.Warn).Tooltip(a.appCatalogError)
			}
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				softButton(c, tr("选择文件…")).Disabled(a.appFileResult != nil).OnClick(a.chooseApplicationFile)
				ui.Spacer(c)
				softButton(c, tr("取消")).OnClick(func() { a.showAppPicker = false })
			})
		})
	})
}

func (a *App) applicationRuleEditor(c *ui.Context) {
	pal := paletteFor(c)
	softModal(c, "application-editor", &a.showAppEditor, func() {
		ui.Column(c).Width(560).Padding(20).Gap(12).Children(func() {
			ui.Text(c, tr("设置应用分流")).FontSize(16).Bold().TextColor(pal.Text)
			ui.Column(c).FillWidth().Gap(4).Children(func() {
				ui.Text(c, a.appEdit.Application.Name).FillWidth().SingleLine().Ellipsis("…").Tooltip(a.appEdit.Application.Name).FontSize(14).TextColor(pal.Text)
				ui.Text(c, a.appEdit.Application.Path).FillWidth().SingleLine().Ellipsis("…").Tooltip(a.appEdit.Application.Path).FontSize(11).TextColor(pal.Muted)
			})
			ui.Text(c, tr("连接方式")).FontSize(11.5).TextColor(pal.Muted)
			softSegmented(c, &a.appEditKind, tr("直连"), tr("拦截"), tr("代理组")).AlignSelf(ui.Start).Label(tr("应用连接方式"))
			validGroup := containsString(a.appEditGroups, a.appEditGroup)
			if a.appEditKind == 2 {
				options := slices.Clone(a.appEditGroups)
				if a.appEditGroup != "" && !validGroup {
					options = append(options, a.appEditGroup)
				}
				if len(options) > 0 {
					ui.Select(c, &a.appEditGroup, options).FillWidth().Label(tr("应用代理组"))
				}
				if !validGroup {
					ui.Text(c, tr("当前配置中没有可用的所选代理组，请更换或先添加订阅。")).FillWidth().FontSize(11).TextColor(pal.Warn)
				}
			}
			ui.Checkbox(c, &a.appEdit.IncludeHelpers, tr("包含应用包内的 Helper 子进程")).Disabled(!a.appEdit.Application.IsBundle()).Label(tr("包含应用 Helper"))
			hint := tr("按应用包路径匹配，覆盖包内 Helper、XPC 服务与更新程序。应用包外的共享进程需单独添加。")
			if !a.appEdit.Application.IsBundle() {
				hint = tr("此项是独立可执行程序，仅匹配该程序。Helper 可通过「选择文件…」单独添加，避免影响同目录的其他应用。")
			}
			ui.Text(c, hint).FillWidth().FontSize(11.5).TextColor(pal.Muted)
			if a.appEditError != "" {
				extensionError(c, a.appEditError)
			}
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, tr("加入草稿后，保存才会生效")).Grow(1).FontSize(11).TextColor(pal.Faint)
				softButton(c, tr("取消")).OnClick(func() { a.showAppEditor = false })
				label := tr("添加到草稿")
				if appRuleIndex(a.appRulesDraft, a.appEdit.Application.Path) >= 0 {
					label = tr("更新草稿")
				}
				softPrimaryButton(c, label).Disabled(a.appEditKind == 2 && !validGroup).OnClick(a.stageApplicationRule)
			})
		})
	})
}
