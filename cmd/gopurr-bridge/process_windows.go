//go:build cgo && windows

package main

import (
	"os/exec"
	"syscall"
)

func configureDesktopCommand(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
