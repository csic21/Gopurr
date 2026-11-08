// Package autostart manages the current user's login startup entry.
package autostart

import (
	"fmt"
	"os"

	"gopurr/internal/localize"
)

func Set(enabled bool) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf(localize.Text("无法定位应用: %w"), err)
	}
	return set(enabled, executable)
}

// SetExecutable uses the visible frontend when the business process is bundled
// as a helper executable. It never changes the launch entry until requested.
func SetExecutable(enabled bool, executable string) error {
	return set(enabled, executable)
}
