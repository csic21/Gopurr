package app

// Menu is the tray and application menu model. The NSH host draws it;
// nothing here talks to a widget toolkit.
type MenuItemType string

const (
	MenuItemNormal    MenuItemType = "normal"
	MenuItemSeparator MenuItemType = "separator"
	MenuItemCheckbox  MenuItemType = "checkbox"
	MenuItemRadio     MenuItemType = "radio"
)

type MenuRole string

const (
	RoleAbout      MenuRole = "about"
	RoleAppMenu    MenuRole = "appMenu"
	RoleServices   MenuRole = "services"
	RoleHide       MenuRole = "hide"
	RoleHideOthers MenuRole = "hideOthers"
	RoleUnhide     MenuRole = "unhide"
	RoleQuit       MenuRole = "quit"
	RoleFileMenu   MenuRole = "fileMenu"
	RoleEditMenu   MenuRole = "editMenu"
	RoleViewMenu   MenuRole = "viewMenu"
	RoleWindowMenu MenuRole = "windowMenu"
)

// Window is unused. Click keeps the old two-argument shape so call sites
// stay mechanical; the host has the only real window.
type Window struct{}

type MenuItem struct {
	ID, Label, Accelerator, ToolTip string
	Type                            MenuItemType
	Role                            MenuRole
	Disabled, Hidden, Checked       bool
	Submenu                         []*MenuItem
	Click                           func(*MenuItem, *Window)

	parent *MenuItem
}

type Menu struct{ items []*MenuItem }

func Separator() *MenuItem { return &MenuItem{Type: MenuItemSeparator} }

func NewMenu(items []*MenuItem) *Menu {
	wireMenu(items, nil)
	return &Menu{items: items}
}

func wireMenu(items []*MenuItem, parent *MenuItem) {
	for _, item := range items {
		if item == nil {
			continue
		}
		item.parent = parent
		wireMenu(item.Submenu, item)
	}
}

func (m *Menu) Items() []*MenuItem {
	if m == nil {
		return nil
	}
	return append([]*MenuItem(nil), m.items...)
}

func (m *Menu) ItemByID(id string) *MenuItem {
	var find func([]*MenuItem) *MenuItem
	find = func(items []*MenuItem) *MenuItem {
		for _, item := range items {
			if item == nil {
				continue
			}
			if item.ID == id {
				return item
			}
			if found := find(item.Submenu); found != nil {
				return found
			}
		}
		return nil
	}
	if m == nil {
		return nil
	}
	return find(m.items)
}

func (it *MenuItem) IsEnabled() bool { return it != nil && !it.Disabled }
func (it *MenuItem) IsVisible() bool { return it != nil && !it.Hidden }
func (it *MenuItem) IsChecked() bool { return it != nil && it.Checked }

func (it *MenuItem) SetChecked(v bool) {
	if it == nil {
		return
	}
	if v && it.Type == MenuItemRadio {
		for _, sibling := range it.siblings() {
			if sibling != it && sibling.Type == MenuItemRadio {
				sibling.Checked = false
			}
		}
	}
	it.Checked = v
}

func (it *MenuItem) siblings() []*MenuItem {
	if it.parent != nil {
		return it.parent.Submenu
	}
	return nil
}

// ApplicationMenu is the macOS menu-bar description. The NSH host owns the
// real menus; this remains the model for update placement tests.
func (a *App) ApplicationMenu(openWindow func()) *Menu {
	checkUpdates := a.checkUpdatesMenuItem(openWindow)
	return NewMenu([]*MenuItem{
		{Role: RoleAppMenu, Submenu: []*MenuItem{
			{Role: RoleAbout},
			checkUpdates,
			Separator(),
			{Role: RoleServices},
			Separator(),
			{Role: RoleHide},
			{Role: RoleHideOthers},
			{Role: RoleUnhide},
			Separator(),
			{Role: RoleQuit},
		}},
		{Role: RoleFileMenu},
		{Role: RoleEditMenu},
		{Role: RoleViewMenu},
		{Role: RoleWindowMenu},
	})
}

func (a *App) checkUpdatesMenuItem(openWindow func()) *MenuItem {
	return &MenuItem{ID: "check-updates", Label: tr("检查软件更新…"), Click: func(*MenuItem, *Window) {
		a.initSoftwareUpdates()
		a.page, a.settingsTab = "settings", 3
		openWindow()
		a.softwareUpdates.Check(true)
	}}
}
