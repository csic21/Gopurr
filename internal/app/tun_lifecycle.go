package app

// UI actions use the asynchronous path; configuration transactions retain the
// synchronous applyTun helper. The kernel verifies the actual device/routes
// before either path commits the preference.
func (a *App) requestTun(on bool, after ...func()) {
	notify := func() {
		for _, fn := range after {
			fn()
		}
	}
	if a.kernelHealthBusy() || a.shutdownPending.Load() {
		a.tunOn = a.ov.TunEnable
		notify()
		return
	}
	if on && !a.k.IsRoot() {
		a.tunOn = a.ov.TunEnable
		a.setTunErr(tr("TUN 需要系统服务：先在概览页点「安装服务…」"))
		notify()
		return
	}
	a.tunWanted = false
	a.bmu.Lock()
	a.tunChanging = true
	a.bmu.Unlock()
	complete := func(ready bool) {
		a.bmu.Lock()
		a.tunChanging = false
		a.bmu.Unlock()
		// Release the transition before restoring the system proxy; otherwise
		// setSystemProxy rejects its own successful boot as a busy operation.
		if ready {
			a.finishKernelBoot()
			if a.booted {
				a.refreshNetworkState()
			}
		}
		notify()
		a.invalidate()
	}
	previous := a.ov.TunEnable
	a.ov.TunEnable, a.tunOn = on, on
	a.setTunErr("")
	a.startKernelAsync(func(ok bool) {
		if !ok {
			a.ov.TunEnable, a.tunOn = previous, previous
			a.setTunErr(a.notice)
			complete(false)
			return
		}
		if err := a.saveOverridesAtomic(); err != nil {
			a.ov.TunEnable, a.tunOn = previous, previous
			message := tr("TUN 设置保存失败: ") + err.Error()
			a.startKernelAsync(func(restored bool) {
				if !restored {
					a.ov.TunEnable, a.tunOn = on, on
					message += tr("；恢复原内核配置失败: ") + a.notice
				} else {
					message += tr("；已恢复原设置")
				}
				a.setTunErr(message)
				complete(false)
			})
			return
		}
		complete(true)
	})
}
