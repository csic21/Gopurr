package nativehost

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
)

var ErrHomeInUse = errors.New("Gopurr is already running for this data directory")

type homeLock struct {
	file *os.File
	once sync.Once
	err  error
}

func (l *homeLock) Close() error { l.once.Do(func() { l.err = l.file.Close() }); return l.err }

// LockHome excludes every desktop model using this home, including direct
// executable launches that bypass LaunchServices single-instance handling.
// The lock file remains in place: removing it would let another process lock
// a different inode while a previous holder is still alive. Closing the OS
// handle, including after a crash, releases the advisory lock automatically.
func LockHome(home string) (io.Closer, error) {
	cleaned := filepath.Clean(home)
	if !filepath.IsAbs(cleaned) {
		return nil, errors.New("absolute desktop home required")
	}
	if filepath.Dir(cleaned) == cleaned {
		return nil, errors.New("desktop home must be a dedicated application directory")
	}
	// Tightening a dedicated app folder is safe; tightening /tmp, the system
	// temporary root or the user's home would change unrelated applications.
	forbidden := []string{os.TempDir(), "/tmp", "/private/tmp"}
	if userHome, err := os.UserHomeDir(); err == nil {
		forbidden = append(forbidden, userHome)
	}
	info, _ := os.Stat(cleaned)
	for _, path := range forbidden {
		shared, err := os.Stat(path)
		if cleaned == filepath.Clean(path) || (info != nil && err == nil && os.SameFile(info, shared)) {
			return nil, errors.New("desktop home must be a dedicated application directory")
		}
	}
	return lockHome(cleaned)
}
