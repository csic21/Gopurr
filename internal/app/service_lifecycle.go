package app

import (
	"errors"
	"os"
	"time"

	"gopurr/internal/service"
)

// The worker owns only immutable config snapshots and lifecycle calls. UI
// state and OS proxy ownership are committed on the main thread.
type serviceTransition struct {
	stop                 func()
	change               func() error
	installed            func() bool
	start                func([]byte) error
	previous, withoutTUN []byte
	wantedTUN            bool
	removing             bool
}

type serviceTransitionResult struct {
	installed, tun bool
	err            error
}

func (t serviceTransition) run() serviceTransitionResult {
	t.stop()
	err := t.change()
	installed := t.installed()
	config, tun := t.previous, t.wantedTUN
	if !installed || (t.removing && err == nil) {
		config, tun = t.withoutTUN, false
	}
	startErr := t.start(config)
	// Restore an ordinary proxy core if a partially successful installation
	// cannot run TUN. Keep the original failure visible even if fallback works.
	if startErr != nil && tun {
		if fallbackErr := t.start(t.withoutTUN); fallbackErr == nil {
			tun = false
		} else {
			startErr = errors.Join(startErr, fallbackErr)
		}
	}
	return serviceTransitionResult{installed: installed, tun: tun, err: errors.Join(err, startErr)}
}

func (a *App) installService()   { a.changeService("install") }
func (a *App) uninstallService() { a.changeService("uninstall") }
func (a *App) restartService()   { a.changeService("restart") }

func (a *App) changeService(action string) {
	a.bmu.Lock()
	if a.svcBusy || a.kernelBusy || a.sysBusy || a.tunChanging || a.shutdownPending.Load() {
		a.bmu.Unlock()
		return
	}
	a.svcBusy, a.svcErr = true, ""
	a.networkGeneration.Add(1)
	a.kernelStopped = false
	a.bmu.Unlock()
	finishError := func(err error) {
		a.bmu.Lock()
		a.svcBusy, a.svcErr = false, err.Error()
		a.bmu.Unlock()
		a.invalidate()
	}
	wantedTUN := a.ov.TunEnable || a.tunWanted
	ov := a.runtimeOverrides()
	ov.TunEnable = wantedTUN
	previous, err := a.configBytesFor(ov)
	if err != nil {
		finishError(err)
		return
	}
	ov.TunEnable = false
	withoutTUN, err := a.configBytesFor(ov)
	if err != nil {
		finishError(err)
		return
	}
	wantedProxy := a.sysApplied || a.healthResumePending()
	// Suspend before stopping any listener; this never changes the user's
	// restore preference and is also undone after cancellation/failure.
	if a.sysApplied {
		if err := a.applySystemProxy(false); err != nil {
			finishError(err)
			return
		}
	}
	change := func() error {
		switch action {
		case "uninstall":
			return service.Uninstall(a.dir)
		case "restart":
			return service.Restart(a.dir)
		default:
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			return service.Install(exe, a.dir)
		}
	}
	work := serviceTransition{stop: a.k.Shutdown, change: change, installed: func() bool { return service.ForDataDir(a.dir) }, start: a.k.Start,
		previous: previous, withoutTUN: withoutTUN, wantedTUN: wantedTUN, removing: action == "uninstall"}
	go func() {
		result := work.run()
		runOnMain(func() {
			a.bmu.Lock()
			a.svcInstalled = result.installed
			a.bmu.Unlock()
			a.ov.TunEnable, a.tunOn = result.tun, result.tun
			a.tunWanted = wantedTUN && !result.tun && action != "uninstall"
			if err := a.saveOverridesAtomic(); err != nil {
				result.err = errors.Join(result.err, err)
			}
			if wantedProxy && !a.shutdownPending.Load() {
				if err := a.applySystemProxy(true); err != nil {
					result.err = errors.Join(result.err, err)
				}
			}
			a.bmu.Lock()
			a.svcBusy = false
			if result.err != nil {
				a.svcErr = result.err.Error()
			} else {
				a.svcErr = ""
			}
			a.bmu.Unlock()
			if result.err == nil {
				a.setTunErr("")
			}
			a.refreshNetworkState()
			a.invalidate()
		})
	}()
}

// Three retries per failure burst; a stable healthy period resets the budget.
// The watchdog never interrupts an unresponsive user-owned process.
type kernelRecoveryPolicy struct {
	attempts           int
	next, healthySince time.Time
}

func (p *kernelRecoveryPolicy) healthy(now time.Time) {
	if p.healthySince.IsZero() {
		p.healthySince = now
	}
	if now.Sub(p.healthySince) >= 30*time.Second {
		p.attempts, p.next = 0, time.Time{}
	}
}

func (p *kernelRecoveryPolicy) retry(now time.Time) bool {
	p.healthySince = time.Time{}
	if p.attempts >= 3 || now.Before(p.next) {
		return false
	}
	delay := []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second}[p.attempts]
	p.attempts++
	p.next = now.Add(delay)
	return true
}
