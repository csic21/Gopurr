package app

import (
	"context"
	"errors"
	"fmt"
)

// NativeTrayMenu keeps the existing desktop menu's behavior in the model,
// while the visible status item belongs to the foreground native host.
type NativeTrayMenu struct {
	build    func() *Menu
	serial   uint64
	commands map[string]*MenuItem
}

type NativeTrayItem struct {
	Command  string           `json:"command,omitempty"`
	Label    string           `json:"label"`
	Type     MenuItemType     `json:"type,omitempty"`
	Checked  bool             `json:"checked,omitempty"`
	Disabled bool             `json:"disabled,omitempty"`
	ToolTip  string           `json:"toolTip,omitempty"`
	Submenu  []NativeTrayItem `json:"submenu,omitempty"`
}

func NewNativeTrayMenu(build func() *Menu) *NativeTrayMenu {
	return &NativeTrayMenu{build: build}
}

func (m *NativeTrayMenu) Snapshot(ctx context.Context) (any, error) {
	return nativeMain(ctx, func() (any, error) {
		m.serial++
		m.commands = make(map[string]*MenuItem)
		var walk func([]*MenuItem, bool) []NativeTrayItem
		walk = func(items []*MenuItem, parentDisabled bool) []NativeTrayItem {
			out := make([]NativeTrayItem, 0, len(items))
			for _, item := range items {
				if item == nil || item.Hidden {
					continue
				}
				entry := NativeTrayItem{Label: item.Label, Type: item.Type, Checked: item.Checked, Disabled: item.Disabled || parentDisabled, ToolTip: item.ToolTip}
				if len(item.Submenu) > 0 {
					entry.Submenu = walk(item.Submenu, entry.Disabled)
				}
				if item.Click != nil && !entry.Disabled {
					entry.Command = fmt.Sprintf("%d:%d", m.serial, len(m.commands))
					m.commands[entry.Command] = item
				}
				out = append(out, entry)
			}
			return out
		}
		return walk(m.build().Items(), false), nil
	})
}

func (m *NativeTrayMenu) Activate(ctx context.Context, command string) (any, error) {
	return nativeMain(ctx, func() (any, error) {
		item := m.commands[command]
		if item == nil || !item.IsEnabled() || !item.IsVisible() || item.Click == nil {
			return nil, errors.New("tray command is unavailable; reopen the menu")
		}
		// A selection consumes this menu. Late or repeated commands cannot
		// operate a different item after a refresh.
		m.commands = nil
		if item.Type == MenuItemCheckbox {
			item.SetChecked(!item.IsChecked())
		}
		if item.Type == MenuItemRadio {
			item.SetChecked(true)
		}
		item.Click(item, nil)
		return map[string]bool{"accepted": true}, nil
	})
}
