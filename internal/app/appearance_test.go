package app

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAppearancePreferences(t *testing.T) {
	path := prefsPathFor(t.TempDir())
	for _, value := range appearances {
		a := &App{prefs: defaultPrefs(), prefsPath: path}
		p := a.Prefs()
		p.Appearance = value
		if err := a.UpdatePrefs(p); err != nil {
			t.Fatal(err)
		}
		reloaded := &App{prefsPath: path}
		reloaded.loadPrefs()
		if got := reloaded.Prefs().Appearance; got != value {
			t.Fatalf("saved %q, reloaded %q", value, got)
		}
	}
	for _, body := range []string{`{"lightMode":true}`, `{"appearance":"unknown","lightMode":true}`} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		a := &App{prefsPath: path}
		a.loadPrefs()
		if p := a.Prefs(); p.Appearance != AppearanceSystem || !p.LightMode {
			t.Fatalf("migration lost preferences: %+v", p)
		}
		p := a.Prefs()
		p.Appearance = "invalid"
		if err := a.UpdatePrefs(p); err == nil || a.Prefs().Appearance != AppearanceSystem {
			t.Fatal("invalid appearance was published")
		}
	}
}

func TestAppearanceSwitchesAndFollowsSystem(t *testing.T) {
	dir, err := os.MkdirTemp("", "gopurr-theme-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("GOPURR_HOME", dir)
	a := New()
	secret, err := os.ReadFile(filepath.Join(a.dir, "core.secret"))
	if err != nil {
		t.Fatal(err)
	}
	startFakeCore(t, a.dir, string(secret))
	// Theme interaction must not touch the system proxy or start a real core.
	a.booted, a.ticking, a.page = true, true, "settings"
	var dark bool
	tr := ui.NewTester(func(c *ui.Context) {
		a.View(c)
		dark = c.Theme().Dark
	}, 840, 560)
	tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 88})
	check := func(want bool) {
		t.Helper()
		tr.Frame()
		if dark != want {
			t.Fatalf("appearance=%q: dark=%v, want %v", a.Prefs().Appearance, dark, want)
		}
		// The native title band uses the same colors as the content below it.
		img := tr.Image()
		if img.At(400, 8) != img.At(400, 40) || img.At(100, 8) != img.At(100, 40) {
			t.Fatal("title bar does not match the content palette")
		}
	}
	tr.SetDark(false)
	check(false)
	tr.SetDark(true)
	check(true)
	for _, step := range []struct {
		label string
		dark  bool
		value string
	}{
		{"浅色", false, AppearanceLight},
		{"深色", true, AppearanceDark},
		{"跟随系统", true, AppearanceSystem},
	} {
		if step.value == AppearanceSystem {
			r, ok := tr.Find("外观主题")
			if !ok {
				t.Fatal("theme selector missing")
			}
			tr.ClickAt(r.X+15, r.Y+r.H/2)
		} else if err := tr.Click(step.label); err != nil {
			t.Fatal(err)
		}
		check(step.dark)
		if a.Prefs().Appearance != step.value {
			t.Fatalf("click %s did not save preference", step.label)
		}
		if step.value != AppearanceSystem {
			tr.SetDark(!step.dark)
			check(step.dark)
			tr.SetDark(true)
		}
	}
	tr.SetDark(false)
	check(false)
	reloaded := &App{prefsPath: a.prefsPath}
	reloaded.loadPrefs()
	if reloaded.Prefs().Appearance != AppearanceSystem {
		t.Fatal("last theme selection was not persisted")
	}
	a.page = "overview"
	tr.Frame()
	for _, label := range []string{"系统代理开关", "TUN 模式", "安装服务…"} {
		r, ok := tr.Find(label)
		if !ok || r.Y < 38 || r.Y+r.H > 544 || r.X+r.W > 816 {
			t.Fatalf("overview control %q is clipped at 840×560: %+v", label, r)
		}
	}
}

func TestAppearanceSaveFailureKeepsCurrentTheme(t *testing.T) {
	a := &App{prefs: defaultPrefs(), prefsPath: t.TempDir()}
	a.initSettings()
	a.appearanceIdx = appearanceIndex(AppearanceDark)
	a.reconcileSettings()
	if a.Prefs().Appearance != AppearanceSystem || a.appearanceIdx != 0 || a.generalSettingsError == "" {
		t.Fatalf("failed save changed appearance: %+v", a.Prefs())
	}
}

func TestAppearanceTextContrast(t *testing.T) {
	luminance := func(c ui.Color) float64 {
		linear := func(v uint8) float64 {
			s := float64(v) / 255
			if s <= 0.04045 {
				return s / 12.92
			}
			return math.Pow((s+0.055)/1.055, 2.4)
		}
		return 0.2126*linear(c.R) + 0.7152*linear(c.G) + 0.0722*linear(c.B)
	}
	contrast := func(fg, bg ui.Color) float64 {
		a, b := luminance(fg), luminance(bg)
		return (math.Max(a, b) + 0.05) / (math.Min(a, b) + 0.05)
	}
	for _, dark := range []bool{false, true} {
		p := paletteForDark(dark)
		for _, fg := range []ui.Color{p.Text, p.Muted, p.Faint, p.Accent, p.Up, p.Down, p.Danger, p.Warn, p.Success} {
			for _, bg := range []ui.Color{p.Bg, p.Side, p.Surface, p.Hover, p.Selected} {
				if ratio := contrast(fg, bg); ratio < 4.5 {
					t.Errorf("dark=%v: text %v on %v has contrast %.2f", dark, fg, bg, ratio)
				}
			}
		}
		for _, bg := range []ui.Color{p.Accent, p.AccentHover, p.AccentPressed} {
			if ratio := contrast(p.AccentText, bg); ratio < 4.5 {
				t.Errorf("dark=%v: button text has contrast %.2f", dark, ratio)
			}
		}
	}
}
