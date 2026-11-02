package app

import (
	"context"
	"errors"
	"time"

	"gopurr/internal/core"
	"gopurr/internal/sysproxy"
)

// Workers receive immutable inputs. Only commitProxyChange touches UI state.
type proxyChange struct {
	on, applied bool
	port        int
	previous    *sysproxy.State
	controller  sysproxy.Controller
	current     func() (sysproxy.State, error)
	runtime     func(context.Context) (core.RuntimeStatus, error)
	ensure      func() error
	baseline    *sysproxy.State
}

var errProxyNotReady = errors.New("local proxy is not ready")

type proxyNotReadyError struct{ cause error }

func (e *proxyNotReadyError) Error() string {
	return tr("本地代理未就绪，请检查内核状态与端口设置")
}
func (e *proxyNotReadyError) Unwrap() []error { return []error{errProxyNotReady, e.cause} }

var errProxyOwnershipChanged = errors.New("system proxy ownership changed")

type proxyChangeResult struct {
	state    sysproxy.State
	readErr  error
	previous *sysproxy.State
	runtime  core.RuntimeStatus
	err      error
}

func (w proxyChange) run() proxyChangeResult {
	r := proxyChangeResult{previous: w.previous}
	if w.on {
		if w.baseline != nil {
			current, err := w.current()
			if err != nil {
				r.err = err
			} else if !sameProxySettings(current, *w.baseline) && !ownsProxy(current, w.port) {
				r.err = errProxyOwnershipChanged
			}
		}
		if r.err == nil && w.ensure != nil {
			r.err = w.ensure()
		}
		if r.err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), kernelProbeTimeout)
			r.runtime, r.err = w.runtime(ctx)
			cancel()
			if r.err != nil || !r.runtime.ProxyReady || r.runtime.MixedPort != w.port {
				r.err = &proxyNotReadyError{cause: r.err}
			}
		}
		if r.err == nil && !w.applied {
			if current, err := w.current(); err == nil && (current.Enabled || current.WindowsOptions || current.ProtocolsKnown) && !ownsProxy(current, w.port) {
				r.previous = &current
			}
		}
		if r.err == nil {
			r.err = w.controller.Enable(w.port, r.previous)
		}
	} else {
		r.err = w.controller.Restore(w.port, w.previous)
		if r.err == nil {
			r.previous = nil
		}
	}
	r.state, r.readErr = w.current()
	return r
}

func (a *App) proxyChangeWork(on bool) proxyChange {
	var previous *sysproxy.State
	if a.prev != nil {
		copy := *a.prev
		previous = &copy
	}
	return proxyChange{on: on, applied: a.sysApplied, port: a.mixedPort(), previous: previous,
		controller: a.proxyController(), current: sysproxy.Current, runtime: a.k.Runtime}
}

func (a *App) commitProxyChange(on bool, r proxyChangeResult) {
	if r.readErr == nil {
		a.sysState, a.proxyReadErr = r.state, ""
	} else {
		a.proxyReadErr = r.readErr.Error()
	}
	if r.err != nil {
		a.sysOn = a.sysApplied
		a.setSysErr(r.err.Error())
		return
	}
	if a.prev == nil || r.previous == nil || *a.prev != *r.previous {
		a.prev = r.previous
	}
	a.savePrevProxy()
	a.sysApplied, a.sysOn, a.proxyChanged = on, on, false
	if on {
		a.networkRuntime, a.networkChecked, a.networkErr = r.runtime, time.Now(), ""
	}
	a.setSysErr("")
}

// Ordinary switches, tray actions and watchdog changes never run networksetup,
// authorization, local probes or journal locks on the event loop.
func (a *App) requestSystemProxy(on, remember, ensure bool, after func(error)) {
	if a.shutdownPending.Load() && on {
		return
	}
	if !systemProxyAvailable() {
		err := errors.New(tr("当前平台暂不支持自动设置系统代理"))
		a.setSysErr(err.Error())
		if after != nil {
			after(err)
		}
		return
	}
	a.bmu.Lock()
	if a.sysBusy {
		a.bmu.Unlock()
		return
	}
	a.sysBusy = true
	a.bmu.Unlock()
	a.networkGeneration.Add(1)
	w := a.proxyChangeWork(on)
	if on && !remember && a.healthBaselineValid {
		baseline := a.healthProxyBaseline
		w.baseline = &baseline
	}
	if on && ensure {
		raw, err := a.configBytes()
		if err != nil {
			a.commitProxyChange(on, proxyChangeResult{err: err})
			a.bmu.Lock()
			a.sysBusy = false
			a.bmu.Unlock()
			if after != nil {
				after(err)
			}
			return
		}
		k := a.k
		w.ensure = func() error {
			if !k.Running() {
				return k.Start(raw)
			}
			return nil
		}
		a.kernelStopped = false
	}
	a.invalidate()
	go func() {
		r := w.run()
		runOnMain(func() {
			a.commitProxyChange(on, r)
			if r.err == nil && remember {
				a.proxySuspendedByHealth.Store(false)
				a.persistRestoreSystemProxy(on)
			}
			if after != nil {
				after(r.err)
			}
			a.bmu.Lock()
			a.sysBusy = false
			a.bmu.Unlock()
			a.invalidate()
		})
	}()
}

func (a *App) setSystemProxy(on bool) {
	if busyNetwork(a) {
		return
	}
	a.requestSystemProxy(on, true, true, nil)
}

func (a *App) toggleSystemProxy(after func(error)) {
	if a.healthResumePending() {
		a.cancelHealthResume()
		return
	}
	if busyNetwork(a) || a.shutdownPending.Load() {
		return
	}
	a.bmu.Lock()
	a.sysBusy = true
	a.bmu.Unlock()
	a.networkGeneration.Add(1)
	go func() {
		current, err := sysproxy.Current()
		runOnMain(func() {
			a.bmu.Lock()
			a.sysBusy = false
			a.bmu.Unlock()
			if a.shutdownPending.Load() {
				return
			}
			if err != nil {
				a.setSysErr(err.Error())
				if after != nil {
					after(err)
				}
				a.invalidate()
				return
			}
			a.sysState = current
			a.sysApplied = ownsProxy(current, a.mixedPort())
			a.requestSystemProxy(!a.sysApplied, true, true, after)
		})
	}()
}
