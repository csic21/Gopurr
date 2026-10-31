package app

import "github.com/egoist/mygo"

// ApplicationMenu keeps the standard macOS menus and adds manual update checks
// beside About, where users expect application-level actions.
func (a *App) ApplicationMenu(openWindow func()) *mygo.Menu {
	checkUpdates := a.checkUpdatesMenuItem(openWindow)
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu, Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			checkUpdates,
			mygo.Separator(),
			{Role: mygo.RoleServices},
			mygo.Separator(),
			{Role: mygo.RoleHide},
			{Role: mygo.RoleHideOthers},
			{Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Role: mygo.RoleFileMenu},
		{Role: mygo.RoleEditMenu},
		{Role: mygo.RoleViewMenu},
		{Role: mygo.RoleWindowMenu},
	})
}

func (a *App) checkUpdatesMenuItem(openWindow func()) *mygo.MenuItem {
	return &mygo.MenuItem{ID: "check-updates", Label: tr("检查软件更新…"), Click: func(*mygo.MenuItem, *mygo.Window) {
		a.initSoftwareUpdates()
		a.page, a.settingsTab = "settings", 3
		openWindow()
		a.softwareUpdates.Check(true)
	}}
}
