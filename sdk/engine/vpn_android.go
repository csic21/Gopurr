//go:build android

package engine

import (
	"context"
	"errors"
	"sync/atomic"
	"syscall"

	"gopurr/internal/core"

	"github.com/metacubex/mihomo/component/dialer"
	"golang.org/x/sys/unix"
)

type vpnProtector struct{ protect func(int) bool }

var androidProtector atomic.Pointer[vpnProtector]

func init() {
	// Install once before any kernel sockets exist. The upstream hook itself
	// stays stable while each VPN session swaps its callback atomically.
	dialer.DefaultSocketHook = func(_, _ string, connection syscall.RawConn) error {
		protector := androidProtector.Load()
		if protector == nil {
			return nil
		}
		var protected bool
		if err := connection.Control(func(fd uintptr) { protected = protector.protect(int(fd)) }); err != nil {
			return err
		}
		if !protected {
			return errors.New("Android refused to protect the outbound VPN socket")
		}
		return nil
	}
}

func mobileVPNSupported() bool { return true }

func setVPNProtector(protect func(int) bool) {
	if protect == nil {
		androidProtector.Store(nil)
	} else {
		androidProtector.Store(&vpnProtector{protect: protect})
	}
}

func duplicateVPNDescriptor(fd int) (int, error) {
	duplicate, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 3)
	return duplicate, err
}

func closeVPNDescriptor(fd int) { _ = unix.Close(fd) }

func probeMixedVPN(ctx context.Context, port int) error { return core.ProbeMixed(ctx, port) }
