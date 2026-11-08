//go:build cgo && !windows && !android

package main

import (
	"os"
	"os/exec"
)

func configureDesktopCommand(cmd *exec.Cmd) {
	// The backend deliberately outlives the frontend during cleanup/relaunch.
	// An os/exec diagnostic pipe belongs to the frontend; after it exits, a
	// backend write to fd 2 would raise SIGPIPE before requesting relaunch.
	// Unix GUI launches already provide a stable inherited stderr descriptor.
	cmd.Stderr = os.Stderr
}
