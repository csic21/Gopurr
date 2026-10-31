package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// Motion settles between interactions. There are no idle loops, artificial
// operation delays, or bouncing controls; the desktop's Reduce Motion setting
// is handled by MyGo's animation channels and transitions.
var (
	gentlePanel     = ui.Animation{Duration: 280 * time.Millisecond, Ease: ui.EaseOut}
	gentleText      = ui.Animation{Duration: 180 * time.Millisecond, Ease: ui.EaseOut}
	gentleData      = ui.Animation{Duration: 400 * time.Millisecond, Ease: ui.EaseOut}
	gentleSelection = ui.Animation{Duration: 240 * time.Millisecond, Ease: ui.EaseOut}
	navigationMorph = ui.Animation{Duration: 320 * time.Millisecond, Ease: ui.CubicBezier(.42, 0, .58, 1)}
)

func colorMotion() ui.ElementTransition {
	return ui.ElementTransition{Duration: 140 * time.Millisecond, Colors: true}
}

func pageMotion() ui.ElementTransition {
	return ui.ElementTransition{Animation: &gentlePanel, Colors: true, Enter: &ui.Motion{Y: 8, Opacity: 0}}
}

func contentMotion() ui.ContentTransition {
	return ui.ContentTransition{Animation: &gentleText}
}

func routeMotion() ui.ElementTransition {
	return ui.ElementTransition{Animation: &gentlePanel,
		Enter: &ui.Motion{Y: 4, Opacity: 0}, Exit: &ui.Motion{Y: -4, Opacity: 0}}
}

func softButton(c *ui.Context, label string) ui.Element {
	return actionButton(ui.Button(c, label))
}

func softPrimaryButton(c *ui.Context, label string) ui.Element {
	return actionButton(ui.PrimaryButton(c, label))
}

// Primary and secondary actions share geometry, including their border space.
func actionButton(button ui.Element) ui.Element {
	return button.Height(actionButtonHeight).Padding(0, actionButtonPaddingX).
		FontSize(controlFontSize).Transition(colorMotion())
}

// SwitchBase retains native accessibility, keyboard handling and Changed.
// One channel drives the track and thumb so rapid reversals stay in sync.
func softSwitch(c *ui.Context, on *bool) ui.Element {
	t := c.Theme()
	sw := ui.SwitchBase(c, on).Size(t.Space(9), t.Space(5)).Radius(t.Space(2.5))
	target := float32(0)
	if *on {
		target = 1
	}
	pos := sw.Spring("thumb", target, ui.SpringOptions{Duration: 240 * time.Millisecond})
	sw.Background(t.Border.Mix(t.Text, 0.15).Mix(t.Accent, pos))
	rtl := c.Direction() == ui.RightToLeft
	return sw.Draw(func(p *ui.Painter, r ui.Rect) {
		in := t.Space(0.5)
		d := r.H - 2*in
		at := pos
		if rtl {
			at = 1 - at
		}
		knob := ui.Rect{X: r.X + in + at*(r.W-r.H), Y: r.Y + in, W: d, H: d}
		p.Shadow(knob, d/2, 0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.25))
		p.Fill(knob, ui.RGB(255, 255, 255), d/2)
	})
}

func softSegmented(c *ui.Context, selected *int, labels ...string) ui.Element {
	t := c.Theme()
	parts := ui.SegmentedBase(c, selected, len(labels))
	inset, gap, padding := t.Space(0.5), t.Space(0.5), t.Space(3)
	track := parts.Track.AlignItems(ui.Stretch).Padding(inset).Gap(gap).
		Radius(t.Radius).Background(t.Surface).Border(1, t.Border)
	widths := make([]float32, len(labels))
	hovers := make([]ui.Color, len(labels))
	track.Children(func() {
		for i, label := range labels {
			w, _ := c.MeasureText(0, ui.Span{Text: label, Size: t.FontSize})
			widths[i] = w + 2*padding
			s := parts.Segment(i).Padding(t.Space(1), padding).TextColor(t.Text).Label(label)
			hover, pressed := float32(0), float32(0)
			if i != *selected && (s.Hovered() || s.Pressed()) {
				hover = 1
			}
			if i != *selected && s.Pressed() {
				pressed = 1
			}
			// Scalar channels also stop immediately when reduced motion is
			// enabled during a hover fade, including an unchanged target.
			a := ui.Animation{Duration: 140 * time.Millisecond}
			opacity := track.AnimateTo(selectionHover(i), hover, a)
			press := track.AnimateTo(selectionPress(i), pressed, a)
			hovers[i] = t.SurfaceHover.Mix(t.SurfacePressed, press).Alpha(opacity)
			s.Children(func() { ui.Text(c, label).FontSize(t.FontSize).SingleLine() })
		}
	})
	if len(labels) == 0 {
		return track
	}
	var target float32
	for i := 0; i < *selected; i++ {
		target += widths[i] + gap
	}
	x := track.AnimateTo("selection-x", target, gentleSelection)
	w := track.AnimateTo("selection-width", widths[*selected], gentleSelection)
	fill := t.Background
	if t.Dark {
		fill = t.SurfacePressed
	}
	rtl := c.Direction() == ui.RightToLeft
	// Paint beneath every label; only the shared background moves. Native
	// segments keep their real positions, focus, hit targets and radio state.
	track.Draw(func(p *ui.Painter, r ui.Rect) {
		at := inset + 1 // the border belongs to the track, not the segments
		radius := max(t.Radius-inset, 0)
		box := func(x, w float32) ui.Rect {
			if rtl {
				x = r.W - x - w
			}
			return ui.Rect{X: r.X + x, Y: r.Y + inset + 1, W: w, H: r.H - 2*(inset+1)}
		}
		for i, width := range widths {
			p.Fill(box(at, width), hovers[i], radius)
			at += width + gap
		}
		pill := box(inset+1+x, w)
		p.Shadow(pill, radius, 0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.12))
		p.Fill(pill, fill, radius)
	})
	return track
}

type selectionHover int
type selectionPress int

// Toolbar destinations share native radio/arrow-key behavior with segmented
// controls. A single quiet selection surface moves beneath stationary labels.
func softTopNavigation(c *ui.Context, selected *int) ui.Element {
	pal := paletteFor(c)
	parts := ui.SegmentedBase(c, selected, len(navItems))
	track := parts.Track.Height(30).Gap(2).AlignItems(ui.Stretch)
	widths := topNavigationWidths(c)
	hovers := make([]ui.Color, len(navItems))
	track.Children(func() {
		for i, it := range navItems {
			item := parts.Segment(i).Width(widths[i]).Radius(8).Label(tr(it.label))
			hover := float32(0)
			if i != *selected && (item.Hovered() || item.Pressed()) {
				hover = 1
			}
			hovers[i] = pal.Hover.Alpha(track.AnimateTo(selectionHover(i), hover,
				ui.Animation{Duration: 140 * time.Millisecond}))
			fg := pal.Muted
			if i == *selected {
				fg = pal.Accent
			}
			item.Children(func() {
				ui.Text(c, tr(it.label)).FontSize(controlFontSize).SingleLine().TextColor(fg)
			})
		}
	})
	var target float32
	for i := 0; i < *selected; i++ {
		target += widths[i] + 2
	}
	x := track.AnimateTo("selection-x", target, gentleSelection)
	w := track.AnimateTo("selection-width", widths[*selected], gentleSelection)
	rtl := c.Direction() == ui.RightToLeft
	track.Draw(func(p *ui.Painter, r ui.Rect) {
		box := func(x, width float32) ui.Rect {
			if rtl {
				x = r.W - x - width
			}
			return ui.Rect{X: r.X + x, Y: r.Y, W: width, H: r.H}
		}
		var at float32
		for i, width := range widths {
			p.Fill(box(at, width), hovers[i], 8)
			at += width + 2
		}
		p.Fill(box(x, w), pal.Selected, 8)
	})
	return track
}

func softSidebar(c *ui.Context, selected *string) ui.Element {
	pal := paletteFor(c)
	positions := make(map[string]float32, len(navItems))
	nav := ui.Sidebar(c, selected, func() {
		var y float32
		for _, it := range navItems {
			if it.id == "settings" {
				ui.Box(c).FillWidth().Height(12)
				y += 12 + navigationGap
			}
			positions[it.id] = y
			y += navigationRowHeight + navigationGap
			itemTheme := *theme(c.Theme().Dark)
			itemTheme.Accent = pal.Faint
			if *selected == it.id {
				itemTheme.Accent, itemTheme.AccentText = pal.Accent, pal.Accent
			}
			c.SetTheme(&itemTheme)
			item := ui.SidebarItem(c, it.id, it.icon, tr(it.label)).
				Height(navigationRowHeight).Padding(8, 12).Gap(10).FontSize(13).
				Background(ui.Transparent).TextColor(pal.Muted).Label(tr(it.label))
			if !c.Preferences().ReduceMotion {
				item.Transition(colorMotion())
			}
			if *selected == it.id {
				item.TextColor(pal.Text).FontWeight(600)
			}
		}
		c.SetTheme(theme(c.Theme().Dark))
	}).FillWidth().Padding(0, navigationPaddingX).Gap(navigationGap).Background(pal.Side)
	target, valid := positions[*selected] // Sidebar also handles keyboard input.
	y := nav.AnimateTo("selection-y", target, gentleSelection)
	if valid {
		nav.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X + navigationPaddingX, Y: r.Y + y,
				W: r.W - 2*navigationPaddingX, H: navigationRowHeight}, pal.Selected, 8)
		})
	}
	return nav
}

func modalBackdrop(back ui.Element) {
	back.Background(ui.RGBA(0, 0, 0, 0.4)).Transition(ui.ElementTransition{
		Duration: 160 * time.Millisecond, Colors: true,
		Enter: &ui.Motion{Opacity: 0}, Exit: &ui.Motion{Opacity: 0},
	})
}

func modalPanel(c *ui.Context, panel ui.Element) {
	t := c.Theme()
	w, h := c.Size()
	panel.Padding(t.Space(5)).Gap(t.Space(3)).Radius(t.Space(2.5)).Background(t.Background).
		MaxWidth(w-t.Space(10)).MaxHeight(h-t.Space(10)).
		Shadow(0, 10, 30, 0, ui.RGBA(0, 0, 0, 0.3))
	// A dialog's panel and backdrop are born together, so a layout Enter on
	// the panel would be skipped. Seed its own finite channel on first mount.
	started := ui.Local(panel, "entrance", func() bool { return false })
	if !*started {
		panel.AnimateTo("entrance", 0, gentlePanel)
		*started = true
	}
	progress := panel.AnimateTo("entrance", 1, gentlePanel)
	panel.Opacity(progress)
	if !c.Preferences().ReduceMotion {
		scale := 0.98 + 0.02*progress
		panel.Translate(0, 8*(1-progress)).Scale(scale, scale)
	}
}

// Closing removes the live dialog immediately; the framework paints a short
// inert exit copy. Focus and actions never wait for the fade to finish.
func softModal(c *ui.Context, key string, open *bool, body func()) ui.Element {
	if !*open {
		return ui.Element{}
	}
	panel := ui.DialogBase(c.Key(key), open, func(back, panel ui.Element) {
		modalBackdrop(back)
		modalPanel(c, panel)
		body()
	})
	if !*open {
		// DialogBase handles Escape during the build. Rebuild now so focus
		// returns before the first exit frame, not on a later animation tick.
		c.Invalidate()
	}
	return panel
}

// Alerts keep their distinct semantics: clicking outside never confirms or
// dismisses a destructive action, and Escape returns AlertDismissed.
func softAlertDialog(c *ui.Context, open *bool, title, message string, buttons ...string) int {
	if !*open {
		return -1
	}
	t := c.Theme()
	chosen := -1
	var defaultButton ui.Element
	ui.Overlay(c, func() {
		back := ui.Box(c.Key("alert:" + title)).Absolute().Left(0).Top(0).Right(0).Bottom(0).Center().Modal()
		modalBackdrop(back)
		if back.OverlayShortcut(0, ui.KeyEscape) {
			chosen = ui.AlertDismissed
		}
		back.Children(func() {
			panel := ui.Column(c).Width(t.Space(75)).Role(ui.RoleAlertDialog).Label(title).Description(message)
			modalPanel(c, panel)
			panel.Children(func() {
				ui.Text(c, title).Bold().FontSize(t.FontSize + 1)
				if message != "" {
					ui.Text(c, message).TextColor(t.TextMuted)
				}
				ui.Row(c).Gap(t.Space(2)).Justify(ui.End).Margin(t.Space(2), 0, 0, 0).Children(func() {
					for i, label := range buttons {
						var b ui.Element
						if i == len(buttons)-1 {
							b = softPrimaryButton(c, label)
							defaultButton = b
						} else {
							b = softButton(c, label)
						}
						if b.Clicked() {
							chosen = i
						}
					}
				})
			})
		})
	})
	// Overlay must record the opener before the default action takes focus.
	defaultButton.AutoFocus()
	if chosen != -1 {
		*open = false
		c.Invalidate()
	}
	return chosen
}
