package backup

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixture() *Snapshot {
	return &Snapshot{Version: 1, Created: time.Now().Truncate(time.Second), Files: map[string][]byte{"prefs.json": []byte(`{}`), "overrides.json": []byte(`{}`), "profiles.json": []byte(`{"profiles":[]}`), "local.yaml": []byte("rules: [MATCH,DIRECT]\n"), "profiles/demo.yaml": []byte("rules: [MATCH,REJECT]\n")}}
}
func TestArchiveRoundTripAndTransactionalRollback(t *testing.T) {
	s := fixture()
	dir := t.TempDir()
	filename := filepath.Join(dir, "backup.zip")
	if err := Write(filename, s); err != nil {
		t.Fatal(err)
	}
	read, err := Read(filename)
	if err != nil {
		t.Fatal(err)
	}
	if !read.Created.Equal(s.Created) || string(read.Files["local.yaml"]) != string(s.Files["local.yaml"]) {
		t.Fatal("archive did not round trip")
	}
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "prefs.json"), []byte("old preferences"), 0600)
	os.WriteFile(filepath.Join(home, "core.secret"), []byte("keep secret"), 0600)
	tx, err := Install(home, read)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(home, "prefs.json"))
	if string(b) != "old preferences" {
		t.Fatal("original not restored")
	}
	if _, err := os.Stat(filepath.Join(home, "profiles")); !os.IsNotExist(err) {
		t.Fatal("imported directory survived rollback")
	}
	tx, err = Install(home, read)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(home, "core.secret"))
	if string(b) != "keep secret" {
		t.Fatal("runtime identity changed")
	}
}
func TestArchiveRejectsUnsafeAndDuplicateEntries(t *testing.T) {
	for _, name := range []string{"../escape", "/tmp/escape", `profiles\escape.yaml`, "profiles/../../escape.yaml", "core.secret", "profiles/demo.yaml"} {
		t.Run(name, func(t *testing.T) {
			f, err := os.Create(filepath.Join(t.TempDir(), "bad.zip"))
			if err != nil {
				t.Fatal(err)
			}
			z := zip.NewWriter(f)
			for i := 0; i < 2; i++ {
				w, err := z.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				w.Write([]byte("bad"))
			}
			z.Close()
			f.Close()
			if _, err := Read(f.Name()); err == nil {
				t.Fatal("accepted unsafe archive")
			}
		})
	}
	s := fixture()
	s.Version = 99
	if err := Write(filepath.Join(t.TempDir(), "future.zip"), s); err == nil {
		t.Fatal("accepted unsupported version")
	}
}
