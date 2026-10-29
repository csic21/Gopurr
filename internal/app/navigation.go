package app

import "github.com/egoist/mygo/ui"

func (a *App) toggleSidebar() {
	if err := a.changePrefs(func(p *Prefs) { p.SidebarCollapsed = !p.SidebarCollapsed }); err != nil {
		a.notice = err.Error()
	}
}

func (a *App) windowChrome(c *ui.Context) {
	pal := paletteFor(c)
	c.OnShortcut(ui.Cmd, ui.KeyBackslash, a.toggleSidebar)
	collapsed := a.Prefs().SidebarCollapsed
	shell := ui.Column(c).Fill().Background(pal.Bg)
	target := float32(0)
	if collapsed {
		target = 1
	}
	progress := shell.AnimateTo("navigation-layout", target, navigationMorph)
	width, moving := sidebarWidth*(1-progress), progress != target
	shell.Children(func() {
		a.navigationTitleBar(c.Key("titlebar"), collapsed, progress, moving)
		ui.Row(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c.Key("sidebar-clip")).Width(width).Shrink(0).FillHeight().Clip().
				Disabled(collapsed || moving).Children(func() {
				if width > 0 {
					a.sidebar(c, progress, moving)
				}
			})
			a.main(c.Key("workspace"))
		})
		if moving {
			a.navigationFlight(c.Key("navigation-flight"), progress)
		}
	})
}

func (a *App) navigationTitleBar(c *ui.Context, collapsed bool, progress float32, moving bool) {
	pal, bar := paletteFor(c), c.TitleBar()
	height := max(bar.Height, 38)
	// The toggle stays in one place in both layouts. Native window controls
	// keep their full reserved area; blank toolbar space still drags the window.
	leading := navigationLeading(c)
	sidebar := sidebarWidth * (1 - progress)
	ui.Row(c).FillWidth().Height(height).Shrink(0).AlignItems(ui.Center).
		DragWindow().Label("窗口标题栏").Children(func() {
		if sidebar > 0 {
			ui.Box(c).Absolute().Left(0).Top(0).Bottom(0).Width(sidebar).
				Background(pal.Side).BorderWidth(0, 1, 0, 0).BorderColor(pal.Border)
		}
		brand := ui.Row(c.Key("toolbar-brand")).Width(leading).Shrink(0).Padding(0, 8, 0, bar.Left+8).
			AlignItems(ui.Center).Gap(6)
		if moving {
			brand.Opacity(0)
		}
		brand.Children(func() {
			if collapsed {
				ui.Image(c, brandLogo).Size(24, 24).Shrink(0).Label("Gopurr 蜷睡猫标记")
				ui.Text(c, "Gopurr").FontSize(13).FontWeight(600).SingleLine().TextColor(pal.Text)
			}
		})
		ui.Row(c).PaddingX(8).AlignItems(ui.Center).Children(func() {
			label := "收起侧栏"
			if collapsed {
				label = "展开侧栏"
			}
			iconButton(c, iconSidebar, label).OnClick(a.toggleSidebar)
		})
		ui.Row(c.Key("navigation-center")).Grow(1).MinWidth(0).FillHeight().AlignItems(ui.Center).Justify(ui.Center).
			Children(func() {
				if collapsed {
					nav := a.topNavigation(c.Key("top-navigation")).Disabled(moving)
					if moving {
						nav.Opacity(0)
					}
				}
				if collapsed && !moving {
					return
				}
				ui.Row(c.Key("breadcrumb")).Absolute().Left(0).Top(0).Bottom(0).AlignItems(ui.Center).Gap(8).
					Opacity(1 - progress).Children(func() {
					ui.Text(c, "工作区").FontSize(11.5).TextColor(pal.Faint)
					ui.Icon(c, iconChevron).Size(12, 12).TextColor(pal.Faint).Role(ui.RoleNone)
					for _, it := range navItems {
						if it.id == a.page {
							ui.Text(c, it.label).FontSize(11.5).FontWeight(600).TextColor(pal.Muted).Role(ui.RoleNone)
						}
					}
				})
			})
		ui.Row(c).Shrink(0).Padding(0, bar.Right+16, 0, 16).Gap(6).AlignItems(ui.Center).
			Opacity(progress).Children(func() {
			if collapsed || moving {
				label, dot := a.navigationStatus(), pal.Faint
				if a.k.Running() {
					dot = pal.Success
				}
				ui.Box(c).Size(5, 5).Radius(3).Background(dot)
				ui.Text(c, label).FontSize(11).TextColor(pal.Muted).SingleLine()
			}
		})
	})
}

func (a *App) topNavigation(c *ui.Context) ui.Element {
	selected := 0
	for i, it := range navItems {
		if it.id == a.page {
			selected = i
		}
	}
	return softTopNavigation(c, &selected).Label("页面导航").OnChange(func() {
		a.page = navItems[selected].id
	})
}

func navigationLeading(c *ui.Context) float32 {
	return max(float32(sidebarWidth), c.TitleBar().Left+96)
}

func topNavigationWidths(c *ui.Context) []float32 {
	widths := make([]float32, len(navItems))
	for i, it := range navItems {
		w, _ := c.MeasureText(0, ui.Span{Text: it.label, Size: controlFontSize})
		widths[i] = w + 24
	}
	return widths
}

func (a *App) navigationStatus() string {
	if a.k.Running() {
		return "内核运行中"
	}
	return "内核已停止"
}

// Paint the same destinations between their two native layouts. A single
// reversible channel drives their geometry and the workspace edge. These
// decorative copies sit above the workspace, outside the sidebar's scroll
// clip, and never duplicate its accessibility or keyboard controls.
func (a *App) navigationFlight(c *ui.Context, progress float32) {
	pal, bar := paletteFor(c), c.TitleBar()
	height := max(bar.Height, 38)
	width, _ := c.Size()
	widths := topNavigationWidths(c)
	var total float32
	for _, w := range widths {
		total += w + 2
	}
	total -= 2
	status, _ := c.MeasureText(0, ui.Span{Text: a.navigationStatus(), Size: 11})
	left, right := navigationLeading(c)+46, bar.Right+32+5+6+status
	x, y := left+(width-left-right-total)/2, height+64
	lerp := func(a, b float32) float32 { return a + (b-a)*progress }
	for i, it := range navItems {
		if it.id == "settings" {
			y += 12 + navigationGap
		}
		// Both layouts use the same 12-DIP label inset. The sidebar's icon
		// and its gap dissolve while the vertical list fans into a row.
		row := ui.Row(c.Key(it.id)).Absolute().Left(lerp(navigationPaddingX, x)).
			Top(lerp(y, (height-30)/2)).Width(lerp(sidebarWidth-1-2*navigationPaddingX, widths[i])).
			Height(lerp(navigationRowHeight, 30)).PaddingX(12).AlignItems(ui.Center).
			Gap(10 * (1 - progress)).Radius(8).Role(ui.RoleNone)
		fg, icon := pal.Muted, pal.Faint
		weight := 400
		if a.page == it.id {
			row.Background(pal.Selected)
			fg, icon = pal.Text.Mix(pal.Accent, progress), pal.Accent
			weight = int(lerp(600, 400))
		}
		row.Children(func() {
			ui.Box(c).Width(16 * (1 - progress)).Height(16).Shrink(0).Children(func() {
				ui.Icon(c, it.icon).Size(16, 16).TextColor(icon).Opacity(max(0, 1-3*progress)).Role(ui.RoleNone)
			})
			ui.Text(c, it.label).FontSize(lerp(13, controlFontSize)).FontWeight(weight).
				TextColor(fg).SingleLine().Role(ui.RoleNone)
		})
		x += widths[i] + 2
		y += navigationRowHeight + navigationGap
	}
	ui.Row(c.Key("brand")).Absolute().Left(lerp(20, bar.Left+8)).Top(lerp(height+16, (height-24)/2)).
		Height(lerp(32, 24)).AlignItems(ui.Center).Gap(lerp(8, 6)).Role(ui.RoleNone).Children(func() {
		ui.Image(c, brandLogo).Size(lerp(32, 24), lerp(32, 24)).Role(ui.RoleNone)
		ui.Text(c, "Gopurr").FontSize(lerp(16, 13)).FontWeight(600).TextColor(pal.Text).Role(ui.RoleNone)
	})
}
