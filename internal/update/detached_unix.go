//go:build !windows

package update

import "os/exec"

func configureDetached(cmd *exec.Cmd) {}
