package app

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/core"
	"gopurr/internal/sysproxy"
)

func TestNetworkOverviewHidesEndpointUntilDetails(t *testing.T) {
	a := subscriptionTestApp(t)
	a.booted, a.ticking = true, true
	a.sysApplied, a.sysOn = true, true
	a.sysState = sysproxy.State{Supported: true, Enabled: true, Server: "127.0.0.1", Port: 7890, Service: "Ethernet"}
	a.networkRuntime, a.networkChecked = fixtureRuntime("mixed-port: 7890\n"), time.Now()
	tr := ui.NewTester(a.View, 840, 560)
	tr.SetPreferences(ui.Preferences{ReduceMotion: true})
	tr.Frame()
	if strings.Contains(a.sysProxyHint(), "127.0.0.1") || strings.Contains(a.sysProxyHint(), "7890") || !tr.HasText("已开启 · 本地代理就绪") {
		t.Fatal("overview exposes endpoint or hides verified state")
	}
	a.showNetworkDetails = true
	tr.Frame()
	if !tr.HasText("127.0.0.1:7890") || !tr.HasText("网络详情") {
		t.Fatal("endpoint missing from details")
	}
	for _, label := range []string{"关闭", "重新检测"} {
		r, ok := tr.Find(label)
		if !ok || r.Y+r.H > 560 || r.X+r.W > 840 {
			t.Fatalf("details action clipped: %s %+v", label, r)
		}
	}
}

func TestObservedProxyDriftDoesNotFightOtherClient(t *testing.T) {
	a := subscriptionTestApp(t)
	a.sysApplied, a.sysOn = true, true
	a.acceptNetworkObservation(core.RuntimeStatus{MixedPort: 7890, ProxyReady: true}, nil,
		sysproxy.State{Supported: true, Enabled: true, Server: "other-client", Port: 8080}, nil)
	if a.sysApplied || a.sysOn || !a.proxyChanged || !strings.Contains(a.sysProxyHint(), "其他程序") {
		t.Fatal("foreign system proxy change not reflected")
	}
}

func TestUserStopPreventsAutomaticRestart(t *testing.T) {
	a, reloads := newSettingsTestApp(t, nil)
	a.kernelStopped = true
	a.recoverFailedKernel(true, false)
	if a.kernelBusyState() || len(reloads()) != 0 {
		t.Fatal("watchdog undid an explicit stop")
	}
}
