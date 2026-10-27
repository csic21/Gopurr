package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"gopurr/internal/update"
)

type fixtureUpdateBackend struct {
	phase     update.Phase
	installed *atomic.Int32
}

func (fixtureUpdateBackend) Enabled() bool { return true }
func (b fixtureUpdateBackend) Check(context.Context) (*update.Release, error) {
	if b.phase == update.Failed {
		return nil, errors.New("暂时无法连接 GitHub，请检查网络后重试。")
	}
	if b.phase == update.Current {
		return nil, nil
	}
	return &update.Release{Version: "0.2.0", Notes: "更新说明\n\n• 支持 Windows 与 macOS Apple Silicon 安装\n• 软件更新支持签名校验、下载进度与重试\n• 保留现有订阅、规则与个人设置", Install: func(ctx context.Context, progress func(int64, int64)) error {
		if b.installed != nil {
			b.installed.Add(1)
		}
		progress(18<<20, 48<<20)
		if b.phase == update.Downloading {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}}, nil
}

func fixtureSoftwareUpdate(t *testing.T, a *App, phase update.Phase) {
	t.Helper()
	if a.softwareUpdates != nil {
		a.softwareUpdates.Close()
	}
	a.softwareUpdates = update.New(fixtureUpdateBackend{phase: phase}, nil)
	t.Cleanup(a.softwareUpdates.Close)
	a.softwareUpdates.Check(true)
	want := phase
	if phase == update.Downloading || phase == update.Ready {
		want = update.Available
	}
	waitSoftwareUpdate(t, a, want)
	if phase == update.Downloading || phase == update.Ready {
		a.softwareUpdates.Install()
		waitSoftwareUpdate(t, a, phase)
	}
}

func waitSoftwareUpdate(t *testing.T, a *App, phase update.Phase) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if a.softwareUpdates.Snapshot().Phase == phase {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("update phase = %s, want %s", a.softwareUpdates.Snapshot().Phase, phase)
}

func TestSoftwareUpdateRequiresInstallConfirmation(t *testing.T) {
	a, _ := newSettingsTestApp(t, nil)
	a.booted, a.ticking = true, true
	var installs atomic.Int32
	a.softwareUpdates = update.New(fixtureUpdateBackend{phase: update.Ready, installed: &installs}, nil)
	defer a.softwareUpdates.Close()
	a.softwareUpdates.Check(true)
	waitSoftwareUpdate(t, a, update.Available)
	tr := ui.NewTester(a.View, 840, 560)
	tr.Frame()
	if installs.Load() != 0 {
		t.Fatal("checking installed an update")
	}
	if err := tr.Click("下载并安装"); err != nil {
		t.Fatal(err)
	}
	waitSoftwareUpdate(t, a, update.Ready)
	tr.Frame()
	if _, ok := tr.Find("立即重启"); !ok || installs.Load() != 1 {
		t.Fatal("installation did not expose explicit restart")
	}
	if err := tr.Click("稍后重启"); err != nil {
		t.Fatal(err)
	}
	if a.softwareUpdates.Snapshot().Visible {
		t.Fatal("postponing restart left the modal open")
	}
}

func TestSoftwareUpdatePreferencePersists(t *testing.T) {
	path := prefsPathFor(t.TempDir())
	a := &App{prefs: defaultPrefs(), prefsPath: path}
	if !a.Prefs().CheckAppUpdates {
		t.Fatal("automatic checks should default on")
	}
	if err := a.changePrefs(func(p *Prefs) { p.CheckAppUpdates = false }); err != nil {
		t.Fatal(err)
	}
	reloaded := &App{prefsPath: path}
	reloaded.loadPrefs()
	if reloaded.Prefs().CheckAppUpdates {
		t.Fatal("disabled automatic checks did not persist")
	}
}
