package app

import (
	"errors"
	"gopurr/internal/nativehost"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ui "gopurr/internal/appui"
	"gopurr/internal/sysproxy"
	"gopurr/internal/update"
)

func TestShutdownWaitsForNetworkCommitBeforeTakingSnapshot(t *testing.T) {
	restored := make(chan struct{}, 1)
	a := &App{proxy: proxyControllerFixture{restore: func(int, *sysproxy.State) error { restored <- struct{}{}; return nil }}}
	a.tunChanging = true
	done := make(chan error, 1)
	a.beginShutdown(func(fn func()) { fn() }, func(err error) { done <- err })
	select {
	case <-done:
		t.Fatal("shutdown finished before network commit")
	case <-time.After(30 * time.Millisecond):
	}
	a.sysApplied = true
	a.bmu.Lock()
	a.tunChanging = false
	a.bmu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not resume")
	}
	select {
	case <-restored:
	default:
		t.Fatal("shutdown used the proxy state from before the commit")
	}
}

func TestUpdateRestartStaysResponsiveAndRetriesCleanup(t *testing.T) {
	t.Skip("legacy window harness was removed with mygo; the native host draws the desktop")
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
	t.Skip("legacy window harness was removed with mygo; the native host draws the desktop")
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

func TestShutdownWaitsForDownloadingProfileBeforeReadyAndHomeUnlock(t *testing.T) {
	a, _ := newSettingsTestApp(t, nil)
	downloadEntered, releaseDownload := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	var block atomic.Bool
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if block.Load() {
			close(downloadEntered)
			<-releaseDownload
		}
		_, _ = w.Write([]byte("rules: [\"MATCH,DIRECT\"]\n"))
	}))
	defer func() { releaseOnce.Do(func() { close(releaseDownload) }); source.Close() }()
	item, err := a.store.Add(source.URL, "Delayed local subscription")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.SetActive(item.ID); err != nil {
		t.Fatal(err)
	}
	homeLock, err := nativehost.LockHome(a.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer homeLock.Close()
	closed := make(chan struct{}, 1)
	a.proxy = proxyControllerFixture{close: func() error { closed <- struct{}{}; return nil }}
	if !a.beginProfileWork() {
		t.Fatal("profile reservation unavailable")
	}
	block.Store(true)
	profileDone := make(chan error, 1)
	go func() { err := a.refreshManagedProfile(item.ID); a.endProfileWork(); profileDone <- err }()
	select {
	case <-downloadEntered:
	case <-time.After(time.Second):
		t.Fatal("profile download did not start")
	}
	posted := make(chan func(), 2)
	done := make(chan error, 1)
	a.beginShutdown(func(fn func()) { posted <- fn }, func(err error) {
		if err == nil {
			if !a.ShutdownReady() {
				t.Error("home unlock preceded ShutdownReady")
			}
			homeLock.Close()
		}
		done <- err
	})
	select {
	case <-posted:
		t.Fatal("shutdown started before profile download/commit finished")
	case <-time.After(35 * time.Millisecond):
	}
	if a.ShutdownReady() {
		t.Fatal("profile download did not block readiness")
	}
	select {
	case <-closed:
		t.Fatal("cleanup ran during profile work")
	default:
	}
	if lock, err := nativehost.LockHome(a.dir); !errors.Is(err, nativehost.ErrHomeInUse) {
		if lock != nil {
			lock.Close()
		}
		t.Fatal("home unlocked during pending profile work", err)
	}
	releaseOnce.Do(func() { close(releaseDownload) })
	select {
	case err := <-profileDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("profile did not commit")
	}
	for i := 0; i < 2; i++ {
		select {
		case fn := <-posted:
			fn()
		case <-time.After(2 * time.Second):
			t.Fatal("shutdown did not resume after profile commit")
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown completion missing")
	}
	if !a.ShutdownReady() {
		t.Fatal("shutdown never became ready")
	}
	again, err := nativehost.LockHome(a.dir)
	if err != nil {
		t.Fatal("completed cleanup did not release the home", err)
	}
	again.Close()
}
