//go:build windows

package mesh

import (
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func LaunchSSH(d Device, s Shortcut) error {
	args, err := s.SSHArguments(d)
	if err != nil {
		return err
	}
	dir, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	exe, err := windows.UTF16PtrFromString(filepath.Join(dir, "OpenSSH", "ssh.exe"))
	if err != nil {
		return err
	}
	for i := range args {
		args[i] = syscall.EscapeArg(args[i])
	}
	parameters, err := windows.UTF16PtrFromString(strings.Join(args, " "))
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, nil, exe, parameters, nil, windows.SW_SHOWNORMAL)
}
