//go:build windows

package main

import (
	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
	"log"
	"os"
	"os/exec"
	"strconv"
	"syscall"
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

// Windows' updater swaps the installation directory while files are mapped.
// Restart the new frontend, never the backend executable. The frontend waits
// for this helper to exit before loading its new DLL or taking any data lock.
func nativeRelaunch(frontend, home string) {
	mygo.App.OnQuit(func() {
		cmd := exec.Command(frontend, "--gopurr-home", home, "--wait-for-process", strconv.Itoa(os.Getpid()))
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
		if err := cmd.Start(); err != nil {
			log.Printf("native relaunch: %v", err)
		} else {
			_ = cmd.Process.Release()
		}
	})
	mygo.App.Quit()
}
