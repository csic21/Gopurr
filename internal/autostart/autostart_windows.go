package autostart

import (
	"errors"
	"golang.org/x/sys/windows/registry"
)

func set(enabled bool, executable string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if !enabled {
		err := key.DeleteValue("Gopurr")
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	return key.SetStringValue("Gopurr", `"`+executable+`" --autostart`)
}
