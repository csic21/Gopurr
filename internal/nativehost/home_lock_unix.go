//go:build !windows

package nativehost

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
	"gopurr/internal/securefs"
)

func lockHome(home string) (*homeLock, error) {
	if !filepath.IsAbs(home) {
		return nil, errors.New("absolute desktop home required")
	}
	if err := securefs.PrivateDir(home); err != nil {
		return nil, err
	}
	fd, err := unix.Open(filepath.Join(home, "desktop.lock"), unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "desktop.lock")
	fail := func(err error) (*homeLock, error) { file.Close(); return nil, err }
	info, err := file.Stat()
	if err != nil {
		return fail(err)
	}
	if !info.Mode().IsRegular() {
		return fail(errors.New("desktop lock is not a regular file"))
	}
	if err := file.Chmod(0600); err != nil {
		return fail(err)
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return fail(ErrHomeInUse)
		}
		return fail(err)
	}
	return &homeLock{file: file}, nil
}
