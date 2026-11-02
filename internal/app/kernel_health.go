package app

import (
	"context"
	"strings"
	"time"

	"gopurr/internal/core"
	"gopurr/internal/kernel"
	"gopurr/internal/sysproxy"
)

// Kernel health watchdog.
//
// The system proxy points every app on the machine at the core, so a core that
// is really gone must not keep it: traffic would black-hole. But a core that is
// merely slow (large reload, GC pause, a busy controller socket, a service
// being reinstalled) must not cost the user their proxy either. v0.6.7 treated
// two failed 700ms probes as death and also persisted RestoreSystemProxy=false,
// so a hiccup silently turned the proxy off for good.
//
// The watchdog therefore:
//   - probes with a generous timeout and only acts after a sustained failure
//     window, or immediately when a self-spawned core's process really exited;
//   - ignores probes while a start/reload/service/proxy operation is in
//     flight, and during subscription/profile work honours only a real exit;
//   - suspends the proxy without touching prefs, remembers that it did, and
//     re-enables it as soon as the core's local data path answers again;
//   - bounds retries for confirmed exits or local data-path failures, while
//     leaving a merely unresponsive process alone;
//   - lets the user cancel that pending resume from any proxy control.
const (
	kernelHealthInterval = 2 * time.Second
	kernelProbeTimeout   = 2500 * time.Millisecond
	// Both must hold before an unresponsive (not exited) core loses the proxy.
	kernelFailureWindow = 10 * time.Second
	kernelFailureMin    = 4
)

type kernelHealthAction int

const (
	kernelHealthNone kernelHealthAction = iota
	kernelHealthSuspend
	kernelHealthResume
)

// kernelHealthWatch is the pure debounce policy; the loop feeds it one
// observation per tick.
type kernelHealthWatch struct {
	failures int
	since    time.Time
}

func (w *kernelHealthWatch) reset() { w.failures, w.since = 0, time.Time{} }

func (w *kernelHealthWatch) observe(h kernel.Health, busy bool, now time.Time) kernelHealthAction {
	if busy || h == kernel.HealthBusy {
		// A reload legitimately stalls the controller; start counting afresh
		// once it finishes.
		w.reset()
		return kernelHealthNone
	}
	switch h {
	case kernel.HealthOK:
		w.reset()
		return kernelHealthResume
	case kernel.HealthExited:
		w.reset()
		return kernelHealthSuspend
	case kernel.HealthUnresponsive:
		if w.failures == 0 {
			w.since = now
		}
		w.failures++
		if w.failures >= kernelFailureMin && now.Sub(w.since) >= kernelFailureWindow {
			w.reset()
			return kernelHealthSuspend
		}
	}
	return kernelHealthNone
}

// kernelHealthBusyState splits in-flight work by what it says about a
// failed probe. hard work (start/reload, service install, a proxy change,
// shutdown) may legitimately stop or replace the core, so the watchdog stands
// down entirely. profile work (subscription downloads, imports, backups) can
// run for a long time without touching the core: a slow probe then proves
// nothing, but a real process exit still does and must not go unnoticed.
func (a *App) kernelHealthBusyState() (hard, profile bool) {
	if a.shutdownPending.Load() {
		return true, false
	}
	a.bmu.Lock()
	defer a.bmu.Unlock()
	return a.kernelBusy || a.svcBusy || a.sysBusy || a.tunChanging, a.subBusy
}

// kernelHealthBusy reports any operation during which a failed probe is
// expected and must not be read as a crash.
func (a *App) kernelHealthBusy() bool {
	hard, profile := a.kernelHealthBusyState()
	return hard || profile
}

// healthDuringWork is the probe result the debounce policy should see:
// during profile work only a real exit or a healthy answer counts.
func healthDuringWork(h kernel.Health, profile bool) kernel.Health {
	if profile && h == kernel.HealthUnresponsive {
		return kernel.HealthBusy
	}
	return h
}

func (a *App) kernelHealthLoop() {
	t := time.NewTicker(kernelHealthInterval)
	defer t.Stop()
	var w kernelHealthWatch
	var tunWatch kernelHealthWatch
	for range t.C {
		if a.shutdownPending.Load() {
			continue
		}
		generation := a.networkGeneration.Load()
		hard, profile := a.kernelHealthBusyState()
		h := kernel.HealthBusy
		var status core.RuntimeStatus
		var statusErr error = context.DeadlineExceeded
		var proxy sysproxy.State
		var proxyErr error
		if !hard {
			h = healthDuringWork(a.k.Health(kernelProbeTimeout), profile)
			if h == kernel.HealthOK {
				ctx, cancel := context.WithTimeout(context.Background(), kernelProbeTimeout)
				status, statusErr = a.k.Runtime(ctx)
				cancel()
				if statusErr != nil || !status.ProxyReady {
					h = kernel.HealthUnresponsive
				}
			}
			proxy, proxyErr = sysproxy.Current()
		}
		runOnMain(func() {
			if generation != a.networkGeneration.Load() || hard {
				w.reset()
				tunWatch.reset()
				return
			}
			if h == kernel.HealthOK && status.MixedPort != a.mixedPort() {
				h = kernel.HealthUnresponsive
			}
			action := w.observe(h, false, time.Now())
			observedErr := statusErr
			if h != kernel.HealthOK && observedErr == nil && !status.ProxyReady {
				observedErr = context.DeadlineExceeded
			}
			a.acceptNetworkObservation(status, observedErr, proxy, proxyErr)
			switch action {
			case kernelHealthSuspend:
				a.suspendProxyForKernelFailure(h == kernel.HealthExited)
				a.recoverFailedKernel(h == kernel.HealthExited, statusErr == nil && (!status.ProxyReady || status.MixedPort != a.mixedPort()))
			case kernelHealthResume:
				if a.proxySuspendedByHealth.Load() {
					a.resumeProxyAfterKernelRecovery()
				}
			}
			tunHealth := kernel.HealthOK
			if a.ov.TunEnable && statusErr == nil && (!status.TunReady || !status.DNSReady || (a.ov.TunAutoRout && !status.AutoRoute)) {
				tunHealth = kernel.HealthUnresponsive
			}
			if tunWatch.observe(tunHealth, a.kernelHealthBusy(), time.Now()) == kernelHealthSuspend {
				a.recoverFailedKernel(false, true)
			}
		})
	}
}

// suspendProxyForKernelFailure hands the system proxy back while the core is
// gone or unresponsive. It deliberately bypasses setSystemProxy so the user's
// RestoreSystemProxy preference is left untouched.
func (a *App) suspendProxyForKernelFailure(exited bool) {
	hard, profile := a.kernelHealthBusyState()
	if a.shutdownPending.Load() || a.k == nil || !a.sysApplied || hard || (profile && !exited) {
		return
	}
	err := a.restoreProxy()
	if st, serr := sysproxy.Current(); serr == nil {
		a.sysState = st
		a.healthProxyBaseline, a.healthBaselineValid = st, true
	}
	if err != nil {
		a.sysOn = a.sysApplied
		a.setSysErr(err.Error())
		a.notice = tr("内核无响应，系统代理恢复失败，请重试: ") + err.Error()
		a.invalidate()
		return
	}
	a.sysApplied, a.sysOn = false, false
	a.setSysErr("")
	a.proxySuspendedByHealth.Store(true)
	a.sysResumeSwitch = true
	// A start or config failure says more about what went wrong than the
	// generic banner; the overview and tray still show the pending resume.
	if !a.isKernelFailureNotice(a.notice) {
		if exited {
			// Recovery is bounded; a manual restart remains available.
			a.notice = tr("内核已退出，已临时恢复系统代理；重新启动内核后将自动重新开启")
		} else {
			// A system service may still come back on its own.
			a.notice = tr("内核暂时无响应，已临时恢复系统代理；内核恢复后将自动重新开启")
		}
	}
	a.invalidate()
}

// resumeProxyAfterKernelRecovery re-enables a proxy the watchdog suspended,
// once a probe proved the core answers again. It never starts or ensures the
// kernel (a slow-but-alive core must not be reaped) and never persists
// RestoreSystemProxy. One attempt only: a failure (e.g. a declined
// authorization prompt) must not turn into a prompt every two seconds.
func (a *App) resumeProxyAfterKernelRecovery() {
	if !a.proxySuspendedByHealth.Load() || a.shutdownPending.Load() || a.k == nil {
		return
	}
	if hard, _ := a.kernelHealthBusyState(); hard {
		return
	}
	if current, err := sysproxy.Current(); err == nil && a.healthBaselineValid &&
		!sameProxySettings(current, a.healthProxyBaseline) && !ownsProxy(current, a.mixedPort()) {
		a.proxySuspendedByHealth.Store(false)
		a.sysState, a.sysApplied, a.sysOn, a.proxyChanged = current, false, false, true
		a.notice = tr("系统代理设置已变化，已取消自动恢复")
		a.invalidate()
		return
	}
	a.proxySuspendedByHealth.Store(false)
	suspendedNotice := a.isKernelSuspendNotice(a.notice)
	if a.sysApplied || !systemProxyAvailable() {
		if suspendedNotice {
			a.notice = ""
		}
		a.invalidate()
		return
	}
	if err := a.applySystemProxy(true); err != nil {
		a.notice = tr("内核已恢复，但重新开启系统代理失败: ") + err.Error()
	} else if suspendedNotice {
		a.notice = ""
	}
	a.invalidate()
}

func sameProxySettings(a, b sysproxy.State) bool {
	return a.Enabled == b.Enabled && a.Server == b.Server && a.Port == b.Port && a.RawServer == b.RawServer &&
		a.WindowsOptions == b.WindowsOptions && a.Flags == b.Flags && a.Bypass == b.Bypass && a.PAC == b.PAC
}

// cancelHealthResume drops a pending watchdog resume because the user turned
// the (suspended) system proxy off: from the overview switch, the tray, the
// hotkey, the settings toggle, or by stopping the kernel. Like an explicit
// off it is remembered for the next launch.
func (a *App) cancelHealthResume() {
	if !a.proxySuspendedByHealth.Swap(false) {
		return
	}
	a.sysResumeSwitch = false
	if a.isKernelSuspendNotice(a.notice) {
		a.notice = ""
	}
	a.persistRestoreSystemProxy(false)
	a.invalidate()
}

// healthResumePending reports a watchdog-suspended proxy still waiting to
// come back; the overview and tray show it as on (pending).
func (a *App) healthResumePending() bool {
	return a.proxySuspendedByHealth.Load() && !a.sysApplied
}

func (a *App) isKernelSuspendNotice(notice string) bool {
	return notice != "" && (notice == tr("内核已退出，已临时恢复系统代理；重新启动内核后将自动重新开启") ||
		notice == tr("内核暂时无响应，已临时恢复系统代理；内核恢复后将自动重新开启"))
}

// isKernelFailureNotice reports a banner explaining why the core failed to
// start or load, which the generic watchdog banner must not replace.
func (a *App) isKernelFailureNotice(notice string) bool {
	for _, prefix := range []string{tr("启动失败: "), tr("配置无法使用: ")} {
		if strings.HasPrefix(notice, prefix) {
			return true
		}
	}
	return false
}
