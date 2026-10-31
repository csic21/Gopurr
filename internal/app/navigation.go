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
	// Keep the expanded toggle inside the workspace, then bring it back beside
	// the compact brand using the existing layout transition.
	compactLeading := navigationLeading(c)
	leading := max(compactLeading, sidebarWidth+8)*(1-progress) + compactLeading*progress
	navX, _ := a.topNavigationBounds(c)
	sidebar := sidebarWidth * (1 - progress)
	ui.Row(c).FillWidth().Height(height).Shrink(0).AlignItems(ui.Center).
		Draw(func(p *ui.Painter, r ui.Rect) {
			// Absolute children paint above flow controls, even with PassThrough.
			// Keep the background and continuous sidebar divider below controls.
			if sidebar > 0 {
				r.W = sidebar
				p.Fill(r, pal.Side, 0)
				p.Fill(ui.Rect{X: r.X + sidebar - 1, Y: r.Y, W: 1, H: r.H}, pal.Border, 0)
			}
		}).DragWindow().Label(tr("窗口标题栏")).Children(func() {
		brand := ui.Row(c.Key("toolbar-brand")).Width(leading).Shrink(0).Padding(0, 0, 0, bar.Left+8).
			AlignItems(ui.Center).Gap(6)
		if moving {
			brand.Opacity(0)
		}
		brand.Children(func() {
			if collapsed {
				ui.Image(c, brandLogo).Size(24, 24).Shrink(0).Label(tr("Gopurr 蜷睡猫标记"))
				ui.Text(c, "Gopurr").FontSize(13).FontWeight(600).SingleLine().TextColor(pal.Text)
			}
		})
		ui.Row(c).PaddingX(4).AlignItems(ui.Center).Children(func() {
			label := tr("收起侧栏")
			if collapsed {
				label = tr("展开侧栏")
			}
			iconButton(c, iconSidebar, label).OnClick(a.toggleSidebar)
		})
		ui.Row(c.Key("navigation-center")).Grow(1).MinWidth(0).FillHeight().AlignItems(ui.Center).
			Padding(0, 0, 0, navX-leading-38).
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
					ui.Text(c, tr("工作区")).FontSize(11.5).TextColor(pal.Faint)
					ui.Icon(c, iconChevron).Size(12, 12).TextColor(pal.Faint).Role(ui.RoleNone)
					for _, it := range navItems {
						if it.id == a.page {
							ui.Text(c, tr(it.label)).FontSize(11.5).FontWeight(600).TextColor(pal.Muted).Role(ui.RoleNone)
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
	return softTopNavigation(c, &selected).Label(tr("页面导航")).OnChange(func() {
		a.page = navItems[selected].id
	})
}

func navigationLeading(c *ui.Context) float32 {
	brand, _ := c.MeasureText(0, ui.Span{Text: "Gopurr", Size: 13, Weight: 600})
	return c.TitleBar().Left + 8 + 24 + 6 + brand
}

// Center against the window, independently of the unequal brand/status widths.
// Reserve native controls on either side when a narrow window needs more room.
func (a *App) topNavigationBounds(c *ui.Context) (x, width float32) {
	for _, w := range topNavigationWidths(c) {
		width += w + 2
	}
	width -= 2
	viewport, _ := c.Size()
	status, _ := c.MeasureText(0, ui.Span{Text: a.navigationStatus(), Size: 11})
	left := navigationLeading(c) + 38 + 12
	right := c.TitleBar().Right + 32 + 5 + 6 + status
	return max(left, min((viewport-width)/2, viewport-right-width)), width
}

func topNavigationWidths(c *ui.Context) []float32 {
	widths := make([]float32, len(navItems))
	for i, it := range navItems {
		w, _ := c.MeasureText(0, ui.Span{Text: tr(it.label), Size: controlFontSize})
		widths[i] = w + 2*topNavigationPadding(c)
	}
	return widths
}

func topNavigationPadding(c *ui.Context) float32 {
	width, _ := c.Size()
	padding := float32(12)
	if width < 900 {
		padding = 10
	}
	var labels float32
	for _, it := range navItems {
		w, _ := c.MeasureText(0, ui.Span{Text: tr(it.label), Size: controlFontSize})
		labels += w
	}
	var status float32
	for _, label := range []string{tr("内核运行中"), tr("内核已停止")} {
		w, _ := c.MeasureText(0, ui.Span{Text: label, Size: 11})
		status = max(status, w)
	}
	available := width - (navigationLeading(c) + 50) - (c.TitleBar().Right + 43 + status)
	fit := (available - labels - float32(len(navItems)-1)*2) / float32(2*len(navItems))
	return max(4, min(padding, fit))
}

func (a *App) navigationStatus() string {
	if a.k.Running() {
		return tr("内核运行中")
	}
	return tr("内核已停止")
}

// Paint the same destinations between their two native layouts. A single
// reversible channel drives their geometry and the workspace edge. These
// decorative copies sit above the workspace, outside the sidebar's scroll
// clip, and never duplicate its accessibility or keyboard controls.
func (a *App) navigationFlight(c *ui.Context, progress float32) {
	pal, bar := paletteFor(c), c.TitleBar()
	height := max(bar.Height, 38)
	widths := topNavigationWidths(c)
	x, _ := a.topNavigationBounds(c)
	y := height + 64
	lerp := func(a, b float32) float32 { return a + (b-a)*progress }
	for i, it := range navItems {
		if it.id == "settings" {
			y += 12 + navigationGap
		}
		// Both layouts use the same 12-DIP label inset. The sidebar's icon
		// and its gap dissolve while the vertical list fans into a row.
		row := ui.Row(c.Key(it.id)).Absolute().Left(lerp(navigationPaddingX, x)).
			Top(lerp(y, (height-30)/2)).Width(lerp(sidebarWidth-1-2*navigationPaddingX, widths[i])).
			Height(lerp(navigationRowHeight, 30)).PaddingX(lerp(12, topNavigationPadding(c))).AlignItems(ui.Center).
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
			ui.Text(c, tr(it.label)).FontSize(lerp(13, controlFontSize)).FontWeight(weight).
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
