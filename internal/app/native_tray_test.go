package app

import (
	"context"
	"testing"
)

func TestNativeTrayCommandsPreserveCheckboxesAndRejectStaleMenus(t *testing.T) {
	nativeDispatchFixture(t)
	calls := 0
	checked := false
	menu := NewNativeTrayMenu(func() *Menu {
		return NewMenu([]*MenuItem{
			{Label: "Proxy", Type: MenuItemCheckbox, Checked: checked, Click: func(it *MenuItem, _ *Window) { checked = it.IsChecked(); calls++ }},
			{Label: "Unavailable", Disabled: true, Submenu: []*MenuItem{{Label: "Child", Click: func(*MenuItem, *Window) { calls += 100 }}}},
			{Label: "Hidden", Hidden: true, Click: func(*MenuItem, *Window) { calls += 1000 }},
			Separator(),
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
