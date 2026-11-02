package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"gopurr/internal/sysproxy"
)

func waitProxyChange(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.bmu.Lock()
		busy := a.sysBusy
		a.bmu.Unlock()
		if !busy {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("proxy change did not complete")
}

func TestNetworkSettingsSaveDoesNotBlockOnCoreReload(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	a, _ := newSettingsTestApp(t, func(raw string) bool {
		if strings.Contains(raw, "allow-lan: true") {
			close(entered)
			<-release
		}
		return false
	})
	a.lanOn = true
	previous := runOnMain
	completed := make(chan func(), 1)
	runOnMain = func(fn func()) { completed <- fn }
	t.Cleanup(func() { runOnMain = previous })
	start := time.Now()
	a.requestNetworkSettings()
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("network settings blocked the event loop")
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("core reload did not start")
	}
	if !busyNetwork(a) || a.ov.AllowLAN {
		t.Fatal("network settings committed before the core accepted them")
	}
	release <- struct{}{}
	select {
	case commit := <-completed:
		commit()
	case <-time.After(3 * time.Second):
		t.Fatal("network completion missing")
	}
	if busyNetwork(a) || !a.ov.AllowLAN || a.networkSettingsDirty() || a.networkError != "" {
		t.Fatal("network settings did not commit after the reload")
	}
}

func TestProxySwitchReturnsDuringAuthorizationAndCommitsOnMainThread(t *testing.T) {
	a, _ := newSettingsTestApp(t, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	a.proxy = proxyControllerFixture{enable: func(int, *sysproxy.State) error {
		close(entered)
		<-release
		return errors.New("authorization canceled")
	}}
	previous := runOnMain
	completed := make(chan func(), 1)
	runOnMain = func(fn func()) { completed <- fn }
	t.Cleanup(func() { runOnMain = previous })
	start := time.Now()
	a.setSystemProxy(true)
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("proxy switch blocked the event loop")
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("proxy worker did not start")
	}
	if !busyNetwork(a) || a.sysApplied {
		t.Fatal("pending authorization was committed early")
	}
	// A repeated frame or tray click must not start another authorization.
	a.setSystemProxy(true)
	release <- struct{}{}
	select {
	case commit := <-completed:
		if !busyNetwork(a) {
			t.Fatal("worker committed outside the main thread")
		}
		commit()
	case <-time.After(3 * time.Second):
		t.Fatal("completion was not dispatched")
	}
	if busyNetwork(a) || a.sysApplied || a.sysOn || a.Prefs().RestoreSystemProxy {
		t.Fatal("canceled authorization changed the user's preference")
	}
}
