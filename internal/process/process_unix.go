//go:build !windows

// Package process contains the small OS differences in the kernel lifecycle.
package process

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

func Configure(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	// An ordinary UI cannot signal its root daemon. EPERM proves that the
	// process exists; treating it as death makes a working TUN service vanish.
	return err == nil || errors.Is(err, syscall.EPERM)
}
func UID() int                 { return os.Getuid() }
func Stop(p *os.Process) error { return p.Signal(syscall.SIGTERM) }
func NotifyStop(ch chan os.Signal) (func(), error) {
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	return func() { signal.Stop(ch) }, nil
}
