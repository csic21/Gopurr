//go:build !windows

package nativehost

import (
	"errors"
	"os"
)

func privateAccess(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0077 != 0 {
		return errors.New("public native session path")
	}
	return nil
}
