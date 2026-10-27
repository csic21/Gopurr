package app

import (
	"embed"

	"github.com/egoist/mygo/ui"
)

// Lucide's 24-unit grid and two-unit stroke are shared by navigation,
// status and action icons. Keep upstream artwork and its license together.
//
//go:embed icons/*.svg
var iconFiles embed.FS

var (
	iconOverview = mustIcon("layout-dashboard")
	iconSubs     = mustIcon("layers-2")
	iconProxies  = mustIcon("globe")
	iconConns    = mustIcon("cable")
	iconRules    = mustIcon("list-filter")
	iconLogs     = mustIcon("square-terminal")
	iconSettings = mustIcon("settings-2")
	iconSearch   = mustIcon("search")
	iconDown     = mustIcon("arrow-down-left")
	iconUp       = mustIcon("arrow-up-right")
	iconActivity = mustIcon("activity")
	iconShield   = mustIcon("shield-check")
	iconNetwork  = mustIcon("network")
	iconCheck    = mustIcon("circle-check")
	iconCircle   = mustIcon("circle")
	iconClose    = mustIcon("x")
	iconRefresh  = mustIcon("refresh-cw")
	iconFile     = mustIcon("file-text")
)

func mustIcon(name string) *ui.SVG {
	data, err := iconFiles.ReadFile("icons/" + name + ".svg")
	if err != nil {
		panic(err)
	}
	return ui.MustParseSVG(data)
}

// iconButton preserves native keyboard handling and focus rings. Repeated
// row actions remain visible without dominating the contents of a list.
func iconButton(c *ui.Context, icon *ui.SVG, label string) ui.Element {
	pal := paletteFor(c)
	b := ui.ButtonBase(c).Size(30, 30).Radius(7).Label(label).Tooltip(label)
	bg, fg := ui.Transparent, pal.Muted
	if !b.IsDisabled() {
		if b.Pressed() {
			bg, fg = pal.Selected, pal.Accent
		} else if b.Hovered() || b.FocusVisible() {
			bg, fg = pal.Hover, pal.Text
		}
	}
	return b.Background(bg).Children(func() {
		ui.Icon(c, icon).Size(15, 15).TextColor(fg).Role(ui.RoleNone)
	})
}
