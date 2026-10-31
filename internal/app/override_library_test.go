package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/patch"
	"gopurr/internal/profile"
)

func TestOverrideLibraryDraftsOrderAndDeletionProtection(t *testing.T) {
	a := extensionTestApp(t)
	a.openOverrideLibrary()
	a.addImportedOverrideTemplate("DNS primary", "dns:\n  nameserver: [1.1.1.1]")
	a.addImportedOverrideTemplate("DNS fallback", "dns:\n  nameserver: [9.9.9.9]")
	if len(a.ov.Library) != 0 || len(a.libraryDraft) != 2 {
		t.Fatal("import changed live definitions")
	}
	a.saveOverrideLibraryDraft()
	if a.libraryError != "" || a.libraryDirty() || len(a.ov.Library) != 2 {
		t.Fatalf("save failed: %s", a.libraryError)
	}
	first, second := a.ov.Library[0].ID, a.ov.Library[1].ID
	a.addTemplateID(first)
	a.addTemplateID(second)
	a.moveTemplateID(1, -1)
	a.saveOverrideTemplateSelection()
	if a.libraryError != "" || !slices.Equal(a.ov.TemplateIDs, []string{second, first}) {
		t.Fatalf("order not saved: %s", a.libraryError)
	}
	a.librarySelected = first
	a.removeOverrideTemplate()
	if a.libraryError == "" || len(a.libraryDraft) != 2 {
		t.Fatal("deleted a referenced template")
	}
	// Changing display names does not break ID-based attachment.
	a.libraryDraft[0].Name = "Renamed"
	a.saveOverrideLibraryDraft()
	if a.libraryError != "" || a.ov.Library[0].Name != "Renamed" || a.ov.TemplateIDs[1] != first {
		t.Fatal("rename changed references")
	}
	a.libraryIDs = []string{second}
	a.saveOverrideTemplateSelection()
	a.librarySelected = first
	a.removeOverrideTemplate()
	a.saveOverrideLibraryDraft()
	if a.libraryError != "" || len(a.ov.Library) != 1 {
		t.Fatalf("detached template could not be deleted: %s", a.libraryError)
	}
	var saved patch.Overrides
	if err := json.Unmarshal(readExtensionFile(t, a.ovPath), &saved); err != nil || len(saved.Library) != 1 || !slices.Equal(saved.TemplateIDs, []string{second}) {
		t.Fatalf("library not persisted: %v", err)
	}
}

func TestOverrideLibraryValidatesInactiveSubscriptionsAndRollsBack(t *testing.T) {
	a := extensionTestApp(t)
	var err error
	a.store, err = profile.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "inactive.yaml")
	if err := os.WriteFile(file, []byte("rules: [MATCH,DIRECT]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := a.store.AddFile(file, "Inactive")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.SetActive(""); err != nil {
		t.Fatal(err)
	}
	next := a.ov
	next.Library = []patch.OverrideTemplate{{ID: "work", Name: "Work", YAML: "tcp-concurrent: true"}}
	if _, err := a.commitOverrideLibrary(next); err != nil {
		t.Fatal(err)
	}
	if err := a.store.SetOverrides(p.ID, patch.ProfileOverrides{TemplateIDs: []string{"work"}}); err != nil {
		t.Fatal(err)
	}
	before := readExtensionFile(t, a.ovPath)
	next = a.ov
	next.Library = slices.Clone(a.ov.Library)
	next.Library[0].YAML = "rules:\n  - MATCH,MissingGroup"
	if _, err := a.commitOverrideLibrary(next); err == nil {
		t.Fatal("inactive subscription broken by edit")
	}
	if string(before) != string(readExtensionFile(t, a.ovPath)) || a.ov.Library[0].YAML != "tcp-concurrent: true" {
		t.Fatal("invalid edit changed saved library")
	}
	// Deleting a template is rejected even if only an inactive profile uses it.
	next = a.ov
	next.Library = nil
	if _, err := a.commitOverrideLibrary(next); err == nil {
		t.Fatal("inactive reference lost")
	}
	// A failed atomic write keeps definitions and application order unchanged.
	next = a.ov
	next.Library = slices.Clone(a.ov.Library)
	next.Library[0].Name = "Candidate"
	a.ovPath = t.TempDir()
	if _, err := a.commitOverrideLibrary(next); err == nil || a.ov.Library[0].Name != "Work" {
		t.Fatal("failed write published candidate")
	}
}

func TestOverrideLibraryScopeIsolationAndCancel(t *testing.T) {
	a := extensionTestApp(t)
	var err error
	a.store, err = profile.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "profile.yaml")
	if err := os.WriteFile(file, []byte("rules: [MATCH,DIRECT]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := a.store.AddFile(file, "Work")
	if err != nil {
		t.Fatal(err)
	}
	next := a.ov
	next.Library = []patch.OverrideTemplate{{ID: "one", Name: "One", YAML: "tcp-concurrent: true"}}
	if _, err := a.commitOverrideLibrary(next); err != nil {
		t.Fatal(err)
	}
	a.extensionScope, a.extensionProfileID = 1, p.ID
	a.openOverrideLibrary()
	a.addTemplateID("one")
	a.saveOverrideTemplateSelection()
	saved, err := a.store.Get(p.ID)
	if err != nil || saved.Overrides == nil || !slices.Equal(saved.Overrides.TemplateIDs, []string{"one"}) || len(a.ov.TemplateIDs) != 0 {
		t.Fatal("scope leaked")
	}
	a.libraryDraft[0].Name = "Unsaved"
	a.closeOverrideLibrary()
	a.openOverrideLibrary()
	if a.libraryDraft[0].Name != "One" || !slices.Equal(a.libraryIDs, []string{"one"}) {
		t.Fatal("cancel kept unsaved definitions or lost saved selection")
	}
}

func TestOverrideLibraryRejectsBadImportAndUnsavedDefinitions(t *testing.T) {
	a := extensionTestApp(t)
	a.openOverrideLibrary()
	a.addImportedOverrideTemplate("Not YAML", "[list]")
	if len(a.libraryDraft) != 0 || a.libraryError == "" {
		t.Fatal("bad import appended")
	}
	a.addImportedOverrideTemplate("Good", "tcp-concurrent: true")
	a.libraryIDs = []string{a.libraryDraft[0].ID}
	a.saveOverrideTemplateSelection()
	if a.libraryError == "" || len(a.ov.Library) != 0 || len(a.ov.TemplateIDs) != 0 {
		t.Fatal("selection implicitly saved definitions")
	}
}

func TestOverrideLibraryFitsMinimumWindow(t *testing.T) {
	a, _ := newSettingsTestApp(t, nil)
	a.booted, a.ticking, a.page = true, true, "rules"
	a.openOverrideLibrary()
	a.libraryDraft = []patch.OverrideTemplate{{ID: "long", Name: strings.Repeat("Work profile ", 12), YAML: "tcp-concurrent: true\n"}}
	a.libraryBase = slices.Clone(a.libraryDraft)
	a.librarySelected = "long"
	tr := ui.NewTester(a.View, 840, 560)
	tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 80})
	tr.Frame()
	for _, label := range []string{"保存模板库", "取消", "模板名称"} {
		r, ok := tr.Find(label)
		if !ok || r.X < 0 || r.X+r.W > 840 || r.Y+r.H > 560 {
			t.Fatalf("clipped %q: %+v", label, r)
		}
	}
	a.libraryError = strings.Repeat("请检查模板中的字段和应用顺序。", 50)
	tr.Frame()
	for _, label := range []string{"保存模板库", "取消"} {
		r, ok := tr.Find(label)
		if !ok || r.Y+r.H > 560 {
			t.Fatalf("long error hid %q: %+v", label, r)
		}
	}
	before := slices.Clone(a.ov.Library)
	if err := tr.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if a.showOverrideLibrary || !reflect.DeepEqual(a.ov.Library, before) {
		t.Fatal("cancel saved template edits")
	}
}

func TestOverrideLibraryRepairRollsBackLastAcceptedRuntime(t *testing.T) {
	for _, outcome := range []string{"accepted", "write failed", "kernel rejected"} {
		t.Run(outcome, func(t *testing.T) {
			a, reloads := newSettingsTestApp(t, func(raw string) bool {
				return outcome == "kernel rejected" && strings.Contains(raw, "tcp-concurrent: true")
			})
			accepted, err := os.ReadFile(a.mgr.ConfigPath())
			if err != nil {
				t.Fatal(err)
			}
			// Model a saved template becoming incompatible after the source is
			// changed externally. The kernel still has its earlier valid config.
			a.ov.Library = []patch.OverrideTemplate{{ID: "work", Name: "Work", YAML: "rules:\n  - MATCH,MissingGroup\n"}}
			a.ov.TemplateIDs = []string{"work"}
			if err := a.saveOverridesAtomic(); err != nil {
				t.Fatal(err)
			}
			next := a.ov
			next.Library = slices.Clone(a.ov.Library)
			next.Library[0].YAML = "tcp-concurrent: true"
			if outcome == "write failed" {
				a.ovPath = t.TempDir()
			}
			_, err = a.commitOverrideLibrary(next)
			if outcome == "accepted" {
				if err != nil || a.ov.Library[0].YAML != "tcp-concurrent: true" {
					t.Fatalf("valid repair rejected: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("failure was accepted")
			}
			if a.ov.Library[0].YAML != "rules:\n  - MATCH,MissingGroup\n" {
				t.Fatal("failed repair published preferences")
			}
			requests := reloads()
			if len(requests) != 2 || requests[1] != string(accepted) {
				t.Fatalf("did not restore actual accepted runtime: %d requests, %v", len(requests), err)
			}
		})
	}
}
