//go:build darwin && gopurr_native

package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"

	"gopurr/internal/app"
)

// Opt in with `make test-native`. These tests open real AppKit
// windows with an isolated home, fake controller and fake OS proxy commands.
var lifecycleApp *app.App

func TestMain(m *testing.M) { os.Exit(runWindowTests(m)) }

func runWindowTests(m *testing.M) int {
	dir, err := os.MkdirTemp("/tmp", "gopurr-window-tests-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(dir)
	fakebin := filepath.Join(dir, "bin")
	if err := os.Mkdir(fakebin, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for name, body := range map[string]string{
		"route":        "#!/bin/sh\nprintf 'interface: en0\\n'\n",
		"networksetup": "#!/bin/sh\ncase \"$1\" in\n-listnetworkserviceorder) printf '(1) Wi-Fi\\n(Hardware Port: Wi-Fi, Device: en0)\\n';;\n-getwebproxy) printf 'Enabled: No\\n';;\nesac\n",
		"osascript":    "#!/bin/sh\nexit 1\n",
	} {
		if err := os.WriteFile(filepath.Join(fakebin, name), []byte(body), 0o700); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	for key, value := range map[string]string{"GOPURR_HOME": dir, "GOPURR_AUTOSTART": "0", "PATH": fakebin} {
		if err := os.Setenv(key, value); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	lifecycleApp = app.New()
	p := lifecycleApp.Prefs()
	p.CheckAppUpdates = false
	if err := lifecycleApp.UpdatePrefs(p); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ln, err := net.Listen("unix", filepath.Join(dir, "core.sock"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			fmt.Fprint(w, `{"version":"window-test"}`)
		case "/proxies":
			fmt.Fprint(w, `{"proxies":{}}`)
		case "/connections":
			fmt.Fprint(w, `{"connections":[]}`)
		case "/rules":
			fmt.Fprint(w, `{"rules":[]}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	})}
	go srv.Serve(ln)
	defer srv.Close()
	mygo.App.SetName("Gopurr Window Tests")
	mygo.App.SetPath(mygo.PathUserData, dir)
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		quitting.Store(true)
		cancelRelease()
		lifecycleApp.Shutdown()
	})
	result := make(chan int, 1)
	mygo.App.WhenReady(func() {
		installWindowLifecycle(lifecycleApp)
		go func() {
			result <- m.Run()
			mygo.App.Quit()
		}()
	})
	if err := mygo.App.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return <-result
}

type nativeWindowState struct {
	window    *mygo.Window
	visible   bool
	minimized bool
	dock      bool
}

func windowState() (state nativeWindowState) {
	mygo.RunOnMain(func() {
		state.window, state.visible = mainWin, windowVisible()
		if mainWin != nil && !mainWin.IsDestroyed() {
			state.minimized = mainWin.IsMinimized()
		}
		nsApp := objc.ID(objc.GetClass("NSApplication")).Send(objc.RegisterName("sharedApplication"))
		state.dock = nsApp.Send(objc.RegisterName("activationPolicy")) == 0
	})
	return
}

func nativeReopen() {
	mygo.RunOnMain(func() {
		nsApp := objc.ID(objc.GetClass("NSApplication")).Send(objc.RegisterName("sharedApplication"))
		delegate := nsApp.Send(objc.RegisterName("delegate"))
		// The exact AppKit delegate event sent by clicking the Dock icon.
		objc.Send[bool](delegate, objc.RegisterName("applicationShouldHandleReopen:hasVisibleWindows:"), nsApp, windowVisible())
	})
}

func waitWindow(t *testing.T, description string, check func(nativeWindowState) bool) nativeWindowState {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		state := windowState()
		if check(state) {
			return state
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s: %+v", description, windowState())
	return nativeWindowState{}
}

func setWindowPrefs(t *testing.T, mode string, light bool) {
	t.Helper()
	p := lifecycleApp.Prefs()
	p.DockVisibility, p.LightMode, p.LightDelay = mode, light, 1
	if err := lifecycleApp.UpdatePrefs(p); err != nil {
		t.Fatal(err)
	}
}

func TestNativeSilentStartupRunsWithoutWindow(t *testing.T) {
	p := lifecycleApp.Prefs()
	p.SilentStart = true
	if err := lifecycleApp.UpdatePrefs(p); err != nil {
		t.Fatal(err)
	}
	defer func() { p.SilentStart = false; _ = lifecycleApp.UpdatePrefs(p) }()
	mygo.RunOnMain(func() { openInitialWindow(lifecycleApp, true) })
	if state := windowState(); state.window != nil || state.visible {
		t.Fatalf("silent startup created a window: %+v", state)
	}
	if lifecycleApp.StartupNeedsAttention() {
		t.Fatal("background startup failed")
	}
	nativeReopen()
	if !windowState().visible {
		t.Fatal("silent app could not reopen from Dock")
	}
	mygo.RunOnMain(func() { mainWin.Destroy(); mainWin = nil })
}

func TestNativeWindowDockLifecycle(t *testing.T) {
	for _, light := range []bool{false, true} {
		for _, mode := range []string{app.DockShown, app.DockFollowWindow, app.DockHidden} {
			t.Run(fmt.Sprintf("%s/light=%v", mode, light), func(t *testing.T) {
				setWindowPrefs(t, mode, light)
				showWindow(lifecycleApp)
				opened := windowState()
				if !opened.visible || opened.dock != (mode != app.DockHidden) {
					t.Fatalf("incorrect opened state: %+v", opened)
				}
				mygo.RunOnMain(func() { mainWin.Close() })
				closed := windowState()
				if closed.visible || closed.window != opened.window || closed.dock != (mode == app.DockShown) {
					t.Fatalf("incorrect closed state: %+v", closed)
				}
				if light {
					freed := waitWindow(t, "window release", func(s nativeWindowState) bool { return s.window == nil })
					if freed.dock != (mode == app.DockShown) {
						t.Fatalf("light mode changed Dock policy: %+v", freed)
					}
				}
				nativeReopen()
				reopened := windowState()
				if !reopened.visible || reopened.dock != (mode != app.DockHidden) || (light && reopened.window == opened.window) || (!light && reopened.window != opened.window) {
					t.Fatalf("Dock reopen failed: %+v", reopened)
				}
			})
		}
	}
}

func TestNativeWindowRestoresMinimizedWindow(t *testing.T) {
	setWindowPrefs(t, app.DockShown, false)
	showWindow(lifecycleApp)
	w := windowState().window
	w.Minimize()
	waitWindow(t, "minimization", func(s nativeWindowState) bool { return s.minimized })
	nativeReopen()
	state := waitWindow(t, "restore", func(s nativeWindowState) bool { return s.visible && !s.minimized })
	if state.window != w {
		t.Fatal("restoring created a duplicate window")
	}
}

func TestNativeWindowIgnoresStaleRelease(t *testing.T) {
	setWindowPrefs(t, app.DockShown, true)
	showWindow(lifecycleApp)
	w := windowState().window
	mygo.RunOnMain(func() {
		mainWin.Close()
		// Hold the UI thread until the old release callback has been posted,
		// then reopen and close again before that callback gets to run.
		time.Sleep(1200 * time.Millisecond)
		showWindow(lifecycleApp)
		mainWin.Close()
	})
	time.Sleep(100 * time.Millisecond)
	if state := windowState(); state.window != w || state.visible {
		t.Fatalf("stale callback released the newly closed window early: %+v", state)
	}
	nativeReopen()
	time.Sleep(1200 * time.Millisecond)
	if state := windowState(); state.window != w || !state.visible {
		t.Fatalf("reopening did not cancel release: %+v", state)
	}
	mygo.RunOnMain(func() { mainWin.Close() })
	setWindowPrefs(t, app.DockShown, false)
	time.Sleep(1200 * time.Millisecond)
	if state := windowState(); state.window != w || state.visible {
		t.Fatalf("disabling light mode did not cancel release: %+v", state)
	}
}
