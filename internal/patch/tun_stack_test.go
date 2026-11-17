package patch

import (
	"encoding/json"
	"testing"
)

func TestDefaultTunStackByPlatform(t *testing.T) {
	if DefaultTunStack("darwin") != "gvisor" {
		t.Fatalf("darwin default = %s", DefaultTunStack("darwin"))
	}
	if DefaultTunStack("windows") != "mixed" {
		t.Fatalf("windows default = %s", DefaultTunStack("windows"))
	}
	darwin := DefaultFor("darwin")
	if darwin.TunStack != "gvisor" || !darwin.TunStackMigrated {
		t.Fatalf("darwin factory = %+v", darwin)
	}
	windows := DefaultFor("windows")
	if windows.TunStack != "mixed" || windows.TunStackMigrated {
		t.Fatalf("windows factory = %+v", windows)
	}
}

func TestDarwinMigratesFactoryMixedOnce(t *testing.T) {
	raw := []byte(`{"mixedPort":7890,"tunEnable":true,"tunStack":"mixed"}`)
	migrated, changed, err := NormalizeStoredOverrides(raw, "darwin")
	if err != nil || !changed || migrated.TunStack != "gvisor" || !migrated.TunStackMigrated || !migrated.TunEnable || migrated.MixedPort != 7890 {
		t.Fatalf("migrate mixed: %+v changed=%v err=%v", migrated, changed, err)
	}

	migrated.TunStack = "mixed"
	body, err := json.Marshal(migrated)
	if err != nil {
		t.Fatal(err)
	}
	kept, changed, err := NormalizeStoredOverrides(body, "darwin")
	if err != nil || changed || kept.TunStack != "mixed" || !kept.TunStackMigrated {
		t.Fatalf("manual mixed = %+v changed=%v err=%v", kept, changed, err)
	}

	migrated.TunStack = "system"
	body, err = json.Marshal(migrated)
	if err != nil {
		t.Fatal(err)
	}
	kept, changed, err = NormalizeStoredOverrides(body, "darwin")
	if err != nil || changed || kept.TunStack != "system" {
		t.Fatalf("manual system = %+v changed=%v err=%v", kept, changed, err)
	}
}

func TestDarwinMigrationKeepsNonFactoryStacks(t *testing.T) {
	for _, stack := range []string{"system", "gvisor"} {
		got, changed, err := NormalizeStoredOverrides([]byte(`{"tunStack":"`+stack+`"}`), "darwin")
		if err != nil || !changed || got.TunStack != stack || !got.TunStackMigrated {
			t.Fatalf("stack %s = %+v changed=%v err=%v", stack, got, changed, err)
		}
		body, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		again, changed, err := NormalizeStoredOverrides(body, "darwin")
		if err != nil || changed || again.TunStack != stack {
			t.Fatalf("second load %s = %+v changed=%v err=%v", stack, again, changed, err)
		}
	}
	empty, changed, err := NormalizeStoredOverrides([]byte(`{"tunStack":""}`), "darwin")
	if err != nil || !changed || empty.TunStack != "gvisor" || !empty.TunStackMigrated {
		t.Fatalf("empty stack = %+v changed=%v err=%v", empty, changed, err)
	}
}

func TestWindowsDoesNotMigrateFactoryMixed(t *testing.T) {
	raw := []byte(`{"mixedPort":7890,"tunStack":"mixed","tunEnable":true}`)
	got, changed, err := NormalizeStoredOverrides(raw, "windows")
	if err != nil || changed || got.TunStack != "mixed" || got.TunStackMigrated {
		t.Fatalf("windows = %+v changed=%v err=%v", got, changed, err)
	}
	fresh := DefaultFor("windows")
	if fresh.TunStack != "mixed" {
		t.Fatalf("windows default = %+v", fresh)
	}
}

func TestNormalizeStoredOverridesRejectsBrokenJSON(t *testing.T) {
	if _, _, err := NormalizeStoredOverrides([]byte(`{"tunStack":`), "darwin"); err == nil {
		t.Fatal("expected JSON error")
	}
}
