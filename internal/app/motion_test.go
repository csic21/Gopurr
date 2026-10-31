package app

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

type motionFixture struct {
	open, alert, on, disabled bool
	selected, confirmed       int
}

func (m *motionFixture) view(c *ui.Context) {
	c.SetTheme(theme(c.Theme().Dark))
	if c.Preferences().ReduceMotion {
		c.DisableAnimations()
	}
	ui.Overlay(c, func() {})
	ui.Column(c).Fill().Padding(24).Gap(16).Background(c.Theme().Background).Children(func() {
		softButton(c, "打开弹窗").AutoFocus().OnClick(func() { m.open = true })
		softButton(c, "打开确认").OnClick(func() { m.alert = true })
		softSwitch(c, &m.on).Label("测试开关").Disabled(m.disabled)
		softSegmented(c, &m.selected, "规则模式", "全局模式", "直连模式").Label("测试模式").Disabled(m.disabled)
	})
	softModal(c, "motion-test", &m.open, func() {
		ui.Column(c).Width(320).Gap(12).Children(func() {
			ui.Text(c, "轻柔流动")
			softButton(c, "关闭弹窗").OnClick(func() { m.open = false })
		})
	})
	if softAlertDialog(c, &m.alert, "确认操作？", "此测试不会执行系统操作。", "取消", "确认") == 1 {
		m.confirmed++
	}
}

func pixels(tr *ui.Tester) []byte { return bytes.Clone(tr.Image().Pix) }

func TestMotionDialogInterruptionFocusAndIdle(t *testing.T) {
	m := &motionFixture{}
	tr := ui.NewTester(m.view, 840, 560)
	tr.Key(0, ui.KeyEnter)
	if !m.open || !tr.Focused("关闭弹窗") {
		t.Fatal("opening did not immediately move focus into the dialog")
	}
	start := pixels(tr)
	tr.Advance(80 * time.Millisecond)
	middle := pixels(tr)
	tr.Advance(400 * time.Millisecond)
	if bytes.Equal(start, middle) || bytes.Equal(middle, pixels(tr)) {
		t.Fatal("dialog did not render distinct initial, intermediate and settled frames")
	}
	tr.Key(0, ui.KeyEscape)
	if m.open || !tr.Focused("打开弹窗") {
		t.Fatalf("closing did not restore focus: open=%v, closeFocused=%v, texts=%v", m.open, tr.Focused("关闭弹窗"), tr.Texts())
	}
	// Reopen while the old exit copy is still visible, then close the new
	// dialog before its entrance finishes. Neither copy may intercept input.
	tr.Key(0, ui.KeyEnter)
	if !m.open || !tr.Focused("关闭弹窗") {
		t.Fatal("an exiting copy blocked reopening")
	}
	tr.Advance(40 * time.Millisecond)
	tr.Key(0, ui.KeyEscape)
	tr.Advance(time.Second)
	if m.open || !tr.Focused("打开弹窗") || tr.FramePending() {
		t.Fatal("interrupted dialog retained focus or kept drawing at rest")
	}
	settled := pixels(tr)
	tr.Advance(time.Second)
	if !bytes.Equal(settled, pixels(tr)) {
		t.Fatal("closed dialog continued to move")
	}
}

func TestMotionControlsAndReducedMotion(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		t.Run(fmt.Sprint(reduced), func(t *testing.T) {
			m := &motionFixture{}
			tr := ui.NewTester(m.view, 840, 560)
			tr.SetPreferences(ui.Preferences{ReduceMotion: reduced})
			before := pixels(tr)
			if err := tr.Click("测试开关"); err != nil {
				t.Fatal(err)
			}
			if !m.on {
				t.Fatal("switch delayed its actual value until animation completion")
			}
			if reduced && (tr.FramePending() || bytes.Equal(before, pixels(tr))) {
				t.Fatal("Reduce Motion did not show the new switch state immediately")
			}
			tr.Advance(50 * time.Millisecond)
			tr.Key(0, ui.KeySpace)
			if m.on {
				t.Fatal("keyboard could not reverse a moving switch")
			}
			if err := tr.Click("规则模式"); err != nil {
				t.Fatal(err)
			}
			tr.Key(0, ui.KeyRight)
			if m.selected != 1 {
				t.Fatal("segmented control lost arrow-key navigation")
			}
			m.disabled = true
			tr.Frame()
			if err := tr.Click("测试开关"); err != nil {
				t.Fatal(err)
			}
			if m.on {
				t.Fatal("disabled switch changed")
			}
			tr.Advance(time.Second)
			if tr.FramePending() {
				t.Fatal("controls kept drawing after settling")
			}
			if reduced {
				if err := tr.Click("打开弹窗"); err != nil {
					t.Fatal(err)
				}
				if !m.open || tr.FramePending() {
					t.Fatal("Reduce Motion left a dialog animation running")
				}
			}
		})
	}
}

func TestMotionAlertDoesNotDismissOutsideOrRepeatAction(t *testing.T) {
	m := &motionFixture{alert: true}
	tr := ui.NewTester(m.view, 840, 560)
	tr.ClickAt(4, 4)
	if !m.alert || m.confirmed != 0 {
		t.Fatal("outside click dismissed or confirmed an alert")
	}
	tr.Key(0, ui.KeyEscape)
	if m.alert || m.confirmed != 0 {
		t.Fatal("Escape confirmed the operation")
	}
	if !tr.Focused("打开弹窗") {
		t.Fatal("alert default action lost the original keyboard focus")
	}
	if err := tr.Click("打开确认"); err != nil {
		t.Fatal(err)
	}
	tr.Key(0, ui.KeyEnter)
	tr.Advance(time.Second)
	if m.alert || m.confirmed != 1 || tr.FramePending() {
		t.Fatalf("confirmation was lost, repeated or never settled: %+v", m)
	}
}

func TestMotionChartSettlesBetweenSamples(t *testing.T) {
	a := &App{}
	hist := []sample{{down: 12800}, {down: 6400}, {down: 19200}, {down: 25600}}
	tr := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme(false))
		ui.Column(c).Fill().Padding(24).Background(c.Theme().Background).Children(func() { a.chart(c, hist) })
	}, 640, 200)
	before := pixels(tr)
	hist = []sample{{down: 6400}, {down: 19200}, {down: 25600}, {down: 45000, up: 12000}}
	tr.Frame()
	tr.Advance(100 * time.Millisecond)
	middle := pixels(tr)
	tr.Advance(400 * time.Millisecond)
	if bytes.Equal(before, middle) || bytes.Equal(middle, pixels(tr)) || tr.FramePending() {
		t.Fatal("chart did not interpolate and settle between real sample changes")
	}
}

func TestMotionChartRespectsReducedMotionAndIdleTraffic(t *testing.T) {
	for _, dark := range []bool{false, true} {
		hist := []sample{{down: 12800}, {up: 6400}, {down: 19200}, {down: 25600}}
		a := &App{}
		tr := ui.NewTester(func(c *ui.Context) {
			c.SetTheme(theme(dark))
			ui.Column(c).Fill().Padding(24).Background(c.Theme().Background).Children(func() { a.chart(c, hist) })
		}, 640, 200)
		tr.Advance(time.Second)
		hist = []sample{{down: 25600}, {up: 32000}, {down: 72000}, {up: 24000}}
		tr.Frame()
		tr.Advance(100 * time.Millisecond)
		tr.SetPreferences(ui.Preferences{ReduceMotion: true})
		settled := pixels(tr)
		tr.Advance(time.Second)
		if tr.FramePending() || !bytes.Equal(settled, pixels(tr)) {
			t.Fatal("reduced motion left a traffic animation running")
		}
		hist = make([]sample, 4)
		tr.Frame()
		idle := pixels(tr)
		tr.Advance(time.Second)
		if tr.FramePending() || !bytes.Equal(idle, pixels(tr)) {
			t.Fatal("zero traffic generated decorative movement")
		}
	}
}

func TestMotionOverviewTrafficFrames(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, size := range [][2]int{{1060, 720}, {840, 560}} {
			t.Run(fmt.Sprintf("%v-%dx%d", dark, size[0], size[1]), func(t *testing.T) {
				a := subscriptionTestApp(t)
				a.booted, a.ticking, a.page = true, true, "overview"
				a.prefs.Appearance = AppearanceLight
				if dark {
					a.prefs.Appearance = AppearanceDark
				}
				fixtureOverviewTraffic(a)
				a.prefs.CloseOnSwitch = false
				tr := ui.NewTester(a.View, size[0], size[1])
				tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 88})
				tr.Advance(time.Second)
				capture := func(stage string) {
					if dir := os.Getenv("GOPURR_MOTION_FRAMES"); dir != "" {
						if err := os.MkdirAll(dir, 0o755); err != nil {
							t.Fatal(err)
						}
						f, err := os.Create(filepath.Join(dir, fmt.Sprintf("traffic-%v-%dx%d-%s.png", dark, size[0], size[1], stage)))
						if err != nil {
							t.Fatal(err)
						}
						defer f.Close()
						if err := png.Encode(f, tr.Image()); err != nil {
							t.Fatal(err)
						}
					}
				}
				capture("000")
				a.mu.Lock()
				a.hist = append(a.hist[1:], sample{down: 1400 * 1024, up: 360 * 1024})
				a.last.DownNow, a.last.UpNow = 1400*1024, 360*1024
				a.mu.Unlock()
				tr.Frame()
				for elapsed := 40; elapsed <= 400; elapsed += 40 {
					tr.Advance(40 * time.Millisecond)
					capture(fmt.Sprintf("%03d", elapsed))
				}
				tr.Advance(200 * time.Millisecond)
				capture("600")
				if tr.FramePending() {
					t.Fatal("overview effects kept requesting frames after sampling settled")
				}
				if err := tr.Click("切换节点"); err != nil {
					t.Fatal(err)
				}
				tr.Advance(300 * time.Millisecond)
				if err := tr.Click("选择 日本 02"); err != nil {
					t.Fatal(err)
				}
				tr.Frame()
				capture("route-000")
				// The picker's independent 160 ms exit has finished; the route's
				// 280 ms handoff is still moving when Reduce Motion is enabled.
				tr.Advance(180 * time.Millisecond)
				capture("route-180")
				tr.SetPreferences(ui.Preferences{ReduceMotion: true})
				tr.Frame()
				capture("route-end")
				if tr.FramePending() || !tr.HasText("日本 02") || a.showOverviewNodes {
					t.Fatalf("route handoff did not settle or respect reduced motion: pending=%v, newExit=%v, picker=%v", tr.FramePending(), tr.HasText("日本 02"), a.showOverviewNodes)
				}
			})
		}
	}
}

// Set GOPURR_MOTION_FRAMES to a directory to inspect actual native frames at
// both supported window sizes and appearances, including interrupted exits.
func TestMotionOverviewFrames(t *testing.T) {
	a := subscriptionTestApp(t)
	a.booted, a.ticking, a.page = true, true, "overview"
	a.prefs.CloseOnSwitch = false
	for _, dark := range []bool{false, true} {
		for _, size := range [][2]int{{1060, 720}, {840, 560}} {
			name := fmt.Sprintf("%v-%dx%d", dark, size[0], size[1])
			t.Run(name, func(t *testing.T) {
				a.showOverviewNodes = false
				a.prefs.Appearance = AppearanceLight
				if dark {
					a.prefs.Appearance = AppearanceDark
				}
				tr := ui.NewTester(a.View, size[0], size[1])
				tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 88})
				tr.Advance(time.Second)
				capture := func(stage string) {
					if dir := os.Getenv("GOPURR_MOTION_FRAMES"); dir != "" {
						if err := os.MkdirAll(dir, 0o755); err != nil {
							t.Fatal(err)
						}
						f, err := os.Create(filepath.Join(dir, name+"-"+stage+".png"))
						if err != nil {
							t.Fatal(err)
						}
						err = png.Encode(f, tr.Image())
						closeErr := f.Close()
						if err != nil {
							t.Fatal(err)
						}
						if closeErr != nil {
							t.Fatal(closeErr)
						}
					}
				}
				if err := tr.Click("切换节点"); err != nil {
					t.Fatal(err)
				}
				capture("open-000")
				tr.Advance(60 * time.Millisecond)
				capture("open-060")
				tr.Advance(80 * time.Millisecond)
				capture("open-140")
				tr.Advance(160 * time.Millisecond)
				capture("open-300")
				tr.Key(0, ui.KeyEscape)
				capture("close-000")
				tr.Advance(60 * time.Millisecond)
				capture("close-060")
				tr.Advance(120 * time.Millisecond)
				capture("close-180")
				if a.showOverviewNodes {
					t.Fatal("Escape failed in the real overview")
				}
			})
		}
	}
}
