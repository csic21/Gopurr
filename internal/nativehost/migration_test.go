package nativehost

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopMigrationPreservesCredentialsAndOriginalSnapshot(t *testing.T) {
	home := t.TempDir()
	files := map[string]string{"prefs.json": `{"appearance":"dark","autoLaunch":true}`, "profiles.json": `{"active":"old","profiles":[]}`, "profiles/old.yaml": "# credential\npassword: secret\n", "overrides.json": `{"library":[{"name":"rules"}]}`, "tailnet/state": "mesh identity"}
	for name, body := range files {
		path := filepath.Join(home, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := PrepareDesktopMigration(home); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		for _, path := range []string{filepath.Join(home, name), filepath.Join(home, DesktopMigration, name)} {
			b, err := os.ReadFile(path)
			if err != nil || string(b) != body {
				t.Fatalf("migration changed %s: %v", name, err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(home, "prefs.json"), []byte(`{"appearance":"light"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := PrepareDesktopMigration(home); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(home, DesktopMigration, "prefs.json"))
	if string(b) != files["prefs.json"] {
		t.Fatal("restart replaced the original backup")
	}
}

func TestDesktopMigrationFailureLeavesNoCommittedSnapshot(t *testing.T) {
	for _, linked := range []bool{false, true} {
		t.Run(map[bool]string{false: "corrupt", true: "symlink"}[linked], func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, "prefs.json")
			if linked {
				if err := os.Symlink(filepath.Join(t.TempDir(), "outside"), path); err != nil {
					t.Fatal(err)
				}
			} else {
				os.WriteFile(path, []byte("{broken"), 0600)
			}
			if err := PrepareDesktopMigration(home); err == nil {
				t.Fatal("unsafe data accepted")
			}
			if _, err := os.Stat(filepath.Join(home, DesktopMigration)); !os.IsNotExist(err) {
				t.Fatal("failed migration was committed")
			}
			if !linked {
				os.WriteFile(path, []byte(`{}`), 0600)
				if err := PrepareDesktopMigration(home); err != nil {
					t.Fatal("retry failed", err)
				}
			}
		})
	}
}
