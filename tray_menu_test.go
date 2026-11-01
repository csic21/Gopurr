package main

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo"
)

type fakeTray struct {
	menu    *mygo.Menu
	popped  []string
	onClick []func()
	onRight []func()
}

func (f *fakeTray) SetMenu(m *mygo.Menu) { f.menu = m }

func (f *fakeTray) PopUpMenu() {
	if f.menu == nil {
		f.popped = append(f.popped, "<nil>")
		return
	}
	f.popped = append(f.popped, f.menu.Items()[0].Label)
}

func (f *fakeTray) OnClick(fn func()) func() { f.onClick = append(f.onClick, fn); return func() {} }

func (f *fakeTray) OnRightClick(fn func()) func() {
	f.onRight = append(f.onRight, fn)
	return func() {}
}

// Every right click must show a menu built from the state at that moment,
// not a snapshot taken at startup (the Windows tray used to keep its first
// menu: unchecked System Proxy and disabled TUN while the window was live).
func TestTrayMenuRebuiltOnEveryOpen(t *testing.T) {
	tray := &fakeTray{}
	builds, opened := 0, 0
	attachTrayMenu(tray, func() *mygo.Menu {
		builds++
		return mygo.NewMenu([]*mygo.MenuItem{{Label: fmt.Sprintf("state %d", builds)}})
	}, func() { opened++ })

	if tray.menu != nil || builds != 0 {
		t.Fatal("a menu snapshot was attached before any opening")
	}
	if len(tray.onClick) != 1 || len(tray.onRight) != 1 {
		t.Fatalf("handlers: click=%d right=%d", len(tray.onClick), len(tray.onRight))
	}
	for i := 1; i <= 3; i++ {
		tray.onRight[0]()
		if builds != i || tray.popped[i-1] != fmt.Sprintf("state %d", i) {
			t.Fatalf("opening %d showed %q after %d builds", i, tray.popped[i-1], builds)
		}
		// Nothing stays attached for the native tray to pop by itself.
		if tray.menu != nil {
			t.Fatalf("opening %d left a menu attached", i)
		}
	}
	tray.onClick[0]()
	if opened != 1 || builds != 3 {
		t.Fatalf("left click: opened=%d builds=%d", opened, builds)
	}
}

func TestTrayMenuIgnoresReentrantOpen(t *testing.T) {
	tray := &fakeTray{}
	builds := 0
	attachTrayMenu(tray, func() *mygo.Menu {
		builds++
		if builds == 1 {
			tray.onRight[0]() // a second right click while the first menu tracks
		}
		return mygo.NewMenu([]*mygo.MenuItem{{Label: "state"}})
	}, func() {})
	tray.onRight[0]()
	if builds != 1 || len(tray.popped) != 1 {
		t.Fatalf("reentrant open: builds=%d popups=%d", builds, len(tray.popped))
	}
}
