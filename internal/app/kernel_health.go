package app

import (
	"time"

	"github.com/egoist/mygo"

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
//   - ignores probes while a start/reload/subscription/service/proxy operation
//     is in flight;
//   - suspends the proxy without touching prefs, remembers that it did, and
//     re-enables it as soon as the core answers again.
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

// kernelHealthBusy reports any operation during which a failed probe is
// expected and must not be read as a crash.
func (a *App) kernelHealthBusy() bool {
	if a.shutdownPending.Load() {
		return true
	}
	a.bmu.Lock()
	defer a.bmu.Unlock()
	return a.kernelBusy || a.subBusy || a.svcBusy || a.sysBusy
}

func (a *App) kernelHealthLoop() {
	t := time.NewTicker(kernelHealthInterval)
	defer t.Stop()
	var w kernelHealthWatch
	for range t.C {
		busy := a.kernelHealthBusy()
		h := kernel.HealthBusy
		if !busy {
			h = a.k.Health(kernelProbeTimeout)
		}
		switch w.observe(h, busy, time.Now()) {
		case kernelHealthSuspend:
			exited := h == kernel.HealthExited
			mygo.RunOnMain(func() { a.suspendProxyForKernelFailure(exited) })
		case kernelHealthResume:
			if a.proxySuspendedByHealth.Load() {
				mygo.RunOnMain(a.resumeProxyAfterKernelRecovery)
			}
		}
	}
}

// suspendProxyForKernelFailure hands the system proxy back while the core is
// gone or unresponsive. It deliberately bypasses setSystemProxy so the user's
// RestoreSystemProxy preference is left untouched.
func (a *App) suspendProxyForKernelFailure(exited bool) {
	if a.shutdownPending.Load() || a.k == nil || !a.sysApplied || a.kernelHealthBusy() {
		return
	}
	err := a.restoreProxy()
	if st, serr := sysproxy.Current(); serr == nil {
		a.sysState = st
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
	if exited {
		a.notice = tr("内核已退出，已临时恢复系统代理；内核恢复后将自动重新开启")
	} else {
		a.notice = tr("内核暂时无响应，已临时恢复系统代理；内核恢复后将自动重新开启")
	}
	a.invalidate()
}

// resumeProxyAfterKernelRecovery re-enables a proxy the watchdog suspended,
// once the core answers again. One attempt only: a failure (e.g. a declined
// authorization prompt) must not turn into a prompt every two seconds.
func (a *App) resumeProxyAfterKernelRecovery() {
	if !a.proxySuspendedByHealth.Load() || a.shutdownPending.Load() || a.k == nil || a.kernelHealthBusy() {
		return
	}
	a.proxySuspendedByHealth.Store(false)
	suspendedNotice := a.isKernelSuspendNotice(a.notice)
	if a.sysApplied {
		if suspendedNotice {
			a.notice = ""
		}
		a.invalidate()
		return
	}
	a.setSystemProxy(true)
	switch {
	case !a.sysApplied:
		a.notice = tr("内核已恢复，但重新开启系统代理失败: ") + a.sysErrText()
	case suspendedNotice:
		a.notice = ""
	}
	a.invalidate()
}

func (a *App) isKernelSuspendNotice(notice string) bool {
	return notice != "" && (notice == tr("内核已退出，已临时恢复系统代理；内核恢复后将自动重新开启") ||
		notice == tr("内核暂时无响应，已临时恢复系统代理；内核恢复后将自动重新开启"))
}
