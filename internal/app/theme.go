package app

import (
	"fmt"

	ui "gopurr/internal/appui"
)

const (
	sidebarWidth         = 184
	controlFontSize      = 12.5
	actionButtonHeight   = 30
	actionButtonPaddingX = 12
	navigationRowHeight  = 36
	navigationGap        = 2
	navigationPaddingX   = 10
)

// Both appearances share semantic tokens. Resolve them from the frame, never
// by mutating package globals: draw callbacks may outlive a theme switch.
type palette struct {
	Bg, Side, Surface, Hover, Border               ui.Color
	Selected, SelectedBorder                       ui.Color
	Text, Muted, Faint                             ui.Color
	Accent, AccentText, AccentHover, AccentPressed ui.Color
	Up, Down, Danger, Warn, Success                ui.Color
	Grid                                           ui.Color
}

var darkPalette = palette{
	Bg:             ui.Hex("#08090d"),
	Side:           ui.Hex("#0c0e13"),
	Surface:        ui.Hex("#12151b"),
	Hover:          ui.Hex("#1b1f27"),
	Border:         ui.Hex("#262b34"),
	Selected:       ui.Hex("#1a2439"),
	SelectedBorder: ui.Hex("#4a6a9e"),
	Text:           ui.Hex("#f3f5f9"),
	Muted:          ui.Hex("#a6aebc"),
	Faint:          ui.Hex("#868f9f"),
	Accent:         ui.Hex("#8fb3ff"),
	AccentText:     ui.Hex("#0a1120"),
	AccentHover:    ui.Hex("#a7c4ff"),
	AccentPressed:  ui.Hex("#7c9ff0"),
	Up:             ui.Hex("#8fb3ff"),
	Down:           ui.Hex("#60d9b3"),
	Danger:         ui.Hex("#ff8f9a"),
	Warn:           ui.Hex("#e8c27c"),
	Success:        ui.Hex("#60d9b3"),
	Grid:           ui.Hex("#21262f"),
}

var lightPalette = palette{
	Bg:             ui.Hex("#eef1f6"),
	Side:           ui.Hex("#e8ecf3"),
	Surface:        ui.Hex("#ffffff"),
	Hover:          ui.Hex("#e4e9f1"),
	Border:         ui.Hex("#dbe0e9"),
	Selected:       ui.Hex("#e6eefd"),
	SelectedBorder: ui.Hex("#7e9cdd"),
	Text:           ui.Hex("#101620"),
	Muted:          ui.Hex("#46536a"),
	Faint:          ui.Hex("#5d6879"),
	Accent:         ui.Hex("#2d5fd8"),
	AccentText:     ui.Hex("#ffffff"),
	AccentHover:    ui.Hex("#2452c1"),
	AccentPressed:  ui.Hex("#1c43a4"),
	Up:             ui.Hex("#2d5fd8"),
	Down:           ui.Hex("#0b7158"),
	Danger:         ui.Hex("#bf2c47"),
	Warn:           ui.Hex("#86570f"),
	Success:        ui.Hex("#0b7158"),
	Grid:           ui.Hex("#e1e6ef"),
}

func paletteFor(c *ui.Context) palette {
	return paletteForDark(c.Theme().Dark)
}

func paletteForDark(dark bool) palette {
	if dark {
		return darkPalette
	}
	return lightPalette
}

// WindowBackgroundColor keeps the native backing surface in sync with the
// palette, including the first frame before the content has painted.
func WindowBackgroundColor() string {
	light, dark := lightPalette.Bg, darkPalette.Bg
	return fmt.Sprintf("light-dark(#%02x%02x%02x, #%02x%02x%02x)",
		light.R, light.G, light.B, dark.R, dark.G, dark.B)
}

// theme hands mygo's own widgets the same palette, so dialogs, inputs, buttons
// and switches stop looking like a different app.
func theme(dark bool) *ui.Theme {
	pal := paletteForDark(dark)
	t := ui.LightTheme()
	if dark {
		t = ui.DarkTheme()
	}
	t.Background = pal.Bg
	t.Surface = pal.Surface
	t.SurfaceHover = pal.Hover
	t.SurfacePressed = pal.Selected
	t.Border = pal.Border
	t.Text = pal.Text
	t.TextMuted = pal.Muted
	t.Accent = pal.Accent
	t.AccentHover = pal.AccentHover
	t.AccentPressed = pal.AccentPressed
	t.AccentText = pal.AccentText
	t.Danger = pal.Danger
	t.Warning = pal.Warn
	t.Success = pal.Success
	t.Selection = pal.Accent.Alpha(0.28)
	t.Focus = pal.Accent.Alpha(0.75)
	t.Scrollbar = pal.Muted.Alpha(0.35)
	t.Radius = 8
	t.Spacing = 4
	t.FontSize = controlFontSize
	return t
}

// card is the one surface used for grouped content.
func card(c *ui.Context) ui.Element {
	pal := paletteFor(c)
	return ui.Column(c).
		FillWidth().
		Padding(16).
		Radius(12).
		Background(pal.Surface).
		Gap(10)
}

// pill is a small rounded label, used for latency and states.
func pill(c *ui.Context, text string, fg, bg ui.Color) ui.Element {
	return ui.Row(c).
		PaddingX(8).PaddingY(3).
		Radius(999).
		Background(bg).
		Children(func() {
			ui.Text(c, text).SingleLine().FontSize(11).FontWeight(500).TextColor(fg)
		})
}

// Empty content has one surface, with a contextual icon and an optional recovery
// action. Dialogs keep their bounded height; page lists fill the available area.
func emptyState(c *ui.Context, icon *ui.SVG, title, hint string) {
	pal := paletteFor(c)
	ui.Column(c).FillWidth().Radius(10).Background(pal.Surface).Border(1, pal.Border).Children(func() {
		emptyStateContent(c, icon, title, hint, nil)
	})
}

func emptyListState(c *ui.Context, icon *ui.SVG, title, hint string, action func()) {
	pal := paletteFor(c)
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Radius(10).Background(pal.Surface).Border(1, pal.Border).Children(func() {
		emptyStateContent(c, icon, title, hint, action)
	})
}

// Use this inside an existing list surface to avoid nested card borders.
func emptyStateContent(c *ui.Context, icon *ui.SVG, title, hint string, action func()) {
	pal := paletteFor(c)
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Padding(16).AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
		ui.Row(c).Size(44, 44).Radius(12).Background(pal.Hover).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			ui.Icon(c, icon).Size(24, 24).TextColor(pal.Muted).Role(ui.RoleNone)
		})
		ui.Text(c, title).FillWidth().FontSize(14).FontWeight(600).TextAlign(ui.Center).TextColor(pal.Text)
		ui.Text(c, hint).FillWidth().MaxWidth(400).FontSize(12).TextAlign(ui.Center).TextColor(pal.Muted)
		if action != nil {
			softButton(c, tr("重置筛选")).OnClick(action)
		}
	})
}

// sectionLabel is the small caps-ish label above a block.
func sectionLabel(c *ui.Context, text string) {
	pal := paletteFor(c)
	ui.Text(c, text).FontSize(11).FontWeight(600).TextColor(pal.Muted)
}

// latencyPill colours a measured delay.
func latencyPill(c *ui.Context, d uint16, known bool) {
	pal := paletteFor(c)
	if !known {
		return
	}
	switch {
	case d == 0:
		pill(c, tr("超时"), pal.Danger, pal.Danger.Alpha(0.10))
	case d < 200:
		pill(c, uintText(d), pal.Success, pal.Success.Alpha(0.10))
	case d < 500:
		pill(c, uintText(d), pal.Warn, pal.Warn.Alpha(0.10))
	default:
		pill(c, uintText(d), pal.Danger, pal.Danger.Alpha(0.10))
	}
}

func uintText(d uint16) string { return fmt.Sprintf("%dms", d) }

// Light-mode choices, shared by the settings page and the window side.
var lightDelays = []int{30, 60, 300}

func delayIndexOf(secs int) int {
	for i, d := range lightDelays {
		if d == secs {
			return i
		}
	}
	return 1
}

func delayLabelOf(secs int) string {
	switch secs {
	case 30:
		return tr("30 秒")
	case 300:
		return tr("5 分钟")
	default:
		return tr("1 分钟")
	}
}

// settingsGroup is a titled block of rows, the shape macOS System Settings and
// Magpie both use: one surface, hairline separators, controls on the right.
func settingsGroup(c *ui.Context, title string, rows func()) {
	pal := paletteFor(c)
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		if title != "" {
			ui.Text(c, title).FontSize(14).FontWeight(600).TextColor(pal.Text).Margin(0, 0, 0, 4)
		}
		ui.Column(c).FillWidth().Radius(12).Background(pal.Surface).Children(rows)
	})
}

func settingsRow(c *ui.Context, title, desc string, control func()) {
	pal := paletteFor(c)
	ui.Row(c).FillWidth().MinHeight(52).Padding(8, 16).AlignItems(ui.Center).Gap(16).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, title).FillWidth().FontSize(13).FontWeight(500).TextColor(pal.Text)
			if desc != "" {
				ui.Text(c, desc).FillWidth().FontSize(11.5).TextColor(pal.Muted)
			}
		})
		if control != nil {
			control()
		}
	})
}

// The overview uses one alignment for status, optional actions and switches.
// Rows grow for wrapped status text instead of reserving empty card height.
func overviewControlRow(c *ui.Context, title, status string, statusColor ui.Color, control func()) {
	pal := paletteFor(c)
	ui.Row(c).FillWidth().MinHeight(44).Padding(8, 16).AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Text(c, title).Width(76).FontSize(13).FontWeight(500).TextColor(pal.Text)
		ui.Text(c, status).Grow(1).MinWidth(0).MaxLines(2).Ellipsis("…").
			FontSize(11).TextColor(statusColor).Tooltip(status)
		control()
	})
}

func controlIcon(c *ui.Context, icon *ui.SVG) {
	pal := paletteFor(c)
	ui.Box(c).Size(30, 30).Radius(8).Background(pal.Hover).AlignItems(ui.Center).Justify(ui.Center).
		Children(func() { ui.Icon(c, icon).Size(17, 17).TextColor(pal.Muted).Role(ui.RoleNone) })
}

func hairline(c *ui.Context) {
	pal := paletteFor(c)
	ui.Box(c).FillWidth().Height(1).Background(pal.Grid)
}

func sectionHeading(c *ui.Context, title, detail string) {
	pal := paletteFor(c)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Text(c, title).FontSize(14).FontWeight(600).TextColor(pal.Text)
		ui.Spacer(c)
		if detail != "" {
			ui.Text(c, detail).MinWidth(0).SingleLine().Ellipsis("…").Tooltip(detail).FontSize(11).TextColor(pal.Faint)
		}
	})
}

// allAdapters is the "no filter" choice of the rules page.
const allAdapters = "全部出口"

// searchRow is our own search field: mygo's SearchField hardcodes an English
// placeholder, so the app can supply a localized one.
func searchRow(c *ui.Context, value *string, placeholder string) ui.Element {
	pal := paletteFor(c)
	return ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).
		Padding(8, 12).Radius(8).Background(pal.Surface).Border(1, pal.Border).
		Children(func() {
			if iconSearch != nil {
				ui.Icon(c, iconSearch).Size(16, 16).TextColor(pal.Faint).Role(ui.RoleNone)
			}
			ui.TextInputBase(c, value).Grow(1).MinWidth(0).Placeholder(placeholder).FontSize(12).Label(placeholder)
			if *value != "" {
				iconButton(c, iconClose, tr("清空搜索")).Size(24, 24).OnClick(func() { *value = "" })
			}
		})
}
