package app

import (
	"context"
	"sync/atomic"
	"time"

	"gopurr/internal/core"
	"gopurr/internal/sysproxy"
)

type networkState struct {
	networkGeneration   atomic.Uint64
	networkRuntime      core.RuntimeStatus
	networkChecked      time.Time
	networkErr          string
	proxyChanged        bool
	proxyReadErr        string
	healthProxyBaseline sysproxy.State
	healthBaselineValid bool
	showNetworkDetails  bool
	kernelStopped       bool
	tunChanging         bool
	recovery            kernelRecoveryPolicy
}

func (a *App) networkFresh() bool {
	return !a.networkChecked.IsZero() && time.Since(a.networkChecked) < 10*time.Second && a.networkErr == ""
}

func (a *App) acceptNetworkObservation(status core.RuntimeStatus, statusErr error, proxy sysproxy.State, proxyErr error) {
	if hard, _ := a.kernelHealthBusyState(); hard {
		return
	}
	a.networkRuntime, a.networkChecked, a.networkErr = status, time.Now(), ""
	if statusErr != nil {
		a.networkErr = tr("实际状态暂不可用，请检查内核或更新服务")
	}
	if proxyErr == nil && proxy.Supported {
		a.proxyReadErr = ""
		applied := ownsProxy(proxy, a.mixedPort())
		if a.sysApplied && !applied {
			a.proxyChanged = true
		}
		a.sysState, a.sysApplied, a.sysOn = proxy, applied, applied
	} else if proxyErr != nil {
		a.proxyReadErr = proxyErr.Error()
	}
	if statusErr == nil && status.ProxyReady && (!a.ov.TunEnable || status.TunReady) {
		a.recovery.healthy(time.Now())
	}
	a.invalidate()
}

func (a *App) recoverFailedKernel(exited, localFailure bool) {
	if a.kernelStopped || a.shutdownPending.Load() || a.kernelHealthBusy() || (!exited && !localFailure) || !a.recovery.retry(time.Now()) {
		return
	}
	a.startKernelAsync(func(ok bool) {
		if ok && a.healthResumePending() {
			a.resumeProxyAfterKernelRecovery()
		}
	})
}

func (a *App) refreshNetworkState() {
	if a.k == nil {
		return
	}
	generation := a.networkGeneration.Load()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), kernelProbeTimeout)
		defer cancel()
		status, err := a.k.Runtime(ctx)
		proxy, proxyErr := a.currentSystemProxy()
		runOnMain(func() {
			if generation == a.networkGeneration.Load() {
				a.acceptNetworkObservation(status, err, proxy, proxyErr)
			}
		})
	}()
}
