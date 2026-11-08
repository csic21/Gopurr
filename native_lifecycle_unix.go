//go:build !windows

package main

import (
	"errors"
	"github.com/egoist/mygo"
	"os"
	"syscall"
)

func nativeParentAlive(pid int) bool {
	if pid <= 1 || os.Getppid() != pid {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

func nativeRelaunch(frontend, home string) { mygo.App.Relaunch() }
