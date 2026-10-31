package app

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestStartKernelAsyncOccupiesBusyAndSurfacesPortError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	t.Setenv("GOPURR_HOME", t.TempDir())
	a := New()
	a.ov.MixedPort = port

	done := make(chan bool, 1)
	a.startKernelAsync(func(ok bool) { done <- ok })
	if a.kernelBusyState() {
		t.Fatal("port check should finish synchronously without leaving busy set")
	}
	select {
	case ok := <-done:
		if ok {
			t.Fatal("expected port conflict to fail start")
		}
	case <-time.After(time.Second):
		t.Fatal("callback not invoked for sync validation failure")
	}
	if !strings.Contains(a.notice, fmt.Sprintf("端口 %d 已被占用", port)) {
		t.Fatalf("missing port notice: %q", a.notice)
	}
}

func TestStartKernelAsyncClearsBusyOnSuccess(t *testing.T) {
	a, _ := newSettingsTestApp(t, nil)
	done := make(chan bool, 1)
	a.startKernelAsync(func(ok bool) { done <- ok })
	select {
	case ok := <-done:
		if !ok {
			t.Fatalf("async start failed: %q", a.notice)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("async start timed out")
	}
	if a.kernelBusyState() {
		t.Fatal("kernelBusy remained set after completion")
	}
	if !a.k.Running() {
		t.Fatal("kernel not running after async start")
	}
}

func TestKernelButtonShowsStartingWhileBusy(t *testing.T) {
	t.Setenv("GOPURR_HOME", t.TempDir())
	a := New()
	a.bmu.Lock()
	a.kernelBusy = true
	a.bmu.Unlock()
	tr := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme(c.Theme().Dark))
		a.kernelButton(c, false)
	}, 400, 80)
	tr.Frame()
	if _, ok := tr.Find("正在启动…"); !ok {
		t.Fatal("busy start state not shown on kernel button")
	}
	if _, ok := tr.Find("启动内核"); ok {
		t.Fatal("start action remained clickable while busy")
	}
}

func TestToggleIgnoresDoubleStartWhileBusy(t *testing.T) {
	a, reloads := newSettingsTestApp(t, nil)
	a.bmu.Lock()
	a.kernelBusy = true
	a.bmu.Unlock()
	before := len(reloads())
	a.toggle()
	if len(reloads()) != before {
		t.Fatal("toggle started another reload while kernelBusy")
	}
	if a.notice != "" && strings.Contains(a.notice, "启动失败") {
		t.Fatalf("toggle should no-op while busy, got notice %q", a.notice)
	}
}

func TestTrayReloadDisabledWhileKernelBusy(t *testing.T) {
	t.Setenv("GOPURR_HOME", t.TempDir())
	a := New()
	a.bmu.Lock()
	a.kernelBusy = true
	a.bmu.Unlock()
	items := a.trayItems(false, nil, func() {})
	reload := findTrayItem(t, items, "reload")
	if !reload.Disabled || reload.Label != "正在启动…" {
		t.Fatalf("reload item = label:%q disabled:%v", reload.Label, reload.Disabled)
	}
}
