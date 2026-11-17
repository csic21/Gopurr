package app

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gopurr/internal/patch"
)

func TestLoadOverridesMigratesFactoryTunStack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "overrides.json")
	raw := []byte(`{"mixedPort":7890,"mode":"rule","tunEnable":false,"tunStack":"mixed"}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	a := &App{ov: patch.Default(), ovPath: path}
	a.loadOverrides()
	if runtime.GOOS == "darwin" {
		if a.ov.TunStack != "gvisor" || !a.ov.TunStackMigrated {
			t.Fatalf("darwin load = %+v", a.ov)
		}
		saved, err := os.ReadFile(path)
		if err != nil || !bytes.Contains(saved, []byte(`"tunStack": "gvisor"`)) || !bytes.Contains(saved, []byte(`"tunStackMigrated": true`)) {
			t.Fatalf("migrated file = %s (%v)", saved, err)
		}
		a.ov.TunStack = "mixed"
		if err := writeSettingsFile(path, a.ov); err != nil {
			t.Fatal(err)
		}
		a.loadOverrides()
		if a.ov.TunStack != "mixed" || !a.ov.TunStackMigrated {
			t.Fatalf("manual mixed lost: %+v", a.ov)
		}
		a.ov.TunStack = "system"
		if err := writeSettingsFile(path, a.ov); err != nil {
			t.Fatal(err)
		}
		a.loadOverrides()
		if a.ov.TunStack != "system" {
			t.Fatalf("manual system lost: %+v", a.ov)
		}
		return
	}
	if a.ov.TunStack != "mixed" || a.ov.TunStackMigrated {
		t.Fatalf("non-darwin load = %+v", a.ov)
	}
	saved, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(saved, raw) {
		t.Fatalf("windows file changed: %s (%v)", saved, err)
	}
}
