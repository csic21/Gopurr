// Package autostart manages the current user's login startup entry.
package autostart

import (
	"fmt"
	"os"
)

func Set(enabled bool) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法定位应用: %w", err)
	}
	return set(enabled, executable)
}
