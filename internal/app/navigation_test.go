package app

import (
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestNavigationLayouts(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, size := range [][2]int{{1060, 720}, {840, 560}} {
			t.Run(fmt.Sprintf("%v-%dx%d", dark, size[0], size[1]), func(t *testing.T) {
				a := subscriptionTestApp(t)
				a.booted, a.ticking = true, true
				a.prefs.Appearance = AppearanceLight
				if dark {
					a.prefs.Appearance = AppearanceDark
				}
				tr := ui.NewTester(a.View, size[0], size[1])
				tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 88})
				tr.Advance(time.Second)
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
				toggle := find("收起侧栏")
				start := find("系统代理开关")
				contentStart := find("连接状态与实时网络活动")
				click("收起侧栏")
				tr.Advance(90 * time.Millisecond)
				middle := find("系统代理开关")
				contentMiddle := find("连接状态与实时网络活动")
				if contentMiddle.X <= 20 || contentMiddle.X >= contentStart.X {
					t.Fatal("sidebar jumped instead of expanding the content smoothly")
				}
				saveSelectionFrame(t, fmt.Sprintf("navigation-%v-%d-middle", dark, size[0]), tr.Image())
				// Reverse before settling: the width must resume from this frame.
				click("展开侧栏")
				if r := find("系统代理开关"); math.Abs(float64(r.X-middle.X)) > 1 {
					t.Fatal("reversing the sidebar caused a layout jump")
				}
				click("收起侧栏")
				tr.Advance(time.Second)
				if !a.Prefs().SidebarCollapsed || find("展开侧栏") != toggle {
					t.Fatal("collapse was lost or its toggle moved")
				}
				nav, brand := find("页面导航"), find("Gopurr")
				if math.Abs(float64(nav.X+nav.W/2)-float64(size[0])/2) > 1 {
					t.Fatalf("navigation is not centered on the window: %+v", nav)
				}
				if gap := toggle.X - (brand.X + brand.W); gap < 4 || gap > 8 {
					t.Fatalf("brand and toggle spacing is not compact: %v", gap)
				}
				if r := find("连接状态与实时网络活动"); contentStart.X-r.X != sidebarWidth {
					t.Fatal("collapsed sidebar did not release its full content width")
				}
				// Right-aligned controls stay put while the left edge moves.
				if middle.X != start.X {
					t.Fatal("right-aligned controls drifted during sidebar motion")
				}
				for _, label := range []string{"系统代理开关", "TUN 模式", "概览DNS开关", "概览域名嗅探开关"} {
					r := find(label)
					if r.X < 20 || r.Y < 38 || r.X+r.W > float32(size[0]) || r.Y+r.H > float32(size[1]) {
						t.Fatalf("overview control %q clipped: %+v", label, r)
					}
				}
				for _, it := range navItems {
					r := find(it.label)
					if r.X < toggle.X+toggle.W || r.Y < 0 || r.Y+r.H > 38 || r.X+r.W > float32(size[0]-16) {
						t.Fatalf("navigation destination %q clipped: %+v", it.label, r)
					}
					click(it.label)
					if a.page != it.id {
						t.Fatalf("%q selected %q", it.label, a.page)
					}
				}
				click("订阅")
				tr.Key(0, ui.KeyRight)
				if a.page != "proxies" {
					t.Fatal("top navigation lost arrow-key selection")
				}
				tr.Advance(60 * time.Millisecond)
				saveSelectionFrame(t, fmt.Sprintf("navigation-%v-%d-selection", dark, size[0]), tr.Image())
				tr.Advance(time.Second)
				if tr.FramePending() {
					t.Fatal("navigation kept animating at rest")
				}
				// Reopening restores the same destination; layout choice survives
				// a fresh App just as it survives releasing the native window.
				if !New().Prefs().SidebarCollapsed {
					t.Fatal("navigation layout was not persisted")
				}
				tr.Key(ui.Cmd, ui.KeyBackslash)
				tr.Advance(time.Second)
				if a.Prefs().SidebarCollapsed || a.page != "proxies" {
					t.Fatal("keyboard expansion lost the current destination")
				}
				tr.SetPreferences(ui.Preferences{ReduceMotion: true})
				click("收起侧栏")
				if _, ok := tr.Find("页面导航"); !ok || tr.FramePending() {
					t.Fatal("reduced motion did not settle immediately")
				}
				tr.SetSize(840, 560)
				if r := find("设置"); r.X+r.W > 824 {
					t.Fatal("resize clipped the last navigation item")
				}
				tr.SetTitleBar(ui.TitleBar{Height: 38, Right: 138})
				if r := find("设置"); r.X+r.W > 702 {
					t.Fatal("top navigation overlaps right-hand window controls")
				}
			})
		}
	}
}

func TestNavigationPreservesDraftAndFailedSave(t *testing.T) {
	a := subscriptionTestApp(t)
	a.booted, a.ticking, a.page = true, true, "rules"
	a.initExtensionDrafts()
	a.extensionTab, a.mergeYAMLDraft = 3, "# unsaved draft\nrules: []\n"
	tr := ui.NewTester(a.View, 840, 560)
	tr.SetPreferences(ui.Preferences{ReduceMotion: true})
	if err := tr.Click("收起侧栏"); err != nil {
		t.Fatal(err)
	}
	if a.page != "rules" || a.extensionTab != 3 || a.mergeYAMLDraft != "# unsaved draft\nrules: []\n" {
		t.Fatal("navigation layout discarded the editor draft")
	}
	if !a.Prefs().SidebarCollapsed {
		t.Fatal("sidebar toggle did not collapse the layout")
	}
	a.showDeleteSub, a.deleteSubName = true, "测试配置"
	tr.Frame()
	if _, ok := tr.Find("删除订阅？"); !ok {
		t.Fatal("confirmation dialog was not opened")
	}
	tr.Key(ui.Cmd, ui.KeyBackslash)
	if !a.showDeleteSub || !a.Prefs().SidebarCollapsed {
		t.Fatalf("sidebar shortcut escaped the modal's focus scope: dialog=%v collapsed=%v", a.showDeleteSub, a.Prefs().SidebarCollapsed)
	}
	tr.Key(0, ui.KeyEscape)
	// A regular file as the parent is an unambiguous write failure, even when
	// the writer creates missing directories.
	parent := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(parent, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.prefsPath = filepath.Join(parent, "prefs.json")
	if err := tr.Click("展开侧栏"); err != nil {
		t.Fatal(err)
	}
	if !a.Prefs().SidebarCollapsed || a.notice == "" || !tr.HasText(a.notice) {
		t.Fatal("failed preference save hid the active navigation or its error")
	}
}

// Observe the painted selection travelling across both axes, including a
// reversal. This catches a clipped flight or an immediately visible top row
// even when the underlying controls have correct final layout bounds.
func TestNavigationMorphPaint(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, size := range [][2]int{{1060, 720}, {840, 560}} {
			t.Run(fmt.Sprintf("%v-%d", dark, size[0]), func(t *testing.T) {
				a := subscriptionTestApp(t)
				a.booted, a.ticking, a.page = true, true, "logs"
				a.prefs.Appearance = AppearanceLight
				if dark {
					a.prefs.Appearance = AppearanceDark
				}
				tr := ui.NewTester(a.View, size[0], size[1])
				tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 88})
				tr.Advance(time.Second)
				painted := func() image.Rectangle {
					img, fill := tr.Image(), paletteForDark(dark).Selected
					// Ignore isolated antialiasing pixels that happen to match
					// the selection color; keep its largest connected surface.
					bounds, count := image.Rectangle{}, 0
					seen := make(map[image.Point]bool)
					matches := func(pt image.Point) bool {
						p := img.RGBAAt(pt.X, pt.Y)
						return pt.In(img.Bounds()) && p.R == fill.R && p.G == fill.G && p.B == fill.B
					}
					for y := 0; y < img.Bounds().Dy(); y++ {
						for x := 0; x < img.Bounds().Dx(); x++ {
							pt := image.Pt(x, y)
							if seen[pt] || !matches(pt) {
								continue
							}
							queue, component := []image.Point{pt}, image.Rectangle{}
							seen[pt] = true
							for at := 0; at < len(queue); at++ {
								p := queue[at]
								component = component.Union(image.Rect(p.X, p.Y, p.X+1, p.Y+1))
								for _, d := range []image.Point{{X: -1}, {X: 1}, {Y: -1}, {Y: 1}} {
									next := p.Add(d)
									if !seen[next] && matches(next) {
										seen[next] = true
										queue = append(queue, next)
									}
								}
							}
							if len(queue) > count {
								bounds, count = component, len(queue)
							}
						}
					}
					if bounds.Empty() {
						t.Fatal("moving navigation was clipped or disappeared")
					}
					return bounds
				}
				start := painted()
				tr.Key(ui.Cmd, ui.KeyBackslash)
				tr.Advance(160 * time.Millisecond)
				middle := painted()
				target, _ := tr.Find("日志")
				if middle.Min.X <= start.Max.X || middle.Min.X >= int(target.X) ||
					middle.Min.Y >= start.Min.Y || middle.Min.Y <= int(target.Y+target.H) {
					t.Fatalf("navigation did not fan from a column into a row: %v -> %v -> %+v", start, middle, target)
				}
				if err := tr.Click("代理"); err != nil {
					t.Fatal(err)
				}
				if a.page != "logs" {
					t.Fatal("invisible destination accepted a click during the flight")
				}
				middle = painted() // Click advances the test clock's pointer events.
				tr.Key(ui.Cmd, ui.KeyBackslash)
				if reversed := painted(); reversed != middle {
					t.Fatalf("reversal jumped: %v -> %v", middle, reversed)
				}
				tr.Advance(100 * time.Millisecond)
				back := painted()
				if back.Min.X >= middle.Min.X || back.Min.Y <= middle.Min.Y {
					t.Fatal("reversal did not return toward the sidebar")
				}
				tr.Key(ui.Cmd, ui.KeyBackslash)
				tr.SetPreferences(ui.Preferences{ReduceMotion: true})
				end := painted()
				if math.Abs(float64(end.Min.X)-float64(target.X)) > 1 || end.Max.Y > 38 || tr.FramePending() {
					t.Fatalf("reduced motion did not finish the in-flight navigation: %v", end)
				}
			})
		}
	}
}

func TestNavigationMorphFrames(t *testing.T) {
	if os.Getenv("GOPURR_SELECTION_FRAMES") == "" {
		t.Skip("optional animation review frames")
	}
	for _, dark := range []bool{false, true} {
		for _, size := range [][2]int{{1060, 720}, {840, 560}} {
			a := subscriptionTestApp(t)
			a.booted, a.ticking = true, true
			a.prefs.Appearance = AppearanceLight
			if dark {
				a.prefs.Appearance = AppearanceDark
			}
			tr := ui.NewTester(a.View, size[0], size[1])
			tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 88})
			tr.Advance(time.Second)
			for _, direction := range []string{"collapse", "expand"} {
				tr.Key(ui.Cmd, ui.KeyBackslash)
				for ms := 0; ms <= 360; ms += 40 {
					if ms > 0 {
						tr.Advance(40 * time.Millisecond)
					}
					saveSelectionFrame(t, fmt.Sprintf("morph-%v-%d-%s-%03d", dark, size[0], direction, ms), tr.Image())
				}
				tr.Advance(time.Second)
			}
		}
	}
}
