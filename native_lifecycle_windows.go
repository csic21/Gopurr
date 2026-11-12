//go:build windows

package main

import (
	"log"
	"os"
	"strconv"

	"golang.org/x/sys/windows"
	"gopurr/internal/update"
)

func nativeParentAlive(pid int) bool {
	if pid <= 1 || os.Getppid() != pid {
		return false
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	state, err := windows.WaitForSingleObject(h, 0)
	return err == nil && state == uint32(windows.WAIT_TIMEOUT)
}

// Windows replaces files in the installation directory while they are mapped.
// Start the new frontend and let it wait until this helper exits.
func nativeRelaunch(frontend, home string) {
	if err := update.ActivateRelaunch(frontend, "--gopurr-home", home, "--wait-for-process", strconv.Itoa(os.Getpid())); err != nil {
		log.Printf("native relaunch: %v", err)
	}
}
