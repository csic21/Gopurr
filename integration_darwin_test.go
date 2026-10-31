//go:build darwin && gopurr_native

package main

import (
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
	"gopurr/internal/shortcuts"
)

func TestNativeShortcutRegistrationSurvivesWindowRelease(t *testing.T) {
	const key = "Ctrl+Alt+Shift+F12"
	var err error
	mygo.RunOnMain(func() {
		lifecycleApp.ConfigureShortcuts(mygo.GlobalShortcut, func() { showWindow(lifecycleApp) })
		p := lifecycleApp.Prefs()
		p.Shortcuts[shortcuts.ShowWindow] = key
		err = lifecycleApp.UpdatePrefs(p)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		mygo.RunOnMain(func() {
			p := lifecycleApp.Prefs()
			p.Shortcuts = shortcuts.Config{}
			if err := lifecycleApp.UpdatePrefs(p); err != nil {
				t.Error(err)
			}
			lifecycleApp.CloseShortcuts()
			if mainWin != nil {
				mainWin.Destroy()
				mainWin = nil
			}
		})
	})
	setWindowPrefs(t, "show", true)
	showWindow(lifecycleApp)
	mygo.RunOnMain(func() { mainWin.Close() })
	waitWindow(t, "window release with a global shortcut", func(s nativeWindowState) bool { return s.window == nil })
	if !mygo.GlobalShortcut.IsRegistered(key) {
		t.Fatal("window release removed the global shortcut")
	}
	// Exercise the window action while no native window exists. Registration
	// uses the real Carbon backend; dispatch behavior is also covered by fakes.
	mygo.RunOnMain(func() { lifecycleApp.RunShortcut(shortcuts.ShowWindow, func() { showWindow(lifecycleApp) }) })
	if !windowState().visible {
		t.Fatal("shortcut could not recreate the window")
	}
	setWindowPrefs(t, "show", false)
}

func TestNativeURLDeliveryReusesWindow(t *testing.T) {
	t.Cleanup(func() {
		mygo.RunOnMain(func() {
			if mainWin != nil {
				mainWin.Destroy()
				mainWin = nil
			}
		})
	})
	showWindow(lifecycleApp)
	w := windowState().window
	got := make(chan string, 1)
	mygo.RunOnMain(func() { installImportLifecycle(lifecycleApp) })
	off := mygo.App.OnOpenURL(func(raw string) { got <- raw })
	defer off()
	const raw = "gopurr://install-config?url=https%3A%2F%2Fsubscription.example.com%2Fsub&name=Native"
	mygo.RunOnMain(func() {
		// This is the same AppKit delegate event used by Launch Services.
		str := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), raw)
		u := objc.ID(objc.GetClass("NSURL")).Send(objc.RegisterName("URLWithString:"), str)
		urls := objc.ID(objc.GetClass("NSArray")).Send(objc.RegisterName("arrayWithObject:"), u)
		nsApp := objc.ID(objc.GetClass("NSApplication")).Send(objc.RegisterName("sharedApplication"))
		delegate := nsApp.Send(objc.RegisterName("delegate"))
		delegate.Send(objc.RegisterName("application:openURLs:"), nsApp, urls)
	})
	select {
	case received := <-got:
		if received != raw {
			t.Fatalf("URL changed: %s", received)
		}
	case <-time.After(time.Second):
		t.Fatal("AppKit URL was not delivered")
	}
	if state := windowState(); !state.visible || state.window != w {
		t.Fatal("URL opened a duplicate window")
	}
}
