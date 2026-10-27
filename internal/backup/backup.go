// Package backup reads portable, versioned archives of user configuration.
// Runtime sockets, secrets, system proxy leases and downloaded executables are
// deliberately never part of an archive.
package backup

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxSize = 64 << 20

var roots = []string{"prefs.json", "overrides.json", "local.yaml", "profiles.json", "profiles"}

type Snapshot struct {
	Version int               `json:"version"`
	Created time.Time         `json:"created"`
	Files   map[string][]byte `json:"-"`
}

func allowed(name string) bool {
	if strings.Contains(name, "\\") || path.Clean(name) != name || strings.HasPrefix(name, "/") {
		return false
	}
	for _, s := range roots[:4] {
		if name == s {
			return true
		}
	}
	return strings.HasPrefix(name, "profiles/") && path.Dir(name) == "profiles" && strings.HasSuffix(name, ".yaml")
}

func (s *Snapshot) Validate() error {
	if s.Version != 1 {
		return fmt.Errorf("不支持的备份版本 %d", s.Version)
	}
	if len(s.Files) > 2048 {
		return fmt.Errorf("备份文件过多")
	}
	total := 0
	for name, data := range s.Files {
		if !allowed(name) {
			return fmt.Errorf("备份包含不允许的路径: %s", name)
		}
		if len(data) > 16<<20 {
			return fmt.Errorf("备份中的文件过大: %s", name)
		}
		total += len(data)
	}
	if total > maxSize {
		return fmt.Errorf("备份内容超过 64 MB")
	}
	for _, name := range roots[:4] {
		if len(s.Files[name]) == 0 {
			return fmt.Errorf("备份缺少 %s", name)
		}
	}
	return nil
}

func Write(destination string, s *Snapshot) error {
	if err := s.Validate(); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(destination), ".gopurr-backup-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	z := zip.NewWriter(f)
	manifest, _ := json.Marshal(struct {
		Version int       `json:"version"`
		Created time.Time `json:"created"`
	}{s.Version, s.Created})
	write := func(name string, data []byte) error {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0600)
		w, err := z.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	err = write("manifest.json", manifest)
	names := make([]string, 0, len(s.Files))
	for n := range s.Files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err != nil {
			break
		}
		err = write(n, s.Files[n])
	}
	err = errors.Join(err, z.Close(), f.Sync(), f.Close())
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), destination)
}

func Read(filename string) (*Snapshot, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxSize {
		return nil, fmt.Errorf("备份文件超过 64 MB")
	}
	z, err := zip.NewReader(f, info.Size())
	if err != nil {
		return nil, fmt.Errorf("不是有效的 Gopurr 备份: %w", err)
	}
	if len(z.File) > 2049 {
		return nil, fmt.Errorf("备份文件过多")
	}
	s := &Snapshot{Files: map[string][]byte{}}
	seen := map[string]bool{}
	remaining := int64(maxSize)
	for _, entry := range z.File {
		name := entry.Name
		if seen[name] || (name != "manifest.json" && !allowed(name)) || entry.Mode()&os.ModeType != 0 {
			return nil, fmt.Errorf("备份包含重复或不允许的文件: %s", name)
		}
		seen[name] = true
		if entry.UncompressedSize64 > 16<<20 {
			return nil, fmt.Errorf("备份中的文件过大: %s", name)
		}
		r, err := entry.Open()
		if err != nil {
			return nil, err
		}
		b, readErr := io.ReadAll(io.LimitReader(r, min(16<<20, remaining)+1))
		closeErr := r.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return nil, err
		}
		remaining -= int64(len(b))
		if remaining < 0 || len(b) > 16<<20 {
			return nil, fmt.Errorf("备份内容过大")
		}
		if name == "manifest.json" {
			if err := json.Unmarshal(b, s); err != nil {
				return nil, err
			}
		} else {
			s.Files[name] = b
		}
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return s, nil
}

// Install swaps only the managed configuration roots. The caller can restore
// all previous files if the kernel rejects the imported configuration.
type Transaction struct {
	dir, stage string
	old, new   []string
}

func Install(dir string, s *Snapshot) (*Transaction, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(dir, ".restore-*")
	if err != nil {
		return nil, err
	}
	tx := &Transaction{dir: dir, stage: stage}
	if err := os.MkdirAll(filepath.Join(stage, "next", "profiles"), 0700); err != nil {
		os.RemoveAll(stage)
		return nil, err
	}
	for name, data := range s.Files {
		if err := os.WriteFile(filepath.Join(stage, "next", filepath.FromSlash(name)), data, 0600); err != nil {
			os.RemoveAll(stage)
			return nil, err
		}
	}
	if err := os.Mkdir(filepath.Join(stage, "previous"), 0700); err != nil {
		os.RemoveAll(stage)
		return nil, err
	}
	for _, name := range roots {
		current := filepath.Join(dir, name)
		if _, err := os.Lstat(current); err == nil {
			if err := os.Rename(current, filepath.Join(stage, "previous", name)); err != nil {
				return nil, errors.Join(err, tx.Rollback())
			}
			tx.old = append(tx.old, name)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, errors.Join(err, tx.Rollback())
		}
		if err := os.Rename(filepath.Join(stage, "next", name), current); err != nil {
			return nil, errors.Join(err, tx.Rollback())
		}
		tx.new = append(tx.new, name)
	}
	return tx, nil
}
func (t *Transaction) Rollback() error {
	var errs []error
	for i := len(t.new) - 1; i >= 0; i-- {
		errs = append(errs, os.RemoveAll(filepath.Join(t.dir, t.new[i])))
	}
	for i := len(t.old) - 1; i >= 0; i-- {
		name := t.old[i]
		errs = append(errs, os.Rename(filepath.Join(t.stage, "previous", name), filepath.Join(t.dir, name)))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("恢复旧文件失败，原文件保留在 %s: %w", t.stage, err)
	}
	return os.RemoveAll(t.stage)
}
func (t *Transaction) Commit() error { return os.RemoveAll(t.stage) }
