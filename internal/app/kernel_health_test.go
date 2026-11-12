package app

import (
	"context"
	"errors"
	"gopurr/internal/core"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ui "gopurr/internal/appui"

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
	waitProxyChange(t, a)
	if calls.restore != 1 || a.sysApplied || a.sysOn {
		t.Fatalf("proxy not handed back: restore=%d applied=%v on=%v", calls.restore, a.sysApplied, a.sysOn)
	}
	if !a.Prefs().RestoreSystemProxy {
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
	for _, tc := range []struct {
		set    func(*App)
		exited bool
	}{
		{func(a *App) { a.kernelBusy = true }, true},
		{func(a *App) { a.svcBusy = true }, true},
		{func(a *App) { a.sysBusy = true }, true},
		// Profile work only ignores an unresponsive core; see
		// TestProfileWorkStillHonoursRealExit.
		{func(a *App) { a.subBusy = true }, false},
	} {
		a, calls := healthTestApp(t, nil)
		tc.set(a)
		a.suspendProxyForKernelFailure(tc.exited)
		if calls.restore != 0 || !a.sysApplied || a.proxySuspendedByHealth.Load() {
			t.Fatalf("suspended while busy: restore=%d applied=%v", calls.restore, a.sysApplied)
		}
	}
}

func TestKernelRecoveryReenablesSuspendedProxy(t *testing.T) {
	a, calls := healthTestApp(t, nil)
	a.suspendProxyForKernelFailure(true)
	waitProxyChange(t, a)
	if !strings.Contains(a.notice, "内核已退出") || !strings.Contains(a.notice, "重新启动内核") {
		t.Fatalf("exit notice should ask for a manual start: %q", a.notice)
	}
	if a.k.Health(time.Second) != kernel.HealthOK {
		t.Fatal("fixture kernel should answer")
	}
	a.resumeProxyAfterKernelRecovery()
	waitProxyChange(t, a)
	if calls.enable != 1 || !a.sysApplied || !a.sysOn {
		t.Fatalf("proxy not re-enabled: enable=%d applied=%v", calls.enable, a.sysApplied)
	}
	if a.proxySuspendedByHealth.Load() || a.notice != "" || !a.Prefs().RestoreSystemProxy {
		t.Fatalf("recovery state: suspended=%v notice=%q pref=%v", a.proxySuspendedByHealth.Load(), a.notice, a.Prefs().RestoreSystemProxy)
	}
	// Nothing left to resume.
	a.resumeProxyAfterKernelRecovery()
	waitProxyChange(t, a)
	if calls.enable != 1 {
		t.Fatal("resumed twice")
	}
}

func TestKernelRecoveryTriesOnceOnEnableFailure(t *testing.T) {
	a, calls := healthTestApp(t, errors.New("authorization canceled"))
	a.suspendProxyForKernelFailure(false)
	waitProxyChange(t, a)
	a.resumeProxyAfterKernelRecovery()
	waitProxyChange(t, a)
	a.resumeProxyAfterKernelRecovery()
	waitProxyChange(t, a)
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
	waitProxyChange(t, a)
	a.setSystemProxy(false) // user turns it off from the tray while suspended
	waitProxyChange(t, a)
	if a.proxySuspendedByHealth.Load() || a.Prefs().RestoreSystemProxy {
		t.Fatalf("explicit off: suspended=%v pref=%v", a.proxySuspendedByHealth.Load(), a.Prefs().RestoreSystemProxy)
	}
}

// A core that answers in 0.7–2.5s is alive for the watchdog but "not
// running" for the quick UI probe. Resuming must trust the watchdog: going
// through setSystemProxy would call Start → Ensure, which reaps the live core.
func TestKernelResumeNeverStartsSlowButAliveKernel(t *testing.T) {
	a, reloads := newSettingsTestAppWithVersionDelay(t, nil, 1200*time.Millisecond)
	calls := &proxyCalls{}
	a.proxy = proxyControllerFixture{
		enable:  func(int, *sysproxy.State) error { calls.enable++; return nil },
		restore: func(int, *sysproxy.State) error { calls.restore++; return nil },
	}
	previous := systemProxyAvailable
	systemProxyAvailable = func() bool { return true }
	t.Cleanup(func() { systemProxyAvailable = previous })
	a.persistRestoreSystemProxy(true)
	a.sysApplied, a.sysOn = true, true

	if h := a.k.Health(kernelProbeTimeout); h != kernel.HealthOK {
		t.Fatalf("slow core health = %v, want ok", h)
	}
	if !a.k.Running() {
		t.Fatal("successful health probe must keep the slow core visible as running")
	}
	a.suspendProxyForKernelFailure(false)
	waitProxyChange(t, a)
	before := len(reloads())
	a.resumeProxyAfterKernelRecovery()
	waitProxyChange(t, a)
	if calls.enable != 1 || !a.sysApplied || !a.sysOn || a.proxySuspendedByHealth.Load() {
		t.Fatalf("resume: enable=%d applied=%v on=%v suspended=%v", calls.enable, a.sysApplied, a.sysOn, a.proxySuspendedByHealth.Load())
	}
	if len(reloads()) != before || a.kernelBusyState() || strings.Contains(a.notice, "正在启动") {
		t.Fatalf("resume started the kernel: reloads %d→%d busy=%v notice=%q", before, len(reloads()), a.kernelBusyState(), a.notice)
	}
}

func TestKernelResumeDoesNotPersistPreference(t *testing.T) {
	a, calls := healthTestApp(t, nil)
	a.suspendProxyForKernelFailure(false)
	waitProxyChange(t, a)
	// The preference changed elsewhere (not through the settings page) while
	// suspended; the resume must not write it back to true.
	if err := a.changePrefs(func(p *Prefs) { p.RestoreSystemProxy = false }); err != nil {
		t.Fatal(err)
	}
	a.resumeProxyAfterKernelRecovery()
	waitProxyChange(t, a)
	if calls.enable != 1 || !a.sysApplied {
		t.Fatalf("enable=%d applied=%v", calls.enable, a.sysApplied)
	}
	if a.Prefs().RestoreSystemProxy {
		t.Fatal("watchdog resume persisted RestoreSystemProxy=true")
	}
}

func TestStartupPolicyDoesNotCancelCurrentHealthRecovery(t *testing.T) {
	a, calls := healthTestApp(t, nil)
	a.reconcileSettings()
	a.suspendProxyForKernelFailure(false)
	waitProxyChange(t, a)
	a.proxyStartupIdx = 2
	a.reconcileSettings()
	if !a.proxySuspendedByHealth.Load() || !a.Prefs().RestoreSystemProxy || a.Prefs().systemProxyOnStartup() {
		t.Fatal("next-launch policy changed current recovery or failed to save")
	}
	a.resumeProxyAfterKernelRecovery()
	waitProxyChange(t, a)
	if calls.enable != 1 || !a.sysApplied {
		t.Fatal("next-launch policy prevented current-session recovery")
	}
}

func TestTrayShowsAndCancelsPendingResume(t *testing.T) {
	a, calls := healthTestApp(t, nil)
	a.suspendProxyForKernelFailure(false)
	waitProxyChange(t, a)
	item := findTrayItem(t, a.trayItems(true, nil, func() {}), "system-proxy")
	if !item.Checked || !strings.Contains(item.Label, "等待内核恢复") {
		t.Fatalf("pending resume item = label:%q checked:%v", item.Label, item.Checked)
	}
	item.SetChecked(false) // what the native menu does before Click
	item.Click(item, nil)
	if a.proxySuspendedByHealth.Load() || a.Prefs().RestoreSystemProxy || calls.enable != 0 {
		t.Fatalf("tray cancel: suspended=%v pref=%v enable=%d", a.proxySuspendedByHealth.Load(), a.Prefs().RestoreSystemProxy, calls.enable)
	}
	if a.kernelBusyState() {
		t.Fatal("tray cancel tried to start the kernel")
	}
	item = findTrayItem(t, a.trayItems(true, nil, func() {}), "system-proxy")
	if item.Checked || item.Label != "系统代理" {
		t.Fatalf("after cancel = label:%q checked:%v", item.Label, item.Checked)
	}
}

func TestOverviewSwitchCancelsPendingResume(t *testing.T) {
	t.Skip("legacy window harness was removed with mygo; the native host draws the desktop")
	a, calls := healthTestApp(t, nil)
	a.suspendProxyForKernelFailure(false)
	waitProxyChange(t, a)
	tr := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme(c.Theme().Dark))
		a.overviewNetworkControls(c)
	}, 720, 320)
	tr.Frame()
	if _, ok := tr.Find("内核恢复后自动重新开启 · 关闭开关可取消"); !ok {
		t.Fatal("pending resume hint not shown")
	}
	if err := tr.Click("系统代理开关"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if a.proxySuspendedByHealth.Load() || a.Prefs().RestoreSystemProxy || calls.enable != 0 || a.kernelBusyState() {
		t.Fatalf("overview cancel: suspended=%v pref=%v enable=%d busy=%v", a.proxySuspendedByHealth.Load(), a.Prefs().RestoreSystemProxy, calls.enable, a.kernelBusyState())
	}
	if a.sysOn || a.sysApplied {
		t.Fatal("canceling turned the proxy on")
	}
}

func TestStopKernelClearsSuspensionEvenWhenStopFails(t *testing.T) {
	a, _ := healthTestApp(t, nil)
	a.suspendProxyForKernelFailure(false)
	waitProxyChange(t, a)
	// The fixture daemon is not owned by the app, so Stop fails; the user's
	// intent to stop still cancels the pending resume.
	a.toggle()
	if a.proxySuspendedByHealth.Load() {
		t.Fatal("stopping the kernel left a pending resume")
	}
}

func TestSuspendKeepsSpecificStartFailureNotice(t *testing.T) {
	a, _ := healthTestApp(t, nil)
	a.notice = "启动失败: bad rule"
	a.suspendProxyForKernelFailure(true)
	waitProxyChange(t, a)
	if a.notice != "启动失败: bad rule" || !a.proxySuspendedByHealth.Load() {
		t.Fatalf("notice=%q suspended=%v", a.notice, a.proxySuspendedByHealth.Load())
	}
	// Resuming does not clear a notice the watchdog did not set.
	a.resumeProxyAfterKernelRecovery()
	waitProxyChange(t, a)
	if a.notice != "启动失败: bad rule" {
		t.Fatalf("resume cleared failure notice: %q", a.notice)
	}
}

func TestProfileWorkStillHonoursRealExit(t *testing.T) {
	if got := healthDuringWork(kernel.HealthUnresponsive, true); got != kernel.HealthBusy {
		t.Fatalf("unresponsive during profile work = %v", got)
	}
	for _, h := range []kernel.Health{kernel.HealthExited, kernel.HealthOK} {
		if got := healthDuringWork(h, true); got != h {
			t.Fatalf("%v during profile work = %v", h, got)
		}
	}
	if got := healthDuringWork(kernel.HealthUnresponsive, false); got != kernel.HealthUnresponsive {
		t.Fatal(got)
	}

	a, calls := healthTestApp(t, nil)
	a.subBusy = true
	a.suspendProxyForKernelFailure(false)
	waitProxyChange(t, a)
	if calls.restore != 0 || !a.sysApplied {
		t.Fatal("slow probe during a download suspended the proxy")
	}
	a.suspendProxyForKernelFailure(true)
	waitProxyChange(t, a)
	if calls.restore != 1 || a.sysApplied || !a.proxySuspendedByHealth.Load() {
		t.Fatal("real exit during a download was ignored")
	}
	a.resumeProxyAfterKernelRecovery()
	waitProxyChange(t, a)
	if calls.enable != 1 || !a.sysApplied {
		t.Fatal("recovery during profile work did not resume")
	}
}

func TestProxyChangeMarksSysBusy(t *testing.T) {
	a, _ := healthTestApp(t, nil)
	var during bool
	a.proxy = proxyControllerFixture{
		enable: func(int, *sysproxy.State) error {
			hard, _ := a.kernelHealthBusyState()
			during = hard
			return nil
		},
		restore: func(int, *sysproxy.State) error { return nil },
	}
	a.requestSystemProxy(true, false, false, nil)
	waitProxyChange(t, a)
	if err := a.sysErrText(); err != "" {
		t.Fatal(err)
	}
	if !during {
		t.Fatal("watchdog not held off while the OS proxy changes")
	}
	if hard, _ := a.kernelHealthBusyState(); hard {
		t.Fatal("sysBusy left set")
	}
}

func TestStartKernelAsyncCompletesOnMainThread(t *testing.T) {
	a, _ := newSettingsTestApp(t, nil)
	previous := runOnMain
	t.Cleanup(func() { runOnMain = previous })
	var onMain atomic.Bool
	done := make(chan struct{})
	runOnMain = func(fn func()) {
		onMain.Store(true)
		fn()
		onMain.Store(false)
	}
	a.startKernelAsync(func(ok bool) {
		if !ok {
			t.Errorf("start failed: %q", a.notice)
		}
		if !onMain.Load() {
			t.Error("after ran off the main thread")
		}
		if a.kernelBusyState() {
			t.Error("kernelBusy still set when after ran")
		}
		close(done)
	})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("async start timed out")
	}
}

func TestControllerOutageDoesNotSuspendWorkingProxy(t *testing.T) {
	var watch kernelHealthWatch
	start := time.Now()
	for i := 0; i < 10; i++ {
		h, s := checkLocalProxyHealth(kernel.HealthUnresponsive, core.RuntimeStatus{}, context.DeadlineExceeded, 7890, func() error { return nil })
		if h != kernel.HealthOK || !s.ProxyReady || s.MixedPort != 7890 || s.TunReady {
			t.Fatalf("independent data path observation: %v %+v", h, s)
		}
		if got := watch.observe(h, false, start.Add(time.Duration(i)*3*time.Second)); got == kernelHealthSuspend {
			t.Fatal("working proxy was suspended")
		}
	}
	for _, h := range []kernel.Health{kernel.HealthExited, kernel.HealthBusy} {
		got, _ := checkLocalProxyHealth(h, core.RuntimeStatus{}, context.DeadlineExceeded, 7890, func() error { t.Fatal("must not override exit or in-flight work"); return nil })
		if got != h {
			t.Fatalf("health changed: %v", got)
		}
	}
	h, _ := checkLocalProxyHealth(kernel.HealthUnresponsive, core.RuntimeStatus{}, context.DeadlineExceeded, 7890, func() error { return errors.New("connection refused") })
	if h != kernel.HealthUnresponsive {
		t.Fatal("failed local handshake hid outage")
	}
	h, _ = checkLocalProxyHealth(kernel.HealthUnresponsive, core.RuntimeStatus{MixedPort: 7891}, nil, 7890, func() error { t.Fatal("must not mask observed port mismatch"); return nil })
	if h != kernel.HealthUnresponsive {
		t.Fatal("observed misconfiguration hidden")
	}
}

func TestKernelResumeKeepsPendingOnPreflightFailure(t *testing.T) {
	a, calls := healthTestApp(t, errProxyNotReady)
	a.suspendProxyForKernelFailure(false)
	waitProxyChange(t, a)
	a.resumeProxyAfterKernelRecovery()
	waitProxyChange(t, a)
	if !a.healthResumePending() || a.sysApplied {
		t.Fatal("transient preflight failure discarded pending resume")
	}
	a.proxy = proxyControllerFixture{
		enable:  func(int, *sysproxy.State) error { calls.enable++; return nil },
		restore: func(int, *sysproxy.State) error { return nil },
	}
	a.resumeProxyAfterKernelRecovery()
	waitProxyChange(t, a)
	if a.healthResumePending() || !a.sysApplied {
		t.Fatal("healthy retry did not restore proxy")
	}
}
