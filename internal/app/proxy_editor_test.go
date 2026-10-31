package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/patch"
)

const proxyEditorSource = `proxies:
  - {name: One, type: ss, server: one.example.com, port: 443, cipher: aes-128-gcm, password: initial-secret}
  - {name: Two, type: trojan, server: two.example.com, port: 8443, password: token}
proxy-groups:
  - {name: Choice, type: select, proxies: [One, Two, DIRECT]}
rules: ["MATCH,Choice"]
`

func proxyEditorTestApp(t *testing.T) (*App, string, string) {
	t.Helper()
	a := subscriptionTestApp(t)
	a.booted, a.ticking = true, true
	path := filepath.Join(a.dir, "source.yaml")
	if err := os.WriteFile(path, []byte(proxyEditorSource), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := a.store.AddFile(path, "Editor source")
	if err != nil {
		t.Fatal(err)
	}
	a.openProfileProxyEditor(p.ID)
	if !a.showProxyEditor {
		t.Fatalf("editor did not open: %s", a.notice)
	}
	return a, p.ID, path
}

func TestProxyEditorSurgicalSaveSurvivesSourceRefresh(t *testing.T) {
	a, id, path := proxyEditorTestApp(t)
	a.proxyEditorNodes["One"].Name = "Local One"
	a.proxyEditorNodes["One"].Server = "local.example.com"
	a.saveProxyEditor()
	if a.showProxyEditor || a.proxyEditorError != "" {
		t.Fatalf("save failed: %s", a.proxyEditorError)
	}
	p, _ := a.store.Get(id)
	if p.Overrides == nil || p.Overrides.ProxyEdits == nil || len(p.Overrides.ProxyEdits.Nodes) != 1 || len(p.Overrides.ProxyEdits.Groups) != 0 {
		t.Fatal("saved a snapshot instead of surgical edits")
	}
	if p.Overrides.ProxyEdits.Nodes[0].Port != nil {
		t.Fatal("untouched port was persisted")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != proxyEditorSource {
		t.Fatal("editor overwrote source file")
	}
	updated := strings.Replace(proxyEditorSource, "initial-secret", "fresh-secret", 1)
	updated = strings.Replace(updated, "[One, Two, DIRECT]", "[One, Two, REJECT, DIRECT]", 1)
	if err := os.WriteFile(path, []byte(updated), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Refresh(id); err != nil {
		t.Fatal(err)
	}
	composed, err := a.configForProfile(id, *p.Overrides)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(composed), "fresh-secret") || !strings.Contains(string(composed), "Local One") || !strings.Contains(string(composed), "REJECT") {
		t.Fatal("refresh lost current subscription values or editor changes")
	}
}

func TestProxyEditorMembersOrderValidationAndCancel(t *testing.T) {
	a, id, _ := proxyEditorTestApp(t)
	a.proxyEditorTab, a.proxyEditorGroup = 1, "Choice"
	tr := ui.NewTester(a.proxyEditorDialog, 840, 560)
	tr.SetPreferences(ui.Preferences{ReduceMotion: true})
	tr.Frame()
	if err := tr.Click("上移第 2 个成员"); err != nil {
		t.Fatal(err)
	}
	f := a.proxyEditorGroups["Choice"]
	if !f.MembersTouched || !reflect.DeepEqual(f.Members, []string{"Two", "One", "DIRECT"}) {
		t.Fatal("member order action did not edit draft")
	}
	f.Members = []string{"Choice"}
	f.MembersTouched = true
	a.saveProxyEditor()
	if a.proxyEditorError == "" || !a.showProxyEditor {
		t.Fatal("cycle did not retain editor draft")
	}
	p, _ := a.store.Get(id)
	if p.Overrides != nil && p.Overrides.ProxyEdits != nil {
		t.Fatal("invalid group saved")
	}
	tr.Frame()
	for _, label := range []string{"恢复所选项", "取消", "保存并应用"} {
		r, ok := tr.Find(label)
		if !ok || r.Y+r.H > 560 || r.X+r.W > 840 {
			t.Fatalf("minimum window clipped %s: %+v", label, r)
		}
	}
	if err := tr.Click("取消"); err != nil {
		t.Fatal(err)
	}
	a.openProfileProxyEditor(id)
	if !reflect.DeepEqual(a.proxyEditorGroups["Choice"].Members, []string{"One", "Two", "DIRECT"}) {
		t.Fatal("cancel leaked member draft")
	}
}

func TestProxyEditorCanClearStaleAndReopenRenamedPolicies(t *testing.T) {
	a, id, path := proxyEditorTestApp(t)
	a.proxyEditorNodes["One"].Name = "Local One"
	a.proxyEditorGroups["Choice"].Name = "Work"
	a.saveProxyEditor()
	local := a.profileOverrides(id)
	local.ExtraRules = []string{"DOMAIN,work.example.com,Work"}
	if _, err := a.commitProfileOverrides(id, local); err != nil {
		t.Fatal(err)
	}
	a.openProfileProxyEditor(id)
	if !a.showProxyEditor || a.proxyEditorGroups["Choice"].Name != "Work" {
		t.Fatal("saved policy targets prevented reopening")
	}
	updated := strings.Replace(proxyEditorSource, "  - {name: One, type: ss, server: one.example.com, port: 443, cipher: aes-128-gcm, password: initial-secret}\n", "", 1)
	updated = strings.Replace(updated, "[One, Two, DIRECT]", "[Two, DIRECT]", 1)
	if err := os.WriteFile(path, []byte(updated), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Refresh(id); err != nil {
		t.Fatal(err)
	}
	a.openProfileProxyEditor(id)
	if !a.proxyEditorNodes["One"].Missing {
		t.Fatal("missing source not recoverable")
	}
	a.proxyEditorNode = "One"
	a.proxyEditorTab = 0
	a.resetProxyEditorItem()
	a.saveProxyEditor()
	if a.proxyEditorError != "" || a.showProxyEditor {
		t.Fatalf("could not clear stale edit: %s", a.proxyEditorError)
	}
	local = a.profileOverrides(id)
	if local.ProxyEdits == nil || len(local.ProxyEdits.Nodes) != 0 || len(local.ProxyEdits.Groups) != 1 {
		t.Fatal("clearing a stale node discarded unrelated group edit")
	}
}

func TestProxyEditorNewGroupAndIndependentDrafts(t *testing.T) {
	a, _, _ := proxyEditorTestApp(t)
	a.proxyEditorNodes["One"].Name = "Renamed"
	a.addProxyEditorGroup()
	f := a.proxyEditorGroups[a.proxyEditorGroup]
	f.Name = "Local group"
	f.Type = "fallback"
	f.Members = []string{"Renamed", "Two"}
	edits, err := a.proxyEditorEdits()
	if err != nil {
		t.Fatal(err)
	}
	if len(edits.Nodes) != 1 || len(edits.Groups) != 1 || edits.Groups[0].Source != "" {
		t.Fatal("new group not represented independently")
	}
	if _, err := patch.PreviewProxyEdits([]byte(a.proxyEditorRaw), edits); err != nil {
		t.Fatal(err)
	}
	a.proxyEditorTab, a.proxyEditorNode = 0, "One"
	a.resetProxyEditorItem()
	if f.Members[0] != "One" {
		t.Fatal("restoring node left explicit member references dangling")
	}
}

func TestProxyEditorRepeatedRenameKeepsSavedPolicies(t *testing.T) {
	a, id, _ := proxyEditorTestApp(t)
	a.proxyEditorGroups["Choice"].Name = "Work"
	a.saveProxyEditor()
	local := a.profileOverrides(id)
	local.ExtraRules = []string{"DOMAIN,work.example.com,Work"}
	if _, err := a.commitProfileOverrides(id, local); err != nil {
		t.Fatal(err)
	}
	a.openProfileProxyEditor(id)
	a.proxyEditorGroups["Choice"].Name = "Work v2"
	a.saveProxyEditor()
	if a.proxyEditorError != "" || a.showProxyEditor {
		t.Fatalf("repeated rename failed: %s", a.proxyEditorError)
	}
	local = a.profileOverrides(id)
	if local.ProxyEdits.Target("Work") != "Work v2" {
		t.Fatal("history not persisted")
	}
	raw, err := a.configForProfile(id, local)
	if err != nil || !strings.Contains(string(raw), "DOMAIN,work.example.com,Work v2") {
		t.Fatalf("policy not migrated: %s %v", raw, err)
	}
}

func TestProxyEditorReopensTemplateRulesReferencingEditedNames(t *testing.T) {
	for _, added := range []bool{false, true} {
		t.Run(map[bool]string{false: "renamed", true: "added"}[added], func(t *testing.T) {
			a, id, _ := proxyEditorTestApp(t)
			if added {
				a.addProxyEditorGroup()
				a.proxyEditorGroups[a.proxyEditorGroup].Name = "Work"
			} else {
				a.proxyEditorGroups["Choice"].Name = "Work"
			}
			a.saveProxyEditor()
			if a.proxyEditorError != "" {
				t.Fatal(a.proxyEditorError)
			}
			a.ov.Library = []patch.OverrideTemplate{{ID: "work", Name: "Work routing", YAML: "rules: [\"MATCH,Work\"]"}}
			local := a.profileOverrides(id)
			local.TemplateIDs = []string{"work"}
			if _, err := a.commitProfileOverrides(id, local); err != nil {
				t.Fatal(err)
			}
			a.showProxyEditor = false
			a.notice = ""
			a.openProfileProxyEditor(id)
			if !a.showProxyEditor {
				t.Fatalf("template rule prevented reopening: %s", a.notice)
			}
		})
	}
}
