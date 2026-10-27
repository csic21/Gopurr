package app

import (
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"gopurr/internal/patch"
)

func TestDockPreferences(t *testing.T) {
	path := prefsPathFor(t.TempDir())
	for _, value := range dockVisibilities {
		a := &App{prefs: defaultPrefs(), prefsPath: path}
		p := a.Prefs()
		p.DockVisibility, p.LightMode = value, true
		if err := a.UpdatePrefs(p); err != nil {
			t.Fatal(err)
		}
		reloaded := &App{prefsPath: path}
		reloaded.loadPrefs()
		if got := reloaded.Prefs(); got != p {
			t.Fatalf("saved %+v, reloaded %+v", p, got)
		}
	}
	for _, body := range []string{
		`{"lightMode":true}`,
		`{"dockVisibility":"unknown","lightMode":true}`,
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		a := &App{prefsPath: path}
		a.loadPrefs()
		if p := a.Prefs(); p.DockVisibility != DockShown || !p.LightMode {
			t.Fatalf("migration lost preferences: %+v", p)
		}
		called := 0
		a.SetPrefsHook(func(Prefs) { called++ })
		p := a.Prefs()
		p.DockVisibility = "invalid"
		if err := a.UpdatePrefs(p); err == nil || a.Prefs().DockVisibility != DockShown || called != 1 {
			t.Fatal("invalid Dock preference was published")
		}
	}
}

func TestDockVisibilityIndependentOfLightMode(t *testing.T) {
	for _, light := range []bool{false, true} {
		for _, visible := range []bool{false, true} {
			for _, mode := range dockVisibilities {
				p := Prefs{DockVisibility: mode, LightMode: light}
				want := mode == DockShown || (mode == DockFollowWindow && visible)
				if got := p.DockVisible(visible); got != want {
					t.Errorf("mode=%s, light=%v, window=%v: Dock=%v, want %v", mode, light, visible, got, want)
				}
			}
		}
	}
}

func TestDockSettingsInteraction(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Dock settings are macOS only")
	}
	for _, size := range [][2]int{{1060, 720}, {840, 560}} {
		a := &App{prefs: defaultPrefs(), prefsPath: prefsPathFor(t.TempDir()), ov: patch.Default()}
		var applied Prefs
		a.SetPrefsHook(func(p Prefs) { applied = p })
		tr := ui.NewTester(func(c *ui.Context) {
			c.SetTheme(theme(c.Theme().Dark))
			ui.Row(c).Fill().Children(func() {
				ui.Box(c).Width(204).FillHeight()
				ui.Column(c).Fill().Padding(24).Gap(16).Children(func() { a.settingsPage(c) })
			})
		}, size[0], size[1])
		tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 88})
		for _, step := range []struct{ label, value string }{
			{"隐藏", DockHidden}, {"跟随窗口", DockFollowWindow}, {"显示", DockShown},
		} {
			box, ok := tr.Find(step.label)
			if !ok || box.X < 228 || box.X+box.W > float32(size[0]-24) || box.Y < 38 || box.Y+box.H > float32(size[1]-24) {
				t.Fatalf("%q is clipped at %dx%d: %+v", step.label, size[0], size[1], box)
			}
			// A background subscription update must survive the UI edit.
			stamp := time.Now().Truncate(time.Second)
			if err := a.changePrefs(func(p *Prefs) { p.LastAutoUpdate = stamp }); err != nil {
				t.Fatal(err)
			}
			if err := tr.Click(step.label); err != nil {
				t.Fatal(err)
			}
			tr.Frame()
			if applied.DockVisibility != step.value || !a.Prefs().LastAutoUpdate.Equal(stamp) {
				t.Fatalf("click %s did not merge and apply preference: %+v", step.label, a.Prefs())
			}
			reloaded := &App{prefsPath: a.prefsPath}
			reloaded.loadPrefs()
			if reloaded.Prefs().DockVisibility != step.value || !tr.HasText(dockVisibilityDescription(step.value)) {
				t.Fatalf("click %s did not persist or update its explanation", step.label)
			}
		}
	}
}

func TestDockSaveFailureKeepsPreferenceAndHook(t *testing.T) {
	a := &App{prefs: defaultPrefs(), prefsPath: t.TempDir()}
	called := 0
	a.SetPrefsHook(func(Prefs) { called++ })
	a.initSettings()
	a.dockIdx = dockVisibilityIndex(DockHidden)
	a.reconcileSettings()
	if a.Prefs().DockVisibility != DockShown || a.dockIdx != dockVisibilityIndex(DockShown) || a.generalSettingsError == "" || called != 1 {
		t.Fatalf("failed save changed Dock preference: %+v, hooks=%d", a.Prefs(), called)
	}
}
