package app

import (
	"fmt"

	"gopurr/internal/patch"
)

type networkSettingsChange struct {
	previous, next       patch.Overrides
	previousRaw, nextRaw []byte
	path                 string
	previousPort         int
	running              func() bool
	start                func([]byte) error
	proxy                *proxyChange
}

type networkSettingsResult struct {
	running bool
	err     error
	proxy   *proxyChangeResult
}

// The listener, preferences and OS proxy form one transaction. Neither a slow
// reload nor a port-change authorization may block the event loop. Rollback
// restores the listener before repointing the proxy to the old port.
func (w networkSettingsChange) run() (r networkSettingsResult) {
	if w.next.MixedPort != w.previousPort && !portFree(w.next.MixedPort) {
		r.err = fmt.Errorf(tr("端口 %d 已被占用，请更换端口"), w.next.MixedPort)
		return
	}
	r.running = w.running != nil && w.running()
	rollback := func(failure string) {
		if r.running {
			if err := w.start(w.previousRaw); err != nil {
				failure += tr("；恢复原内核配置失败：") + err.Error()
			}
		}
		r.err = fmt.Errorf("%s", failure)
	}
	if r.running {
		if err := w.start(w.nextRaw); err != nil {
			rollback(tr("网络配置重载失败：") + err.Error())
			return
		}
	}
	if err := writeSettingsFile(w.path, w.next); err != nil {
		rollback(tr("网络设置保存失败：") + err.Error())
		return
	}
	if w.proxy != nil && w.next.MixedPort != w.previousPort {
		proxy := *w.proxy
		proxy.port = w.next.MixedPort
		result := proxy.run()
		r.proxy = &result
		if result.err != nil {
			failure := tr("系统代理端口更新失败：") + result.err.Error()
			if err := writeSettingsFile(w.path, w.previous); err != nil {
				failure += tr("；恢复原网络设置文件失败：") + err.Error()
			}
			rollback(failure)
			original := w.proxy.run()
			r.proxy = &original
			if original.err != nil {
				r.err = fmt.Errorf("%s%s%s", r.err.Error(), tr("；恢复原系统代理失败："), original.err.Error())
			}
		}
	}
	return
}

func (a *App) prepareNetworkSettings() (networkSettingsChange, error) {
	a.networkError, a.networkStatus = "", ""
	port, err := parseSettingsPort(a.mixedPortDraft)
	if err != nil {
		return networkSettingsChange{}, err
	}
	if !containsString(settingsTunStacks, a.tunStackPick) {
		return networkSettingsChange{}, fmt.Errorf("%s", tr("TUN 协议栈须为 mixed、system 或 gvisor"))
	}
	if port != a.mixedPort() && !portFree(port) {
		return networkSettingsChange{}, fmt.Errorf(tr("端口 %d 已被占用，请更换端口"), port)
	}
	if a.remoteBase.Enabled && a.remoteBase.Port == port {
		return networkSettingsChange{}, fmt.Errorf(tr("远程 Web 控制台正在使用端口 %d"), port)
	}
	w := networkSettingsChange{previous: a.ov, next: a.ov, path: a.ovPath, previousPort: a.mixedPort()}
	w.next.MixedPort = port
	w.next.AllowLAN, w.next.IPv6, w.next.Unified = a.lanOn, a.ipv6On, a.unifiedOn
	w.next.TunStack = a.tunStackPick
	w.nextRaw, err = a.configBytesFor(w.next)
	if err == nil {
		w.previousRaw, err = a.configBytesFor(w.previous)
	}
	if err != nil {
		return w, fmt.Errorf("%s%w", tr("网络配置校验失败："), err)
	}
	if a.k != nil {
		w.running, w.start = a.k.Running, a.k.Start
	}
	if a.sysApplied {
		proxy := a.proxyChangeWork(true)
		w.proxy = &proxy
	}
	return w, nil
}

func (a *App) commitNetworkSettings(w networkSettingsChange, r networkSettingsResult) bool {
	if r.proxy != nil {
		a.commitProxyChange(true, *r.proxy)
	}
	if r.err != nil {
		a.networkError, a.notice = r.err.Error(), r.err.Error()
		return false
	}
	a.ov = w.next
	a.resetNetworkDraft()
	a.networkStatus = tr("网络设置已保存并应用")
	if !r.running {
		a.networkStatus = tr("网络设置已保存，内核就绪后生效")
	}
	a.notice = a.networkStatus
	return true
}

func (a *App) requestNetworkSettings() {
	if busyNetwork(a) || a.shutdownPending.Load() {
		return
	}
	w, err := a.prepareNetworkSettings()
	if err != nil {
		a.networkError, a.notice = err.Error(), err.Error()
		return
	}
	a.bmu.Lock()
	a.sysBusy = true
	a.bmu.Unlock()
	a.networkGeneration.Add(1)
	go func() {
		r := w.run()
		runOnMain(func() {
			a.commitNetworkSettings(w, r)
			a.bmu.Lock()
			a.sysBusy = false
			a.bmu.Unlock()
			a.invalidate()
		})
	}()
}

// Synchronous entry for transaction tests and callers without an event loop.
func (a *App) saveNetworkSettings() bool {
	w, err := a.prepareNetworkSettings()
	if err != nil {
		a.networkError, a.notice = err.Error(), err.Error()
		return false
	}
	return a.commitNetworkSettings(w, w.run())
}
