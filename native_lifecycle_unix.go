//go:build !windows

package main

import (
	"errors"
	"log"
	"os"
	"syscall"

	"gopurr/internal/update"
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

func nativeRelaunch(frontend, home string) {
	if err := update.ActivateRelaunch(frontend, "--gopurr-home", home); err != nil {
		log.Printf("native relaunch: %v", err)
	}
}
