//go:build !windows

package core

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"

	"golang.org/x/sys/unix"
)

// A privileged daemon must not create its backend beneath a user-controlled
// TMPDIR ancestor. /tmp is root-owned and sticky, and keeps socket paths short.
func privateControllerDir() (string, error) { return os.MkdirTemp("/tmp", "gp-core-") }

// Set ownership and permissions before publishing the socket in the user's
// directory. A privileged chmod/chown on a public pathname would itself create
// a symlink race. Pin the destination directory while publishing and removing.
func listenController(path, _ string) (net.Listener, error) {
	fd, err := unix.Open(filepath.Dir(path), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	parent := os.NewFile(uintptr(fd), filepath.Dir(path))
	keep := false
	defer func() {
		if !keep {
			parent.Close()
		}
	}()
	info, err := parent.Stat()
	if err != nil {
		return nil, err
	}
	// Publish a link to a separately protected frontend socket. Unlike moving
	// the socket itself, this also works when the data directory is on another
	// volume. Only the authenticated frontend is traversable by the UI.
	gatewayDir, err := os.MkdirTemp("/tmp", "gp-gateway-")
	if err != nil {
		return nil, err
	}
	staged := filepath.Join(gatewayDir, "c.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: staged, Net: "unix"})
	if err != nil {
		os.RemoveAll(gatewayDir)
		return nil, err
	}
	listener.SetUnlinkOnClose(false)
	success := false
	defer func() {
		if !success {
			listener.Close()
			os.RemoveAll(gatewayDir)
		}
	}()
	if err := os.Chmod(staged, 0600); err != nil {
		return nil, err
	}
	if os.Geteuid() == 0 {
		owner := info.Sys().(*syscall.Stat_t)
		if err := os.Chown(staged, int(owner.Uid), int(owner.Gid)); err != nil {
			return nil, err
		}
	}
	if err := os.Chmod(gatewayDir, 0711); err != nil {
		return nil, err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	link := ".controller-" + hex.EncodeToString(random[:])
	if err := unix.Symlinkat(staged, fd, link); err != nil {
		return nil, err
	}
	defer unix.Unlinkat(fd, link, 0)
	if err := unix.Renameat(fd, link, fd, filepath.Base(path)); err != nil {
		return nil, err
	}
	keep, success = true, true
	return &controllerListener{UnixListener: listener, parent: parent, name: filepath.Base(path), link: path, socket: staged, dir: gatewayDir}, nil
}

type controllerListener struct {
	*net.UnixListener
	parent *os.File
	name   string // published name inside the data directory
	link   string // published path (a symlink to socket)
	socket string // real socket inside the private gateway directory
	dir    string
	once   sync.Once
	gone   atomic.Bool
}

// Intact reports whether the published link still resolves to this listener.
// The gateway directory lives in a system temporary directory, and macOS (or a
// cleanup tool) can delete it while the daemon keeps running. Every client then
// fails forever because the link points at nothing; the daemon uses this check
// to exit so launchd (KeepAlive) or the app's watchdog starts a clean copy.
func (l *controllerListener) Intact() bool {
	if l == nil || l.gone.Load() {
		return true
	}
	info, err := os.Lstat(l.link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	target, err := os.Readlink(l.link)
	if err != nil || target != l.socket {
		return false
	}
	_, err = os.Stat(l.socket)
	return err == nil
}

func (l *controllerListener) Close() error {
	var err error
	l.once.Do(func() {
		l.gone.Store(true)
		err = l.UnixListener.Close()
		// Only remove the published link when it still points at this
		// listener. A successor may have replaced it (service restart), and
		// unlinking by name would then disconnect the new daemon.
		var target [1024]byte
		if n, readErr := unix.Readlinkat(int(l.parent.Fd()), l.name, target[:]); readErr == nil && string(target[:n]) == l.socket {
			_ = unix.Unlinkat(int(l.parent.Fd()), l.name, 0)
		}
		_ = l.parent.Close()
		_ = os.RemoveAll(l.dir)
	})
	return err
}
