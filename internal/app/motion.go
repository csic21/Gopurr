package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// Motion settles between interactions. There are no idle loops, artificial
// operation delays, or bouncing controls; the desktop's Reduce Motion setting
// is handled by MyGo's animation channels and transitions.
var (
	gentlePanel = ui.Animation{Duration: 280 * time.Millisecond, Ease: ui.EaseOut}
	gentleText  = ui.Animation{Duration: 180 * time.Millisecond, Ease: ui.EaseOut}
	gentleData  = ui.Animation{Duration: 400 * time.Millisecond, Ease: ui.EaseOut}
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
	track := parts.Track.AlignItems(ui.Stretch).Padding(t.Space(0.5)).Gap(t.Space(0.5)).
		Radius(t.Radius).Background(t.Surface).Border(1, t.Border)
	track.Children(func() {
		for i, label := range labels {
			s := parts.Segment(i).Padding(t.Space(1), t.Space(3)).Radius(max(t.Radius-t.Space(0.5), 0)).TextColor(t.Text).
				Transition(colorMotion())
			bg := ui.Transparent
			if s.Hovered() {
				bg = t.SurfaceHover
			}
			if s.Pressed() {
				bg = t.SurfacePressed
			}
			if i == *selected {
				bg = t.Background
				if t.Dark {
					bg = t.SurfacePressed
				}
				s.Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.12))
			}
			s.Background(bg).Children(func() { ui.Text(c, label).SingleLine() })
		}
	})
	return track
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
