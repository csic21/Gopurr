//go:build windows

package nativehost

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
	"gopurr/internal/securefs"
)

func lockHome(home string) (*homeLock, error) {
	if !filepath.IsAbs(home) {
		return nil, errors.New("absolute desktop home required")
	}
	if err := securefs.PrivateDir(home); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(home, "desktop.lock"), os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*homeLock, error) { file.Close(); return nil, err }
	if err := securefs.PrivateFile(file.Name()); err != nil {
		return fail(err)
	}
	var overlapped windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped); err != nil {
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return fail(ErrHomeInUse)
		}
		return fail(err)
	}
	return &homeLock{file: file}, nil
}
