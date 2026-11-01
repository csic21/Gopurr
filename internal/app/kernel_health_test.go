package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"gopurr/internal/kernel"
	"gopurr/internal/sysproxy"
)

func TestKernelHealthWatchNeedsSustainedFailures(t *testing.T) {
	var w kernelHealthWatch
	start := time.Unix(1000, 0)
	// Many quick failures inside the window do not trigger.
	for i := 0; i < 8; i++ {
		if got := w.observe(kernel.HealthUnresponsive, false, start.Add(time.Duration(i)*time.Second)); got != kernelHealthNone {
			t.Fatalf("failure %d at +%ds acted early: %v", i+1, i, got)
		}
	}
	// Window elapsed with enough failures: suspend.
	if got := w.observe(kernel.HealthUnresponsive, false, start.Add(kernelFailureWindow)); got != kernelHealthSuspend {
		t.Fatalf("sustained failure did not suspend: %v", got)
	}

	// Long window but too few samples does not trigger.
	w.reset()
	if got := w.observe(kernel.HealthUnresponsive, false, start); got != kernelHealthNone {
		t.Fatal(got)
	}
	if got := w.observe(kernel.HealthUnresponsive, false, start.Add(30*time.Second)); got != kernelHealthNone {
		t.Fatalf("two failures suspended: %v", got)
	}
}

func TestKernelHealthWatchResetsOnSuccessAndBusy(t *testing.T) {
	var w kernelHealthWatch
	start := time.Unix(1000, 0)
	step := func(i int) time.Time { return start.Add(time.Duration(i) * 3 * time.Second) }
	for i := 0; i < 3; i++ {
		w.observe(kernel.HealthUnresponsive, false, step(i))
	}
	if got := w.observe(kernel.HealthOK, false, step(3)); got != kernelHealthResume {
		t.Fatalf("healthy probe = %v", got)
	}
	for i := 4; i < 7; i++ {
		if got := w.observe(kernel.HealthUnresponsive, false, step(i)); got != kernelHealthNone {
			t.Fatalf("streak did not reset after success: %v", got)
		}
	}
	// A reload in flight resets the streak and never acts.
	if got := w.observe(kernel.HealthUnresponsive, true, step(7)); got != kernelHealthNone {
		t.Fatal(got)
	}
	if got := w.observe(kernel.HealthBusy, false, step(8)); got != kernelHealthNone {
		t.Fatal(got)
	}
	for i := 9; i < 13; i++ { // four failures, but only 9s since the reset
		if got := w.observe(kernel.HealthUnresponsive, false, step(i)); got != kernelHealthNone {
			t.Fatalf("streak did not reset after busy: %v", got)
		}
	}
	if got := w.observe(kernel.HealthUnresponsive, false, step(13)); got != kernelHealthSuspend {
		t.Fatalf("fresh sustained streak = %v", got)
	}
}

func TestKernelHealthWatchActsImmediatelyOnRealExit(t *testing.T) {
	var w kernelHealthWatch
	if got := w.observe(kernel.HealthExited, false, time.Unix(1000, 0)); got != kernelHealthSuspend {
		t.Fatalf("exit = %v", got)
	}
	if got := w.observe(kernel.HealthExited, true, time.Unix(1001, 0)); got != kernelHealthNone {
		t.Fatalf("exit while busy = %v", got)
	}
}

type proxyCalls struct{ enable, restore int }

func healthTestApp(t *testing.T, enableErr error) (*App, *proxyCalls) {
	t.Helper()
	a, _ := newSettingsTestApp(t, nil)
	calls := &proxyCalls{}
	a.proxy = proxyControllerFixture{
		enable:  func(int, *sysproxy.State) error { calls.enable++; return enableErr },
		restore: func(int, *sysproxy.State) error { calls.restore++; return nil },
	}
	previous := systemProxyAvailable
	systemProxyAvailable = func() bool { return true }
	t.Cleanup(func() { systemProxyAvailable = previous })
	a.persistRestoreSystemProxy(true)
	a.sysApplied, a.sysOn = true, true
	return a, calls
}

func TestKernelSuspendKeepsRestorePreference(t *testing.T) {
	a, calls := healthTestApp(t, nil)
	a.suspendProxyForKernelFailure(false)
	if calls.restore != 1 || a.sysApplied || a.sysOn {
		t.Fatalf("proxy not handed back: restore=%d applied=%v on=%v", calls.restore, a.sysApplied, a.sysOn)
	}
	if !a.Prefs().RestoreSystemProxy || !a.restoreProxyOn {
		t.Fatal("watchdog suspension persisted RestoreSystemProxy=false")
	}
	if !a.proxySuspendedByHealth.Load() {
		t.Fatal("suspension not remembered")
	}
	if !strings.Contains(a.notice, "暂时无响应") {
		t.Fatalf("notice = %q", a.notice)
	}
}

func TestKernelSuspendSkippedWhileBusy(t *testing.T) {
	for _, set := range []func(*App){
		func(a *App) { a.kernelBusy = true },
		func(a *App) { a.subBusy = true },
		func(a *App) { a.svcBusy = true },
	} {
		a, calls := healthTestApp(t, nil)
		set(a)
		a.suspendProxyForKernelFailure(true)
		if calls.restore != 0 || !a.sysApplied || a.proxySuspendedByHealth.Load() {
			t.Fatalf("suspended while busy: restore=%d applied=%v", calls.restore, a.sysApplied)
		}
	}
}

func TestKernelRecoveryReenablesSuspendedProxy(t *testing.T) {
	a, calls := healthTestApp(t, nil)
	a.suspendProxyForKernelFailure(true)
	if !strings.Contains(a.notice, "内核已退出") {
		t.Fatalf("exit notice = %q", a.notice)
	}
	if !a.k.Running() {
		t.Fatal("fixture kernel should answer")
	}
	a.resumeProxyAfterKernelRecovery()
	if calls.enable != 1 || !a.sysApplied || !a.sysOn {
		t.Fatalf("proxy not re-enabled: enable=%d applied=%v", calls.enable, a.sysApplied)
	}
	if a.proxySuspendedByHealth.Load() || a.notice != "" || !a.Prefs().RestoreSystemProxy {
		t.Fatalf("recovery state: suspended=%v notice=%q pref=%v", a.proxySuspendedByHealth.Load(), a.notice, a.Prefs().RestoreSystemProxy)
	}
	// Nothing left to resume.
	a.resumeProxyAfterKernelRecovery()
	if calls.enable != 1 {
		t.Fatal("resumed twice")
	}
}

func TestKernelRecoveryTriesOnceOnEnableFailure(t *testing.T) {
	a, calls := healthTestApp(t, errors.New("authorization canceled"))
	a.suspendProxyForKernelFailure(false)
	a.resumeProxyAfterKernelRecovery()
	a.resumeProxyAfterKernelRecovery()
	if calls.enable != 1 || a.sysApplied || a.proxySuspendedByHealth.Load() {
		t.Fatalf("enable=%d applied=%v suspended=%v", calls.enable, a.sysApplied, a.proxySuspendedByHealth.Load())
	}
	if !strings.Contains(a.notice, "authorization canceled") {
		t.Fatalf("notice = %q", a.notice)
	}
	if !a.Prefs().RestoreSystemProxy {
		t.Fatal("failed resume cleared preference")
	}
}

func TestExplicitProxyToggleClearsHealthSuspension(t *testing.T) {
	a, _ := healthTestApp(t, nil)
	a.suspendProxyForKernelFailure(false)
	a.setSystemProxy(false) // user turns it off from the tray while suspended
	if a.proxySuspendedByHealth.Load() || a.Prefs().RestoreSystemProxy {
		t.Fatalf("explicit off: suspended=%v pref=%v", a.proxySuspendedByHealth.Load(), a.Prefs().RestoreSystemProxy)
	}
}
