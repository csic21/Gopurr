package app

import (
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/sysproxy"
	"gopurr/internal/update"
)

func TestUpdateRestartStaysResponsiveAndRetriesCleanup(t *testing.T) {
	a, _ := newSettingsTestApp(t, nil)
	a.booted, a.ticking = true, true
	fixtureSoftwareUpdate(t, a, update.Ready)
	previous := &sysproxy.State{Enabled: true, Server: "previous.example", Port: 8080}
	a.prev, a.sysApplied, a.sysOn = previous, true, true
	a.savePrevProxy()
	entered, unblock := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-unblock:
		default:
			close(unblock)
		}
	})
	posted := make(chan func(), 2)
	var attempts, closes atomic.Int32
	a.proxy = proxyControllerFixture{
		restore: func(int, *sysproxy.State) error {
			if attempts.Add(1) == 1 {
				close(entered)
				<-unblock
				return errors.New("等待系统代理恢复操作超时，请稍后重试")
			}
			return nil
		},
		close: func() error { closes.Add(1); return nil },
	}
	completed, quitRequests := 0, 0
	var cleanupError error
	a.shutdown.quit = func() {
		quitRequests++
		a.beginShutdown(func(fn func()) { posted <- fn }, func(err error) { completed++; cleanupError = err })
	}
	tr := ui.NewTester(a.View, 840, 560)
	tr.Frame()
	if err := tr.Click("立即重启"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not start")
	}
	// Cleanup is blocked, but frames, keyboard input and repeated quit events
	// still finish. Escape must not expose controls that can change the proxy.
	tr.Frame()
	tr.Key(0, ui.KeyEscape)
	tr.Frame()
	if _, ok := tr.Find("正在重启…"); !ok {
		t.Fatal("restart progress is missing or dismissible")
	}
	_ = tr.Click("正在重启…")
	a.beginShutdown(func(fn func()) { posted <- fn }, func(error) { t.Error("duplicate quit completed") })
	if completed != 0 || attempts.Load() != 1 || quitRequests != 1 || a.ShutdownReady() || !a.RelaunchRequested() {
		t.Fatal("restart advanced or duplicated cleanup while restore was blocked")
	}
	close(unblock)
	runCompletion := func() {
		t.Helper()
		select {
		case fn := <-posted:
			fn()
		case <-time.After(2 * time.Second):
			t.Fatal("cleanup did not return to the UI")
		}
	}
	runCompletion()
	if completed != 1 || cleanupError == nil || a.ShuttingDown() || closes.Load() != 0 || a.prev != previous || !a.sysApplied || !a.k.Running() {
		t.Fatal("failed cleanup did not preserve the running app and recovery state")
	}
	if _, err := os.Stat(a.prevPath); err != nil {
		t.Fatal("failed cleanup discarded the recovery record")
	}
	tr.Frame()
	if err := tr.Click("重试重启"); err != nil {
		t.Fatal(err)
	}
	runCompletion()
	if completed != 2 || cleanupError != nil || !a.ShutdownReady() || !a.RelaunchRequested() || attempts.Load() != 2 || closes.Load() != 1 || a.sysApplied || a.prev != nil {
		t.Fatal("retry did not finish cleanup and preserve relaunch intent")
	}
}

func TestShutdownCloseFailureCommitsSuccessfulRestore(t *testing.T) {
	a := &App{sysApplied: true, sysOn: true, prev: &sysproxy.State{Enabled: true}}
	var restores int
	fail := true
	a.proxy = proxyControllerFixture{
		restore: func(int, *sysproxy.State) error { restores++; return nil },
		close: func() error {
			if fail {
				return errors.New("cleanup failed")
			}
			return nil
		},
	}
	if err := a.Shutdown(); err == nil || a.sysApplied || a.sysOn || a.prev != nil || a.ShutdownReady() {
		t.Fatal("partial cleanup lost the successful restore or allowed quitting")
	}
	fail = false
	if err := a.Shutdown(); err != nil || restores != 1 || !a.ShutdownReady() || a.shutdown.err != "" {
		t.Fatal("close retry repeated a completed restore or retained its error")
	}
}

func TestDismissRestartFailureClearsRelaunchIntent(t *testing.T) {
	a, _ := newSettingsTestApp(t, nil)
	a.booted, a.ticking = true, true
	fixtureSoftwareUpdate(t, a, update.Ready)
	a.shutdown.relaunch, a.shutdown.err = true, "恢复失败"
	tr := ui.NewTester(a.View, 840, 560)
	tr.Frame()
	if err := tr.Click("返回应用"); err != nil {
		t.Fatal(err)
	}
	if a.RelaunchRequested() || a.shutdown.err != "" || a.softwareUpdates.Snapshot().Visible {
		t.Fatal("returning to the app retained a stale restart request or modal")
	}
}
