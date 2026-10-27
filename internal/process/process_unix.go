//go:build !windows

// Package process contains the small OS differences in the kernel lifecycle.
package process

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

func Configure(cmd *exec.Cmd)  { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func Alive(pid int) bool       { return syscall.Kill(pid, 0) == nil }
func UID() int                 { return os.Getuid() }
func Stop(p *os.Process) error { return p.Signal(syscall.SIGTERM) }
func NotifyStop(ch chan os.Signal) (func(), error) {
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	return func() { signal.Stop(ch) }, nil
}
