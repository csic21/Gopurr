//go:build !windows

package core

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"sync"
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
	return &controllerListener{UnixListener: listener, parent: parent, name: filepath.Base(path), dir: gatewayDir}, nil
}

type controllerListener struct {
	*net.UnixListener
	parent *os.File
	name   string
	dir    string
	once   sync.Once
}

func (l *controllerListener) Close() error {
	var err error
	l.once.Do(func() {
		err = l.UnixListener.Close()
		_ = unix.Unlinkat(int(l.parent.Fd()), l.name, 0)
		_ = l.parent.Close()
		_ = os.RemoveAll(l.dir)
	})
	return err
}
