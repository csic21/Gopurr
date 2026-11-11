package nativehost

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"gopurr/internal/securefs"
)

const (
	relaunchShowFile = ".relaunch-show"
	relaunchShowTTL  = 10 * time.Minute
)

// MarkRelaunchShow records that the next desktop host launch belongs to an
// explicit update restart. The marker is deliberately one-shot: a normal
// launch, including a silent start, must not inherit this visibility.
func MarkRelaunchShow(home string) error {
	if !filepath.IsAbs(home) {
		return errors.New("absolute desktop home required")
	}
	path := filepath.Join(home, relaunchShowFile)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return securefs.WriteFile(path, []byte("1\n"), 0600)
}

// ConsumeRelaunchShow reports whether this launch should show the window even
// when silent start is enabled. Stale markers are ignored and removed.
func ConsumeRelaunchShow(home string) bool {
	path := filepath.Join(home, relaunchShowFile)
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	_ = os.Remove(path)
	if !info.Mode().IsRegular() {
		return false
	}
	return time.Since(info.ModTime()) <= relaunchShowTTL
}
