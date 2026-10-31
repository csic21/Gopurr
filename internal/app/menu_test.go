package app

import (
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gopurr/internal/update"
)

func TestMenuCheckUpdatesOpensWindowAndRequiresConfirmation(t *testing.T) {
	for _, source := range []string{"application", "tray"} {
		t.Run(source, func(t *testing.T) {
			a, _ := newSettingsTestApp(t, nil)
			a.booted, a.ticking = true, true
			var installs atomic.Int32
			a.softwareUpdates = update.New(fixtureUpdateBackend{phase: update.Ready, installed: &installs}, nil)
			t.Cleanup(a.softwareUpdates.Close)
			opened := 0
			openWindow := func() {
				opened++
				if a.page != "settings" || a.settingsTab != 3 {
					t.Fatal("update destination was not set before reopening the window")
				}
			}
			var item *mygo.MenuItem
			if source == "application" {
				menu := a.ApplicationMenu(openWindow)
				item = menu.ItemByID("check-updates")
				items := menu.Items()[0].Submenu
				if items[0].Role != mygo.RoleAbout || items[1] != item || items[2].Type != mygo.MenuItemSeparator {
					t.Fatal("update action must follow About, before the separator")
				}
			} else {
				item = findTrayItem(t, a.trayItems(false, nil, openWindow), "check-updates")
			}
			clickTrayItem(t, item)
			waitSoftwareUpdate(t, a, update.Available)
			tr := ui.NewTester(a.View, 840, 560)
			tr.Frame()
			if _, ok := tr.Find("下载并安装"); !ok || opened != 1 || installs.Load() != 0 {
				t.Fatal("manual check did not show the update confirmation without installing")
			}
			// Reopening during download or after staging must preserve that work.
			for _, phase := range []update.Phase{update.Downloading, update.Ready} {
				fixtureSoftwareUpdate(t, a, phase)
				a.softwareUpdates.Hide()
				clickTrayItem(t, item)
				s := a.softwareUpdates.Snapshot()
				if !s.Visible || s.Phase != phase {
					t.Fatalf("reopening changed active update: %+v", s)
				}
			}
		})
	}
}
