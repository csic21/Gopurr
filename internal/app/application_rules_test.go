package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/applications"
	"gopurr/internal/patch"
)

func fixtureApplicationRules(a *App) {
	a.page, a.extensionTab = "rules", 1
	a.appRulesReady, a.appCatalogReady = true, true
	a.appCatalog = []applications.Application{
		{Name: "Work Browser · 工作与家庭共用的浏览器", Path: "/Applications/Work Browser.app", Executable: "/Applications/Work Browser.app/Contents/MacOS/Work Browser"},
		{Name: "Mail", Path: "/System/Applications/Mail.app", Executable: "/System/Applications/Mail.app/Contents/MacOS/Mail"},
		{Name: "Chat", Path: "/Applications/Chat.app", Executable: "/Applications/Chat.app/Contents/MacOS/Chat"},
		{Name: "Notes", Path: "/System/Applications/Notes.app", Executable: "/System/Applications/Notes.app/Contents/MacOS/Notes"},
		{Name: "Studio · 多媒体设计工具与自动更新程序", Path: "/Users/demo/Applications/Creative Tools/Studio Design and Collaboration.app", Executable: "/Users/demo/Applications/Creative Tools/Studio Design and Collaboration.app/Contents/MacOS/Studio"},
	}
	a.ov.AppRules = []patch.AppRule{
		{Application: a.appCatalog[0], Target: "漏网之鱼 · 工作与家庭共用的长名称策略组", IncludeHelpers: true},
		{Application: a.appCatalog[1], Target: "DIRECT", IncludeHelpers: true},
		{Application: a.appCatalog[2], Target: "REJECT", IncludeHelpers: false},
	}
	a.appRulesDraft = slices.Clone(a.ov.AppRules)
}

func TestApplicationRuleDraftPersistence(t *testing.T) {
	a := extensionTestApp(t)
	a.cfgText = "proxy-groups:\n  - name: Work\n    type: select\n    proxies: [DIRECT]\nrules:\n  - MATCH,DIRECT\n"
	a.extraRulesDraft, a.mergeYAMLDraft = "unfinished rules", "unfinished: ["
	app := applications.Application{Name: "Browser", Path: "/Applications/Browser.app", Executable: "/Applications/Browser.app/Contents/MacOS/Main"}
	a.openApplicationEditor(app)
	if !a.appEdit.IncludeHelpers || !slices.Equal(a.appEditGroups, []string{"Work"}) {
		t.Fatalf("stopped-kernel groups or helper default missing: %+v", a.appEdit)
	}
	a.appEditKind, a.appEditGroup = 2, "Work"
	a.stageApplicationRule()
	if len(a.ov.AppRules) != 0 || len(a.appRulesDraft) != 1 {
		t.Fatal("staging applied a rule or failed to add it")
	}
	if err := a.saveApplicationRules(); err != nil {
		t.Fatal(err)
	}
	if a.extraRulesDraft != "unfinished rules" || a.mergeYAMLDraft != "unfinished: [" || len(a.ov.ExtraRules) != 1 {
		t.Fatal("saving application routes consumed other editor drafts")
	}
	var persisted patch.Overrides
	if err := json.Unmarshal(readExtensionFile(t, a.ovPath), &persisted); err != nil || !slices.Equal(persisted.AppRules, a.appRulesDraft) {
		t.Fatalf("application routes not persisted: %+v, %v", persisted, err)
	}
	before := readExtensionFile(t, a.ovPath)
	a.openApplicationEditor(app)
	a.appEditKind, a.appEdit.IncludeHelpers = 1, false
	a.stageApplicationRule()
	if len(a.appRulesDraft) != 1 || a.ov.AppRules[0].Target != "Work" || !a.ov.AppRules[0].IncludeHelpers {
		t.Fatal("editing duplicated an application or mutated the saved slice")
	}
	a.appRulesDraft[0].Target = "Removed Group"
	if err := a.saveApplicationRules(); err == nil || a.appRulesError == "" || !bytes.Equal(before, readExtensionFile(t, a.ovPath)) || a.ov.AppRules[0].Target != "Work" {
		t.Fatal("invalid policy was not rolled back")
	}
	a.appRulesDraft[0].Target = "REJECT"
	originalPath := a.ovPath
	a.ovPath = filepath.Join(filepath.Dir(originalPath), "blocked")
	if err := os.Mkdir(a.ovPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := a.saveApplicationRules(); err == nil || a.ov.AppRules[0].Target != "Work" || !bytes.Equal(before, readExtensionFile(t, originalPath)) {
		t.Fatal("write failure changed saved policies")
	}
	a.ovPath = originalPath
	a.appRulesDraft = nil
	if err := a.saveApplicationRules(); err != nil || len(a.ov.AppRules) != 0 {
		t.Fatalf("remove all application rules: %v", err)
	}
}

func TestApplicationRulePickerInteraction(t *testing.T) {
	a := extensionTestApp(t)
	a.cfgText = "proxy-groups:\n  - name: Work\n    type: select\n    proxies: [DIRECT]\nrules:\n  - MATCH,DIRECT\n"
	a.appCatalogReady = true
	a.appCatalog = []applications.Application{
		{Name: "Browser", Path: "/Applications/Browser.app", Executable: "/Applications/Browser.app/Contents/MacOS/Browser"},
		{Name: "Other", Path: "/Applications/Other.app", Executable: "/Applications/Other.app/Contents/MacOS/Other"},
	}
	tr := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme(c.Theme().Dark))
		ui.Column(c).Fill().Padding(20).Gap(12).Children(func() { a.rulesPage(c) })
	}, 840, 560)
	tr.Frame()
	click := func(label string) {
		t.Helper()
		if err := tr.Click(label); err != nil {
			t.Fatal(err)
		}
		tr.Frame()
	}
	click("应用分流")
	click("添加应用…")
	click("搜索应用名称或路径…")
	tr.Type("browser")
	tr.Frame()
	if !tr.HasText("Browser") || tr.HasText("Other") {
		t.Fatal("application search did not filter the catalog")
	}
	click("选择应用 Browser")
	if !a.showAppEditor || a.showAppPicker || !a.appEdit.IncludeHelpers {
		t.Fatal("selecting an app did not open its policy with helpers enabled")
	}
	click("代理组")
	if _, ok := tr.Find("应用代理组"); !ok || a.appEditGroup != "Work" {
		t.Fatal("proxy group selection missing")
	}
	click("添加到草稿")
	click("保存应用分流")
	if len(a.ov.AppRules) != 1 || a.ov.AppRules[0].Target != "Work" {
		t.Fatal("picker workflow did not persist the application policy")
	}
	body, err := a.configBytes()
	if err != nil || !strings.Contains(string(body), "PROCESS-PATH-REGEX") || !strings.Contains(string(body), "find-process-mode: always") {
		t.Fatalf("application policy missing from composed runtime: %s, %v", body, err)
	}
	click("编辑应用分流 Browser")
	click("拦截")
	click("包含应用 Helper")
	click("更新草稿")
	if a.ov.AppRules[0].Target != "Work" || a.appRulesDraft[0].Target != "REJECT" || a.appRulesDraft[0].IncludeHelpers {
		t.Fatal("edit ignored helper choice or changed saved state")
	}
	click("放弃修改")
	click("移除应用分流 Browser")
	click("保存应用分流")
	if len(a.ov.AppRules) != 0 {
		t.Fatal("removing a policy did not persist")
	}
}

func TestApplicationScanResultsStayOnUIThread(t *testing.T) {
	a := extensionTestApp(t)
	a.appCatalogBusy = true
	a.appCatalogResult = make(chan applicationScanResult, 1)
	a.appCatalogResult <- applicationScanResult{apps: []applications.Application{{Name: "App", Path: "/Applications/App.app"}}}
	a.pollApplicationResults()
	if !a.appCatalogReady || a.appCatalogBusy || len(a.appCatalog) != 1 {
		t.Fatal("scan completion not published")
	}
	// A cancelled picker must not reappear when its native dialog finishes.
	a.showAppPicker = false
	a.appFileResult = make(chan applicationScanResult, 1)
	a.appFileResult <- applicationScanResult{apps: a.appCatalog}
	a.pollApplicationResults()
	if a.showAppEditor || a.appFileResult != nil {
		t.Fatal("late native picker result reopened a dismissed editor")
	}
}

func TestApplicationRuleReloadAndBackup(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "rejected"}[rejected], func(t *testing.T) {
			a, payloads := newSettingsTestApp(t, func(raw string) bool {
				return rejected && strings.Contains(raw, "PROCESS-PATH-REGEX")
			})
			before := readExtensionFile(t, a.ovPath)
			a.openApplicationEditor(applications.Application{Name: "Browser", Path: "/Applications/Browser.app", Executable: "/Applications/Browser.app/Contents/MacOS/Main"})
			a.stageApplicationRule()
			err := a.saveApplicationRules()
			if rejected {
				if err == nil || len(a.ov.AppRules) != 0 || len(a.appRulesDraft) != 1 || !bytes.Equal(before, readExtensionFile(t, a.ovPath)) {
					t.Fatal("kernel rejection lost the draft or changed saved routes")
				}
				return
			}
			if err != nil || len(payloads()) != 1 || !strings.Contains(payloads()[0], "PROCESS-PATH-REGEX") || !strings.Contains(a.appRulesStatus, "重新载入") {
				t.Fatalf("running kernel did not receive policy: %v, %v", err, payloads())
			}
			snapshot, err := a.backupSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			a.appRulesDraft = nil
			if err := a.saveApplicationRules(); err != nil {
				t.Fatal(err)
			}
			if err := a.restoreBackup(snapshot); err != nil {
				t.Fatal(err)
			}
			a.initApplicationRules()
			if len(a.appRulesDraft) != 1 || !a.appRulesDraft[0].IncludeHelpers || a.appRulesDraft[0].Application.Name != "Browser" {
				t.Fatal("backup did not restore policies and refresh their editor")
			}
		})
	}
}
