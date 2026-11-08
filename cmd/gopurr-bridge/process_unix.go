//go:build cgo && !windows && !android

package main

import "os/exec"

func configureDesktopCommand(cmd *exec.Cmd) {}
