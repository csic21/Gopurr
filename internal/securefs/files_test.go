package securefs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAtomicWriteDoesNotFollowLinks(t *testing.T) {
	for _, link := range []struct {
		name   string
		create func(string, string) error
	}{{"symlink", os.Symlink}, {"hardlink", os.Link}} {
		t.Run(link.name, func(t *testing.T) {
			dir := t.TempDir()
			target, destination := filepath.Join(t.TempDir(), "target"), filepath.Join(dir, "state")
			if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := link.create(target, destination); err != nil {
				t.Skip(err)
			}
			if err := WriteFile(destination, []byte("replacement"), 0600); err != nil {
				t.Fatal(err)
			}
			if b, _ := os.ReadFile(target); string(b) != "original" {
				t.Fatal("modified link target")
			}
			if b, _ := os.ReadFile(destination); string(b) != "replacement" {
				t.Fatal("replacement missing")
			}
		})
	}
}

func TestPrivatePermissionsMigrateExistingFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "credentials")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "token")
	if err := os.WriteFile(file, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := PrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := PrivateFile(file); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		for path, mode := range map[string]os.FileMode{dir: 0700, file: 0600} {
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != mode {
				t.Fatalf("permissions %s: %v %v", path, info, err)
			}
		}
	}
}
