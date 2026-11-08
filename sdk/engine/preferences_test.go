package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopurr/internal/core"
	"gopurr/internal/profile"
)

func TestRuntimePreferencesReadAndIsolation(t *testing.T) {
	home := t.TempDir()
	store, err := profile.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	p, err := store.AddBytesChecked("Local", []byte(directConfig), nil)
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{home: home, store: store, mode: "rule"}
	if err := e.loadPreferences(); err != nil || e.mode != "rule" {
		t.Fatalf("legacy home did not keep defaults: %v", err)
	}
	choices := map[string]map[string]string{p.ID: {"出口": "直连 B"}, "deleted": {"出口": "直连 A"}}
	if err := e.savePreferencesLocked("direct", choices); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, preferencesFile)
	info, err := os.Stat(path)
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		t.Fatalf("preferences are not private: %v, %v", info, err)
	}
	reopened := &Engine{home: home, store: store, mode: "rule"}
	if err := reopened.loadPreferences(); err != nil {
		t.Fatal(err)
	}
	if reopened.mode != "direct" || reopened.manualSelections[p.ID]["出口"] != "直连 B" || reopened.manualSelections["deleted"] != nil {
		t.Fatalf("wrong reopened preferences: %+v", reopened)
	}
	copy := cloneSelections(reopened.manualSelections)
	copy[p.ID]["出口"] = "直连 A"
	if reopened.manualSelections[p.ID]["出口"] != "直连 B" {
		t.Fatal("draft mutation leaked into committed preferences")
	}
}

func TestRuntimePreferencesRejectCorruptionWithoutOverwriting(t *testing.T) {
	for _, raw := range []string{`{`, `null`, `{"version":2,"mode":"direct"}`, `{"version":1,"mode":"unknown"}`, `{"version":1,"mode":"rule","selections":{"id":42}}`, strings.Repeat(" ", MaxDocumentBytes+1)} {
		t.Run(raw[:min(len(raw), 40)], func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, preferencesFile)
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			e := &Engine{home: home, mode: "rule"}
			if err := e.loadPreferences(); err == nil || asError(err).Code != "preferences_failed" {
				t.Fatalf("accepted invalid preferences: %v", err)
			}
			after, _ := os.ReadFile(path)
			if string(after) != raw {
				t.Fatal("unreadable preferences were overwritten")
			}
		})
	}
}

func TestRuntimePreferencesRejectLinkedReads(t *testing.T) {
	for _, link := range []struct {
		name string
		make func(string, string) error
	}{{"symlink", os.Symlink}, {"hardlink", os.Link}} {
		t.Run(link.name, func(t *testing.T) {
			home := t.TempDir()
			target := filepath.Join(t.TempDir(), "target.json")
			original := []byte(`{"version":1,"mode":"direct"}`)
			if err := os.WriteFile(target, original, 0644); err != nil {
				t.Fatal(err)
			}
			if err := link.make(target, filepath.Join(home, preferencesFile)); err != nil {
				t.Skip(err)
			}
			e := &Engine{home: home, mode: "rule"}
			if err := e.loadPreferences(); err == nil {
				t.Fatal("followed linked preferences")
			}
			if err := e.savePreferencesLocked("global", nil); err != nil {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(target)
			info, _ := os.Stat(target)
			if string(after) != string(original) || (runtime.GOOS != "windows" && info.Mode().Perm() != 0644) {
				t.Fatal("changed linked target contents or permissions")
			}
		})
	}
}

func TestModeSaveFailureDoesNotChangeStoppedEngine(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, preferencesFile), 0700); err != nil {
		t.Fatal(err)
	}
	e := &Engine{home: home, core: &core.Core{}, mode: "rule"}
	_, err := e.Call(context.Background(), "engine.mode", json.RawMessage(`{"mode":"direct"}`))
	if err == nil || asError(err).Code != "preferences_failed" || e.mode != "rule" {
		t.Fatalf("save failure changed mode: %s, %v", e.mode, err)
	}
}

func testLivePreferenceSaveFailure(t *testing.T, e *Engine, id string) {
	t.Helper()
	path := filepath.Join(e.home, preferencesFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	backup := path + ".test-backup"
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Remove(path); err != nil {
			t.Error(err)
		}
		if err := os.Rename(backup, path); err != nil {
			t.Error(err)
		}
	}()
	for method, params := range map[string]json.RawMessage{
		"engine.mode":    json.RawMessage(`{"mode":"rule"}`),
		"proxies.select": json.RawMessage(`{"group":"出口","member":"直连 A"}`),
	} {
		if _, err := e.Call(context.Background(), method, params); err == nil || asError(err).Code != "preferences_failed" {
			t.Fatalf("%s acknowledged failed persistence: %v", method, err)
		}
	}
	if e.core.Mode() != "direct" || e.mode != "direct" || e.groupsLocked().Groups[0].Now != "直连 B" || e.manualSelections[id]["出口"] != "直连 B" {
		t.Fatal("failed save changed live or remembered routing")
	}
	after, _ := os.ReadFile(backup)
	if string(after) != string(before) {
		t.Fatal("failed save altered prior disk preferences")
	}
}
