package app

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"gopurr/internal/sysproxy"
)

type proxyControllerFixture struct {
	enable  func(int, *sysproxy.State) error
	restore func(int, *sysproxy.State) error
	close   func() error
}

func (p proxyControllerFixture) Enable(port int, previous *sysproxy.State) error {
	return p.enable(port, previous)
}
func (p proxyControllerFixture) Restore(port int, previous *sysproxy.State) error {
	return p.restore(port, previous)
}
func (p proxyControllerFixture) Close() error {
	if p.close != nil {
		return p.close()
	}
	return nil
}

// Only used with fakeSettingsSystemProxy/fakeTraySystemProxy's private PATH.
// No real osascript or networksetup can be reached by these fixture operations.
type scriptedProxyFixture struct{}

func (scriptedProxyFixture) Enable(port int, _ *sysproxy.State) error {
	return runProxyFixture("networksetup -setwebproxy Wi-Fi 127.0.0.1 " + strconv.Itoa(port) +
		"; networksetup -setsocksfirewallproxy Wi-Fi 127.0.0.1 " + strconv.Itoa(port) + ";")
}
func (scriptedProxyFixture) Restore(_ int, previous *sysproxy.State) error {
	if previous != nil && previous.Enabled {
		return runProxyFixture("networksetup -setwebproxy Wi-Fi " + previous.Server + " " + strconv.Itoa(previous.Port))
	}
	return runProxyFixture("networksetup -setwebproxystate Wi-Fi off")
}
func (scriptedProxyFixture) Close() error { return nil }

func runProxyFixture(script string) error {
	output, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil && strings.Contains(string(output), "canceled") {
		return errors.New("已取消授权")
	}
	return err
}

func TestShutdownPreservesRecoveryStateAndKernelOnRestoreFailure(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS proxy fixtures")
	}
	a, _ := newSettingsTestApp(t, nil)
	fakeSettingsSystemProxy(t, a, a.mixedPort(), false)
	previous := &sysproxy.State{Enabled: true, Server: "previous.example", Port: 8080}
	a.prev, a.sysApplied, a.sysOn = previous, true, true
	a.savePrevProxy()
	backup, err := os.ReadFile(a.prevPath)
	if err != nil {
		t.Fatal(err)
	}
	fail, closed, updatesStopped := true, false, false
	a.stopSoftwareUpdates = func() { updatesStopped = true }
	a.proxy = proxyControllerFixture{
		restore: func(gotPort int, got *sysproxy.State) error {
			if gotPort != a.mixedPort() {
				t.Fatalf("restore port = %d, want %d", gotPort, a.mixedPort())
			}
			if got == nil || *got != *previous {
				t.Fatal("previous proxy was discarded before restore")
			}
			if fail {
				return errors.New("fixture restore failed")
			}
			return nil
		},
		close: func() error { closed = true; return nil },
	}
	if err := a.Shutdown(); err == nil {
		t.Fatal("quit was allowed after a failed restore")
	}
	after, err := os.ReadFile(a.prevPath)
	if err != nil || string(after) != string(backup) || a.prev != previous || !a.sysApplied || !a.sysOn {
		t.Fatalf("recovery state lost: prev=%+v sysApplied=%v backup=%s err=%v", a.prev, a.sysApplied, after, err)
	}
	if !a.k.Running() || closed || updatesStopped || a.page != "overview" || !strings.Contains(a.sysErrText(), "fixture restore failed") {
		t.Fatal("failed restore did not keep the application usable")
	}
	fail = false
	if err := a.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if !closed || !updatesStopped || a.prev != nil || a.sysApplied {
		t.Fatal("successful retry did not finish shutdown")
	}
	if _, err := os.Stat(a.prevPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("successful restore retained an obsolete recovery record")
	}
}

func TestShutdownChecksPendingHelperCleanupEvenWhenProxySwitchIsOff(t *testing.T) {
	closed := 0
	a := &App{proxy: proxyControllerFixture{close: func() error {
		closed++
		return errors.New("pending proxy cleanup failed")
	}}}
	if err := a.Shutdown(); err == nil || closed != 1 || a.sysErrText() == "" {
		t.Fatal("pending helper cleanup failure was ignored")
	}
}
