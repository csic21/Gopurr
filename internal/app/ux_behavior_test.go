package app

import (
	ui "gopurr/internal/appui"
	"gopurr/internal/sysproxy"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProxyStartupMigrationAndLiveStateIndependence(t *testing.T) {
	for _, remembered := range []bool{false, true} {
		dir := t.TempDir()
		path := filepath.Join(dir, "prefs.json")
		raw := `{"restoreSystemProxy":false}`
		if remembered {
			raw = `{"restoreSystemProxy":true}`
		}
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		a := &App{prefsPath: path}
		a.loadPrefs()
		if a.Prefs().SystemProxyStartup != proxyStartupLast || a.Prefs().systemProxyOnStartup() != remembered {
			t.Fatal("legacy behavior changed")
		}
		for _, policy := range []string{proxyStartupLast, proxyStartupOn, proxyStartupOff} {
			if err := a.changePrefs(func(p *Prefs) { p.SystemProxyStartup = policy }); err != nil {
				t.Fatal(err)
			}
			for _, on := range []bool{true, false} {
				a.persistRestoreSystemProxy(on)
				p := a.Prefs()
				want := policy == proxyStartupOn || (policy == proxyStartupLast && on)
				if p.SystemProxyStartup != policy || p.RestoreSystemProxy != on || p.systemProxyOnStartup() != want {
					t.Fatalf("live switch changed policy: %+v", p)
				}
			}
		}
	}
}

func TestFeatureSaveDoesNotBlockAndCommitsOnUIThread(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	a, _ := newSettingsTestApp(t, func(raw string) bool {
		if strings.Contains(raw, "sniffer:") {
			close(entered)
			<-release
		}
		return false
	})
	a.resetSnifferDraft()
	a.snifferDraft.Source, a.snifferDraft.Enable = 1, true
	original := runOnMain
	completed := make(chan func(), 1)
	runOnMain = func(fn func()) { completed <- fn }
	t.Cleanup(func() { runOnMain = original })
	a.saveSnifferSettings()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("reload did not start")
	}
	if !busyNetwork(a) || a.ov.Sniffer != nil {
		t.Fatal("published unaccepted state")
	}
	if a.beginProfileWork() {
		t.Fatal("concurrent profile write was allowed")
	}
	release <- struct{}{}
	select {
	case done := <-completed:
		done()
	case <-time.After(3 * time.Second):
		t.Fatal("completion missing")
	}
	if busyNetwork(a) || a.ov.Sniffer == nil || a.snifferDraft != a.snifferBase || a.snifferError != "" {
		t.Fatal("completion did not commit saved draft")
	}
}

func TestProxyEditorEscapeProtectsDraftAndDefaultsToKeepEditing(t *testing.T) {
	t.Skip("legacy window harness was removed with mygo; the native host draws the desktop")
	a, _ := newSettingsTestApp(t, nil)
	a.booted, a.ticking = true, true
	fixtureProxyEditor(t, a)
	a.proxyEditorNodes["香港 01"].Name = "unsaved node"
	view := ui.NewTester(a.View, 840, 560)
	view.SetPreferences(ui.Preferences{ReduceMotion: true})
	view.Frame()
	view.Key(0, ui.KeyEscape)
	if !a.showProxyEditor || !a.proxyEditorDiscard {
		t.Fatal("Escape discarded draft")
	}
	view.Key(0, ui.KeyEnter)
	if !a.showProxyEditor || a.proxyEditorDiscard || a.proxyEditorNodes["香港 01"].Name != "unsaved node" {
		t.Fatal("default confirmation lost draft")
	}
	a.requestCloseProxyEditor()
	view.Frame()
	if err := view.Click("放弃修改"); err != nil {
		t.Fatal(err)
	}
	if a.showProxyEditor {
		t.Fatal("confirmed close did not close")
	}
}

func TestSuccessFeedbackDoesNotMoveControlsAndExpires(t *testing.T) {
	t.Skip("legacy window harness was removed with mygo; the native host draws the desktop")
	a, _ := newSettingsTestApp(t, nil)
	a.booted, a.ticking, a.page = true, true, "overview"
	a.effectiveFeatures()
	view := ui.NewTester(a.View, 840, 560)
	view.SetPreferences(ui.Preferences{ReduceMotion: true})
	view.SetTitleBar(ui.TitleBar{Height: 38, Left: 80})
	view.Frame()
	before, ok := view.Find("概览域名嗅探开关")
	if !ok {
		t.Fatal("missing sniffer control")
	}
	a.successNotice("订阅已启用")
	view.Frame()
	after, ok := view.Find("概览域名嗅探开关")
	if !ok || before != after || after.Y+after.H > 536 {
		t.Fatal("feedback moved or clipped controls")
	}
	view.Advance(5 * time.Second)
	if a.notice != "" {
		t.Fatal("success did not expire")
	}
	a.errorNotice("测试失败")
	view.Frame()
	view.Advance(5 * time.Second)
	if a.notice == "" {
		t.Fatal("error disappeared before dismissal")
	}
}

func TestFirstWindowCloseExplainsBackgroundAndRemembersChoice(t *testing.T) {
	t.Skip("legacy window harness was removed with mygo; the native host draws the desktop")
	a := &App{prefs: defaultPrefs(), prefsPath: filepath.Join(t.TempDir(), "prefs.json")}
	closed := 0
	a.RequestWindowClose(func() { closed++ })
	view := ui.NewTester(a.windowCloseDialog, 840, 560)
	view.Key(0, ui.KeyEscape)
	if closed != 0 || a.Prefs().CloseToTrayAcknowledged {
		t.Fatal("dismissal hid the window")
	}
	a.RequestWindowClose(func() { closed++ })
	view.Frame()
	if err := view.Click("继续后台运行"); err != nil {
		t.Fatal(err)
	}
	if closed != 1 || !a.Prefs().CloseToTrayAcknowledged {
		t.Fatal("close choice not saved")
	}
	a.RequestWindowClose(func() { closed++ })
	if closed != 2 || a.windowClose.show {
		t.Fatal("repeat close prompted again")
	}
}

func TestStartupPolicyAppliesWithoutChangingRememberedChoice(t *testing.T) {
	for _, policy := range []string{proxyStartupOn, proxyStartupOff} {
		t.Run(policy, func(t *testing.T) {
			a, _ := newSettingsTestApp(t, nil)
			enabled, restored := 0, 0
			a.proxy = proxyControllerFixture{
				enable:  func(int, *sysproxy.State) error { enabled++; return nil },
				restore: func(int, *sysproxy.State) error { restored++; return nil },
			}
			a.prefs.SystemProxyStartup = policy
			a.sysApplied, a.sysOn = policy == proxyStartupOff, policy == proxyStartupOff
			a.finishKernelBoot()
			waitProxyChange(t, a)
			if a.Prefs().SystemProxyStartup != policy || a.Prefs().RestoreSystemProxy {
				t.Fatal("boot rewrote remembered user choice")
			}
			if policy == proxyStartupOn && (enabled != 1 || !a.sysApplied) {
				t.Fatal("always on did not enable proxy")
			}
			if policy == proxyStartupOff && (restored != 1 || a.sysApplied) {
				t.Fatal("always off left our proxy applied")
			}
		})
	}
}
