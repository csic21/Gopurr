package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
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
	var got patch.Overrides
	if err != nil || json.Unmarshal(saved, &got) != nil || !reflect.DeepEqual(got, a.ov) {
		t.Fatalf("windows file changed: %s (%v)", saved, err)
	}
}

func TestSettingsOfferOfficialTunStacks(t *testing.T) {
	want := []string{"mixed", "system", "gvisor", "mips"}
	if !reflect.DeepEqual(settingsTunStacks, want) {
		t.Fatalf("stacks = %v", settingsTunStacks)
	}
	if tunStackHint() != "建议 mips；官方默认栈" {
		t.Fatalf("hint = %q", tunStackHint())
	}
}

func TestNetworkSettingsSavesMipsAndRejectsUnknown(t *testing.T) {
	before := patch.Default()
	a := &App{prefs: defaultPrefs(), cfgText: demoConfig, ov: before, ovPath: filepath.Join(t.TempDir(), "overrides.json")}
	a.initSettings()
	a.tunStackPick = "mips"
	if !a.saveNetworkSettings() {
		t.Fatal(a.networkError)
	}
	saved := readSavedOverrides(t, a.ovPath)
	if saved.TunStack != "mips" || saved.TunStackMigrated != before.TunStackMigrated || saved.MixedPort != before.MixedPort {
		t.Fatalf("saved mips = %+v", saved)
	}
	a.tunStackPick = "gVisor"
	if a.saveNetworkSettings() || !strings.Contains(a.networkError, "TUN 协议栈须为 system、gvisor、mixed 或 mips") {
		t.Fatalf("unknown stack accepted: %q", a.networkError)
	}
	if got := readSavedOverrides(t, a.ovPath); got.TunStack != "mips" {
		t.Fatalf("rejected stack replaced mips: %+v", got)
	}
}

func TestOfficialTunStackName(t *testing.T) {
	for _, name := range []string{"gVisor", "System", "Mixed", "Mips"} {
		if got := officialTunStackName(name); got != strings.ToLower(name) {
			t.Fatalf("%q displayed as %q", name, got)
		}
	}
	for _, name := range []string{"custom", "Unknown", ""} {
		if got := officialTunStackName(name); got != name {
			t.Fatalf("%q displayed as %q", name, got)
		}
	}
}

func readSavedOverrides(t *testing.T, path string) patch.Overrides {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved patch.Overrides
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	return saved
}
