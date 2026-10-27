package app

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLegacyDataDirMigrates covers the rename: an existing MyClash data
// directory is copied to Gopurr once, so profiles and settings survive.
func TestLegacyDataDirMigrates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("GOPURR_HOME", "")

	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(base, "myclash")
	if err := os.MkdirAll(filepath.Join(old, "profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "prefs.json"), []byte(`{"lightMode":true,"lightDelaySeconds":30}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "profiles", "a.yaml"), []byte("mixed-port: 7890\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := New()

	want := filepath.Join(base, "Gopurr")
	if a.dir != want {
		t.Fatalf("dir = %q, want %q", a.dir, want)
	}
	if _, err := os.Stat(filepath.Join(want, "profiles", "a.yaml")); err != nil {
		t.Fatalf("profile not migrated: %v", err)
	}
	if p := a.Prefs(); !p.LightMode || p.LightDelay != 30 {
		t.Fatalf("prefs not migrated: %+v", p)
	}

	// The old directory is left alone, and a second start copies nothing new.
	if _, err := os.Stat(filepath.Join(old, "prefs.json")); err != nil {
		t.Fatalf("legacy directory was removed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(old, "prefs.json"), []byte(`{"lightMode":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	again := New()
	if !again.Prefs().LightMode {
		t.Fatal("migration ran twice and overwrote the new settings")
	}
}

// TestNoLegacyDirLeavesNothingBehind makes sure a fresh install just works.
func TestNoLegacyDirLeavesNothingBehind(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("GOPURR_HOME", "")

	a := New()
	if _, err := os.Stat(a.dir); err != nil {
		t.Fatalf("data dir missing: %v", err)
	}
}
