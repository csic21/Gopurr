package app

import (
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Read the actual painted pill, rather than just an animation's numeric value.
func selectionSpan(t *testing.T, tr *ui.Tester, bounds ui.Rect, fill ui.Color, vertical bool) (int, int) {
	t.Helper()
	img := tr.Image()
	first, last := -1, -1
	start, end := int(bounds.X)+3, int(bounds.X+bounds.W)-3
	if vertical {
		start, end = int(bounds.Y), int(bounds.Y+bounds.H)
	}
	for at := start; at < end; at++ {
		x, y := at, int(bounds.Y+bounds.H/2)
		if vertical {
			x, y = int(bounds.X)+18, at
		}
		p := img.RGBAAt(x, y)
		if p.R == fill.R && p.G == fill.G && p.B == fill.B {
			if first == -1 {
				first = at
			}
			last = at
		}
	}
	if first == -1 {
		t.Fatalf("no painted selection in %+v", bounds)
	}
	return first, last
}

func TestSelectionSlidesWithoutMovingControls(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, size := range [][2]int{{1060, 720}, {840, 560}} {
			t.Run(fmt.Sprintf("%v-%dx%d", dark, size[0], size[1]), func(t *testing.T) {
				page, selected, disabled, shown := "overview", 0, false, true
				view := func(c *ui.Context) {
					c.SetTheme(theme(dark))
					if c.Preferences().ReduceMotion {
						c.DisableAnimations()
					}
					ui.Row(c).Fill().Background(c.Theme().Background).Children(func() {
						ui.Column(c).Width(sidebarWidth).FillHeight().PaddingY(60).Background(paletteFor(c).Side).Children(func() {
							softSidebar(c, &page).Label("测试侧栏")
						})
						ui.Column(c).Grow(1).Padding(24).Children(func() {
							if shown {
								softSegmented(c, &selected, "A", "较长的分类 B", "C").AlignSelf(ui.Start).Label("测试分段").Disabled(disabled)
							}
						})
					})
				}
				tr := ui.NewTester(view, size[0], size[1])
				find := func(label string) ui.Rect {
					r, ok := tr.Find(label)
					if !ok {
						t.Fatalf("missing %q", label)
					}
					return r
				}
				click := func(label string) {
					if err := tr.Click(label); err != nil {
						t.Fatal(err)
					}
				}
				fill := theme(dark).Background
				if dark {
					fill = theme(dark).SurfacePressed
				}
				track, a, b, c := find("测试分段"), find("A"), find("较长的分类 B"), find("C")
				start, _ := selectionSpan(t, tr, track, fill, false)
				click("C")
				if selected != 2 || find("A") != a || find("较长的分类 B") != b || find("C") != c {
					t.Fatal("selection was delayed or the controls moved")
				}
				tr.Advance(60 * time.Millisecond)
				tr.Frame()
				middle, _ := selectionSpan(t, tr, track, fill, false)
				if middle <= start+3 || middle >= int(c.X)-3 {
					t.Fatalf("selection jumped instead of sliding: %d -> %d -> %.1f", start, middle, c.X)
				}
				click("较长的分类 B")
				reversed, _ := selectionSpan(t, tr, track, fill, false)
				if math.Abs(float64(reversed-middle)) > 2 {
					t.Fatalf("retargeting jumped: %d -> %d", middle, reversed)
				}
				tr.Advance(time.Second)
				left, right := selectionSpan(t, tr, track, fill, false)
				if math.Abs(float64(left)-float64(b.X)) > 2 || math.Abs(float64(right-left+1)-float64(b.W)) > 2 {
					t.Fatalf("selection does not fit the unequal label: %d..%d vs %+v", left, right, b)
				}
				tr.Key(0, ui.KeyRight)
				if selected != 2 {
					t.Fatal("segmented keyboard navigation stopped working")
				}
				disabled = true
				tr.Frame()
				click("A")
				if selected != 2 {
					t.Fatal("disabled segment changed selection")
				}
				disabled = false
				tr.Frame()
				// Navigation includes the larger gap before Settings.
				nav := find("测试侧栏")
				initial, _ := selectionSpan(t, tr, nav, paletteForDark(dark).Selected, true)
				click("设置")
				if page != "settings" {
					t.Fatal("sidebar delayed the selected page")
				}
				tr.Advance(60 * time.Millisecond)
				tr.Frame()
				navMiddle, _ := selectionSpan(t, tr, nav, paletteForDark(dark).Selected, true)
				if navMiddle <= initial+3 || navMiddle >= int(find("设置").Y)-3 {
					t.Fatalf("sidebar did not slide: %d -> %d", initial, navMiddle)
				}
				click("订阅")
				navReversed, _ := selectionSpan(t, tr, nav, paletteForDark(dark).Selected, true)
				if math.Abs(float64(navReversed-navMiddle)) > 2 {
					t.Fatal("sidebar reversal jumped to a row")
				}
				tr.Key(0, ui.KeyDown)
				if page != "proxies" {
					t.Fatal("sidebar lost arrow-key navigation")
				}
				tr.Advance(time.Second)
				y, _ := selectionSpan(t, tr, nav, paletteForDark(dark).Selected, true)
				if math.Abs(float64(y)-float64(find("代理").Y)) > 2 || tr.FramePending() {
					t.Fatal("sidebar failed to land on the selected row or kept drawing")
				}
				// Reopening initializes at the chosen segment; reduced motion
				// also takes the new position immediately, even mid-animation.
				shown = false
				tr.Frame()
				shown, selected = true, 1
				tr.Frame()
				left, _ = selectionSpan(t, tr, find("测试分段"), fill, false)
				if math.Abs(float64(left)-float64(find("较长的分类 B").X)) > 2 {
					t.Fatal("reopened control animated from a stale selection")
				}
				click("A")
				tr.Advance(50 * time.Millisecond)
				tr.SetPreferences(ui.Preferences{ReduceMotion: true})
				click("C")
				click("设置")
				left, _ = selectionSpan(t, tr, find("测试分段"), fill, false)
				y, _ = selectionSpan(t, tr, nav, paletteForDark(dark).Selected, true)
				if math.Abs(float64(left)-float64(find("C").X)) > 2 || math.Abs(float64(y)-float64(find("设置").Y)) > 2 || tr.FramePending() {
					t.Fatalf("reduced motion did not settle: x=%d target=%+v, y=%d target=%+v, pending=%v", left, find("C"), y, find("设置"), tr.FramePending())
				}
			})
		}
	}
}

func TestSelectionFitsRTLAndScaledTextAfterResize(t *testing.T) {
	for _, direction := range []ui.Direction{ui.LeftToRight, ui.RightToLeft} {
		for _, scale := range []float32{1, 1.4} {
			t.Run(fmt.Sprintf("%v-%v", direction, scale), func(t *testing.T) {
				selected := 0
				tr := ui.NewTester(func(c *ui.Context) {
					c.SetTheme(theme(false))
					ui.Column(c).Fill().Padding(24).Background(c.Theme().Background).Children(func() {
						softSegmented(c, &selected, "短", "更长的标签", "末项").FillWidth().Label("自适应分段")
					})
				}, 1060, 720)
				tr.SetDirection(direction)
				tr.SetPreferences(ui.Preferences{TextScale: scale})
				selected = 1
				tr.Frame()
				tr.Advance(60 * time.Millisecond)
				tr.SetSize(840, 560)
				tr.Advance(time.Second)
				track, _ := tr.Find("自适应分段")
				segment, _ := tr.Find("更长的标签")
				left, right := selectionSpan(t, tr, track, theme(false).Background, false)
				if math.Abs(float64(left)-float64(segment.X)) > 2 || math.Abs(float64(right-left+1)-float64(segment.W)) > 2 || tr.FramePending() {
					t.Fatalf("selection missed the resized/scaled segment: %d..%d vs %+v", left, right, segment)
				}
			})
		}
	}
}

func saveSelectionFrame(t *testing.T, name string, img image.Image) {
	t.Helper()
	if dir := os.Getenv("GOPURR_SELECTION_FRAMES"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, img)
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("save selection frame: %v %v", err, closeErr)
		}
	}
}

func TestSelectionOverviewFrames(t *testing.T) {
	a := subscriptionTestApp(t)
	a.booted, a.ticking = true, true
	for _, dark := range []bool{false, true} {
		for _, size := range [][2]int{{1060, 720}, {840, 560}} {
			a.page, a.modeToken, a.modeIdx = "overview", "rule", 0
			a.prefs.Appearance = AppearanceLight
			if dark {
				a.prefs.Appearance = AppearanceDark
			}
			tr := ui.NewTester(a.View, size[0], size[1])
			tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 88})
			tr.Advance(time.Second)
			prefix := fmt.Sprintf("%v-%dx%d", dark, size[0], size[1])
			for _, action := range []struct{ label, name string }{{"全局", "mode"}, {"订阅", "sidebar"}} {
				if err := tr.Click(action.label); err != nil {
					t.Fatal(err)
				}
				elapsed := time.Duration(0)
				for _, at := range []time.Duration{0, 60, 120, 260} {
					tr.Advance((at - elapsed) * time.Millisecond)
					elapsed = at
					saveSelectionFrame(t, fmt.Sprintf("%s-%s-%03d", prefix, action.name, at), tr.Image())
				}
			}
		}
	}
}
