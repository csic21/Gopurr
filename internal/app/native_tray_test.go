package app

import (
	"context"
	"github.com/egoist/mygo"
	"testing"
)

func TestNativeTrayCommandsPreserveCheckboxesAndRejectStaleMenus(t *testing.T) {
	nativeDispatchFixture(t)
	calls := 0
	checked := false
	menu := NewNativeTrayMenu(func() *mygo.Menu {
		return mygo.NewMenu([]*mygo.MenuItem{
			{Label: "Proxy", Type: mygo.MenuItemCheckbox, Checked: checked, Click: func(it *mygo.MenuItem, _ *mygo.Window) { checked = it.IsChecked(); calls++ }},
			{Label: "Unavailable", Disabled: true, Submenu: []*mygo.MenuItem{{Label: "Child", Click: func(*mygo.MenuItem, *mygo.Window) { calls += 100 }}}},
			{Label: "Hidden", Hidden: true, Click: func(*mygo.MenuItem, *mygo.Window) { calls += 1000 }},
			mygo.Separator(),
		})
	})
	first, err := menu.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	old := first.([]NativeTrayItem)
	if len(old) != 3 || old[0].Command == "" || old[1].Submenu[0].Command != "" || !old[1].Submenu[0].Disabled {
		t.Fatal(old)
	}
	second, err := menu.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = menu.Activate(context.Background(), old[0].Command); err == nil {
		t.Fatal("old menu command accepted")
	}
	command := second.([]NativeTrayItem)[0].Command
	if _, err = menu.Activate(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	if !checked || calls != 1 {
		t.Fatal("checkbox was not toggled before dispatch", checked, calls)
	}
	if _, err = menu.Activate(context.Background(), command); err == nil || calls != 1 {
		t.Fatal("repeated command executed")
	}
}
