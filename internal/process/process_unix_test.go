//go:build !windows

package process

import (
	"os"
	"testing"
)

func TestAliveIncludesPrivilegedProcesses(t *testing.T) {
	// PID 1 is launchd on macOS. Its signal-0 probe returns EPERM to a normal
	// user, yet the privileged process is alive.
	if !Alive(1) || !Alive(os.Getpid()) {
		t.Fatal("a live process was treated as dead")
	}
	if Alive(-1) || Alive(0) {
		t.Fatal("process groups are not valid process identities")
	}
}
