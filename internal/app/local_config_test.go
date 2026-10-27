package app

import (
	"os"
	"testing"
)

func TestLocalConfigSaveSurvivesRestart(t *testing.T) {
	t.Setenv("GOPURR_HOME", t.TempDir())
	a := New()
	a.k = nil
	a.rawDraft = "rules:\n  - DOMAIN-SUFFIX,example.com,DIRECT\n  - MATCH,DIRECT\n"
	a.showRaw = true
	a.saveLocalConfig()
	if a.rawError != "" || a.showRaw {
		t.Fatalf("save failed: %s", a.rawError)
	}
	if b := New(); b.cfgText != a.rawDraft {
		t.Fatalf("saved config lost on restart: %q", b.cfgText)
	}
	previous, err := os.ReadFile(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	a.rawDraft = "[not, a, config]"
	a.showRaw = true
	a.saveLocalConfig()
	if a.rawError == "" || !a.showRaw {
		t.Fatal("invalid config closed the editor")
	}
	got, err := os.ReadFile(a.cfgPath)
	if err != nil || string(got) != string(previous) || a.cfgText != string(previous) {
		t.Fatal("invalid save replaced working config")
	}
}
