package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCredentialPermissionsOnImportAndMigration(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(t.TempDir(), "input.yaml")
	os.WriteFile(source, []byte(sampleYAML), 0644)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.AddFile(source, "credentials")
	if err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		if runtime.GOOS == "windows" {
			return
		} // DACL is tested in securefs.
		for path, mode := range map[string]os.FileMode{dir: 0700, filepath.Join(dir, "profiles"): 0700, filepath.Join(dir, "profiles.json"): 0600, filepath.Join(dir, "profiles", p.File): 0600} {
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != mode {
				t.Fatalf("credential permissions %s: %v %v", path, info, err)
			}
		}
	}
	check()
	// Simulate files saved by an older release.
	for _, path := range []string{dir, filepath.Join(dir, "profiles")} {
		os.Chmod(path, 0755)
	}
	for _, path := range []string{filepath.Join(dir, "profiles.json"), filepath.Join(dir, "profiles", p.File)} {
		os.Chmod(path, 0644)
	}
	if _, err := Open(dir); err != nil {
		t.Fatal(err)
	}
	check()
}

func TestStoreRejectsEscapingCachePath(t *testing.T) {
	dir := t.TempDir()
	data, _ := json.Marshal(&Store{Items: []Profile{{ID: "malicious", File: "../../outside.yaml"}}})
	os.WriteFile(filepath.Join(dir, "profiles.json"), data, 0600)
	if _, err := Open(dir); err == nil {
		t.Fatal("escaping cache path accepted")
	}
}
