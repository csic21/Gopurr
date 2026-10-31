package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopurr/internal/applications"
	"gopurr/internal/patch"
	"gopurr/internal/profile"
)

func workflowSource(t *testing.T, a *App, name string) (profile.Profile, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.yaml")
	raw := "proxies:\n  - {name: HK, type: ss, server: one.example, port: 443, cipher: aes-128-gcm, password: secret}\nproxy-groups:\n  - {name: Select, type: select, proxies: [HK, DIRECT]}\nrules:\n  - MATCH,Select\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := a.store.AddFile(path, name)
	if err != nil {
		t.Fatal(err)
	}
	return p, path
}

func TestAggregateDependencyReloadKeepsLastGoodOnFailedRebuild(t *testing.T) {
	a := subscriptionTestApp(t)
	source, path := workflowSource(t, a, "source")
	aggregate, _, err := a.store.SaveAggregate("", "all", profile.AggregateSpec{Sources: []profile.AggregateSource{{ID: source.ID}}, Include: "^HK$", Dedupe: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.SetActive(aggregate.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("proxies: [{name: HK, type: ss, server: two.example, port: 443, cipher: aes-128-gcm, password: fresh}]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.refreshManagedProfile(source.ID); err != nil {
		t.Fatal(err)
	}
	before, err := a.store.Content(aggregate.ID)
	if err != nil || !strings.Contains(string(before), "two.example") {
		t.Fatalf("aggregate not refreshed: %v", err)
	}
	runtime, err := os.ReadFile(a.mgr.ConfigPath())
	if err != nil || !strings.Contains(string(runtime), "two.example") {
		t.Fatalf("active aggregate not reloaded: %v", err)
	}
	if err := os.WriteFile(path, []byte("proxies: [{name: JP, type: ss, server: three.example, port: 443, cipher: aes-128-gcm, password: fresh}]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.refreshManagedProfile(source.ID); err == nil {
		t.Fatal("empty filtered aggregate accepted")
	}
	after, _ := a.store.Content(aggregate.ID)
	if string(after) != string(before) {
		t.Fatal("failed rebuild replaced last good aggregate")
	}
	runtime, _ = os.ReadFile(a.mgr.ConfigPath())
	if strings.Contains(string(runtime), "three.example") {
		t.Fatal("failed rebuild changed running config")
	}
}

func TestBackupPreservesAggregateTemplateAndProxyEdits(t *testing.T) {
	a := subscriptionTestApp(t)
	source, _ := workflowSource(t, a, "source")
	aggregate, _, err := a.store.SaveAggregate("", "all", profile.AggregateSpec{Sources: []profile.AggregateSource{{ID: source.ID, Prefix: "Work "}}, Dedupe: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.ov.Library = []patch.OverrideTemplate{{ID: "work", Name: "Work defaults", YAML: "tcp-concurrent: true\n"}}
	name := "My Exit"
	local := patch.ProfileOverrides{TemplateIDs: []string{"work"}, ProxyEdits: &patch.ProxyEdits{Groups: []patch.GroupEdit{{Source: profile.AggregateSelect, Name: &name}}}}
	if err := a.store.SetOverrides(aggregate.ID, local); err != nil {
		t.Fatal(err)
	}
	if err := a.store.SetActive(aggregate.ID); err != nil {
		t.Fatal(err)
	}
	s, err := a.backupSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	_, restored, count, err := a.prepareRestore(s)
	if err != nil || count != 2 || len(restored.Library) != 1 {
		t.Fatalf("new config did not validate in backup: %v", err)
	}
	if err := a.restoreBackup(s); err != nil {
		t.Fatal(err)
	}
	raw, err := a.configBytes()
	if err != nil || !strings.Contains(string(raw), "My Exit") || !strings.Contains(string(raw), "tcp-concurrent: true") {
		t.Fatalf("composed layers not restored: %v", err)
	}
	p, _ := a.store.Get(aggregate.ID)
	if p.Aggregate == nil || p.Aggregate.Sources[0].ID != source.ID {
		t.Fatal("aggregate sources not restored")
	}
	var index struct {
		Profiles []profile.Profile `json:"profiles"`
		Active   string            `json:"active"`
	}
	if err := json.Unmarshal(s.Files["profiles.json"], &index); err != nil {
		t.Fatal(err)
	}
	for i := range index.Profiles {
		if index.Profiles[i].IsAggregate() {
			index.Profiles[i].Aggregate.Sources[0].ID = "missing-source"
		}
	}
	s.Files["profiles.json"], _ = json.Marshal(index)
	if _, _, _, err := a.prepareRestore(s); err == nil {
		t.Fatal("backup accepted missing aggregate source")
	}
}

func TestProxyRenameMapsAdditionalPoliciesAndEditorCanReopen(t *testing.T) {
	a := subscriptionTestApp(t)
	source, _ := workflowSource(t, a, "source")
	rename := "Work Exit"
	local := patch.ProfileOverrides{ProxyEdits: &patch.ProxyEdits{Groups: []patch.GroupEdit{{Source: "Select", Name: &rename}}}}
	a.ov.ExtraRules = []string{"DOMAIN,work.example,Select"}
	if _, err := a.commitProfileOverrides(source.ID, local); err != nil {
		t.Fatal(err)
	}
	raw, err := a.configBytes()
	if err != nil || !strings.Contains(string(raw), "DOMAIN,work.example,Work Exit") {
		t.Fatalf("extra policy not mapped: %v", err)
	}
	// Policies created after the rename already contain the new identity.
	a.ov.ExtraRules = []string{"DOMAIN,work.example,Work Exit"}
	a.openProfileProxyEditor(source.ID)
	if !a.showProxyEditor || a.proxyEditorGroups["Select"] == nil {
		t.Fatalf("could not reopen renamed editor: %s", a.notice)
	}
}

func TestReimportUpdatesActiveAggregateWithoutRefetch(t *testing.T) {
	a := subscriptionTestApp(t)
	source, path := workflowSource(t, a, "source")
	aggregate, _, err := a.store.SaveAggregate("", "all", profile.AggregateSpec{Sources: []profile.AggregateSource{{ID: source.ID}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.SetActive(aggregate.ID); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	updated := strings.ReplaceAll(string(body), "one.example", "new.example")
	if err := os.WriteFile(path, []byte(updated), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.importManagedProfile(func(accept func(profile.Profile, []byte) error) (profile.Profile, error) {
		return a.store.AddFileChecked(path, "", accept)
	}); err != nil {
		t.Fatal(err)
	}
	raw, _ := a.store.Content(aggregate.ID)
	if !strings.Contains(string(raw), "new.example") {
		t.Fatal("reimport left aggregate stale")
	}
	runtime, _ := os.ReadFile(a.mgr.ConfigPath())
	if !strings.Contains(string(runtime), "new.example") {
		t.Fatal("reimport did not reload active aggregate")
	}
}

func TestReimportRejectsStaleEditsWithoutReplacingSource(t *testing.T) {
	a := subscriptionTestApp(t)
	source, path := workflowSource(t, a, "source")
	name := "Local HK"
	local := patch.ProfileOverrides{ProxyEdits: &patch.ProxyEdits{Nodes: []patch.NodeEdit{{Source: "HK", Name: &name}}}}
	if _, err := a.commitProfileOverrides(source.ID, local); err != nil {
		t.Fatal(err)
	}
	before, _ := a.store.Content(source.ID)
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(before), "HK", "JP")), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.importManagedProfile(func(accept func(profile.Profile, []byte) error) (profile.Profile, error) {
		return a.store.AddFileChecked(path, "", accept)
	}); err == nil {
		t.Fatal("stale edited source accepted")
	}
	after, _ := a.store.Content(source.ID)
	if string(after) != string(before) {
		t.Fatal("rejected reimport changed source cache")
	}
	runtime, _ := os.ReadFile(a.mgr.ConfigPath())
	if !strings.Contains(string(runtime), "Local HK") {
		t.Fatal("rejected reimport changed running config")
	}
}

func TestInactiveProfileValidationDoesNotInheritCurrentAppPolicies(t *testing.T) {
	a := subscriptionTestApp(t)
	active, _ := workflowSource(t, a, "active")
	inactive, _ := workflowSource(t, a, "inactive")
	rename := "Only Active"
	if _, err := a.commitProfileOverrides(active.ID, patch.ProfileOverrides{ProxyEdits: &patch.ProxyEdits{Groups: []patch.GroupEdit{{Source: "Select", Name: &rename}}}}); err != nil {
		t.Fatal(err)
	}
	a.ov.AppRules = []patch.AppRule{{Application: applications.Application{Name: "Browser", Path: "/Applications/Browser.app", Executable: "/Applications/Browser.app/Contents/MacOS/Browser"}, Target: rename}}
	if err := a.refreshManagedProfileOnly(inactive.ID); err != nil {
		t.Fatalf("inactive refresh inherited current app policy: %v", err)
	}
	if _, err := a.commitProfileOverrides(inactive.ID, patch.ProfileOverrides{}); err != nil {
		t.Fatalf("inactive editor inherited current app policy: %v", err)
	}
	next := a.ov
	next.Library = []patch.OverrideTemplate{{ID: "common", Name: "Common", YAML: "tcp-concurrent: true\n"}}
	if _, err := a.commitOverrideLibrary(next); err != nil {
		t.Fatalf("library validation inherited current app policy: %v", err)
	}
	s, err := a.backupSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := a.prepareRestore(s); err != nil {
		t.Fatalf("backup inherited current policy into inactive profile: %v", err)
	}
}
