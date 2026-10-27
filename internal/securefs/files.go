// Package securefs keeps credentials private and replaces files without
// following destination symlinks or hard links, including in privileged helpers.
package securefs

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

func PrivateDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return restrict(path, true)
}

// PrivateFile also tightens files written by older releases. Links are rejected
// rather than changing the permissions of a target outside the data directory.
func PrivateFile(path string) error { return restrict(path, false) }

func WriteFile(path string, data []byte, mode os.FileMode) error {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	name := ".gopurr-" + hex.EncodeToString(random[:])
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer root.Remove(name)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := root.Rename(name, filepath.Base(path)); err != nil {
		return fmt.Errorf("替换文件失败: %w", err)
	}
	return nil
}
