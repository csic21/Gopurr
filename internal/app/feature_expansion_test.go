package app

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/core"
	"gopurr/internal/diagnostic"
	"gopurr/internal/patch"
	"gopurr/internal/profile"
)

func addFeatureProfile(t *testing.T, a *App, name string) profile.Profile {
	t.Helper()
	file := filepath.Join(a.dir, name+".yaml")
	if err := os.WriteFile(file, []byte("rules: [\"MATCH,DIRECT\"]"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := a.store.AddFile(file, name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProfileOverridesSwitchBackupAndSourceRefresh(t *testing.T) {
	a, _ := newSettingsTestApp(t, nil)
	first := addFeatureProfile(t, a, "work")
	second := addFeatureProfile(t, a, "personal")
	dns := patch.DefaultDNSSettings()
	dns.Nameserver = []string{"119.29.29.29"}
	if _, err := a.commitProfileOverrides(first.ID, patch.ProfileOverrides{ExtraRules: []string{"DOMAIN,work.example,DIRECT"}, DNS: &dns}); err != nil {
		t.Fatal(err)
	}
	if err := a.refreshManagedProfile(first.ID); err != nil {
		t.Fatal(err)
	}
	raw, err := a.configBytes()
	if err != nil || !strings.Contains(string(raw), "work.example") || !strings.Contains(string(raw), "119.29.29.29") {
		t.Fatalf("active overrides missing: %s %v", raw, err)
	}
	if err := a.store.SetActive(second.ID); err != nil {
		t.Fatal(err)
	}
	raw, err = a.configBytes()
	if err != nil || strings.Contains(string(raw), "work.example") {
		t.Fatal("overrides leaked across profiles")
	}
	snapshot, err := a.backupSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.SetOverrides(first.ID, patch.ProfileOverrides{}); err != nil {
		t.Fatal(err)
	}
	if err := a.restoreBackup(snapshot); err != nil {
		t.Fatal(err)
	}
	saved, err := a.store.Get(first.ID)
	if err != nil || saved.Overrides == nil || len(saved.Overrides.ExtraRules) != 1 {
		t.Fatal("backup lost per-profile overrides")
	}
}

func TestProfileOverridesRollbackRejectedKernelAndFailedSave(t *testing.T) {
	a, reloads := newSettingsTestApp(t, func(raw string) bool { return strings.Contains(raw, "rejected.example") })
	p := addFeatureProfile(t, a, "work")
	if _, err := a.commitProfileOverrides(p.ID, patch.ProfileOverrides{ExtraRules: []string{"DOMAIN,rejected.example,DIRECT"}}); err == nil {
		t.Fatal("kernel rejection accepted")
	}
	saved, _ := a.store.Get(p.ID)
	if saved.Overrides != nil {
		t.Fatal("rejected override persisted")
	}
	requests := reloads()
	if len(requests) != 2 || !strings.Contains(requests[0], "rejected.example") || strings.Contains(requests[1], "rejected.example") {
		t.Fatal("old runtime not restored after rejection")
	}
	index := filepath.Join(a.dir, "profiles.json")
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(index, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := a.commitProfileOverrides(p.ID, patch.ProfileOverrides{ExtraRules: []string{"DOMAIN,unsaved.example,DIRECT"}}); err == nil {
		t.Fatal("save failure accepted")
	}
	saved, _ = a.store.Get(p.ID)
	requests = reloads()
	if saved.Overrides != nil || len(requests) != 4 || strings.Contains(requests[3], "unsaved.example") {
		t.Fatal("failed persistence changed runtime or metadata")
	}
}

func TestExtensionScopePreservesIndependentDrafts(t *testing.T) {
	a, _ := newSettingsTestApp(t, nil)
	p := addFeatureProfile(t, a, "work")
	a.initExtensionDrafts()
	a.extraRulesDraft = "DOMAIN,global-draft.example,DIRECT"
	a.extensionScope, a.extensionProfileID = 1, p.ID
	a.initExtensionDrafts()
	if a.extraRulesDraft != "" {
		t.Fatal("global draft reused in profile")
	}
	a.extraRulesDraft = "DOMAIN,local-draft.example,DIRECT"
	a.extensionScope = 0
	a.initExtensionDrafts()
	if !strings.Contains(a.extraRulesDraft, "global-draft") {
		t.Fatal("global draft lost")
	}
	a.extensionScope = 1
	a.initExtensionDrafts()
	if !strings.Contains(a.extraRulesDraft, "local-draft") {
		t.Fatal("profile draft lost")
	}
	if err := a.saveExtraRulesDraft(); err != nil {
		t.Fatal(err)
	}
	if len(a.ov.ExtraRules) != 0 || len(a.profileOverrides(p.ID).ExtraRules) != 1 {
		t.Fatal("save affected wrong scope")
	}
}

func TestConnectionRuleCreatesScopedRuleAndRejectsStaleSubscription(t *testing.T) {
	a, reloads := newSettingsTestApp(t, nil)
	p := addFeatureProfile(t, a, "work")
	cn := core.Conn{ID: "old", Host: "api.example.com", Process: "Browser", DestinationIP: "1.2.3.4"}
	if len(connectionRuleChoices(cn)) != 4 {
		t.Fatal("missing connection match choices")
	}
	a.openConnectionRule(cn)
	if a.connRuleScope != 1 || a.connRuleProfileID != p.ID {
		t.Fatal("not scoped to active subscription")
	}
	a.connRuleTarget = "REJECT"
	a.extensionScope, a.extensionProfileID = 1, p.ID
	a.initExtensionDrafts()
	a.extraRulesDraft = "# keep this draft\nDOMAIN,unsaved.example,DIRECT"
	a.saveConnectionRule()
	if a.showConnRule || a.connRuleError != "" {
		t.Fatal(a.connRuleError)
	}
	if !strings.Contains(a.extraRulesDraft, "# keep this draft") || !strings.Contains(a.extraRulesDraft, "unsaved.example") || a.extraRulesDraft == a.extraRulesSaved {
		t.Fatal("connection action discarded unsaved editor changes")
	}
	local := a.profileOverrides(p.ID)
	if len(local.ExtraRules) != 1 || local.ExtraRules[0] != "DOMAIN,api.example.com,REJECT" || len(a.ov.ExtraRules) != 0 {
		t.Fatal("connection rule saved in wrong scope")
	}
	a.openConnectionRule(cn)
	a.connRuleTarget = "REJECT"
	a.saveConnectionRule()
	if len(a.profileOverrides(p.ID).ExtraRules) != 1 {
		t.Fatal("duplicate rule added")
	}
	if len(reloads()) != 2 {
		t.Fatal("rule did not reload running kernel")
	}
	a.openConnectionRule(cn)
	other := addFeatureProfile(t, a, "personal")
	if err := a.store.SetActive(other.ID); err != nil {
		t.Fatal(err)
	}
	a.saveConnectionRule()
	if !a.showConnRule || a.connRuleError == "" || len(a.profileOverrides(other.ID).ExtraRules) != 0 {
		t.Fatal("stale connection wrote to another subscription")
	}
	ipChoices := connectionRuleChoices(core.Conn{Host: "2001:db8::1"})
	if len(ipChoices) != 1 || ipChoices[0].kind != "IP-CIDR6" || ipChoices[0].value != "2001:db8::1/128" {
		t.Fatal("IPv6 generated invalid rule")
	}
}

func TestExpandedControlsFitMinimumWindow(t *testing.T) {
	a := subscriptionTestApp(t)
	p := addFeatureProfile(t, a, "工作与家庭共用的长名称订阅")
	a.booted, a.ticking = true, true
	a.openProfileOverrides(p)
	for _, tab := range []int{2, 3, 4} {
		a.extensionTab = tab
		tr := ui.NewTester(a.View, 840, 560)
		tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 88})
		tr.Advance(200 * time.Millisecond)
		tr.Frame()
		label := map[int]string{2: "保存额外规则", 3: "保存 YAML 覆写", 4: "保存订阅 DNS"}[tab]
		for _, name := range []string{"覆写作用范围", "预览配置…", label} {
			box, ok := tr.Find(name)
			if !ok || box.X < 184 || box.X+box.W > 824 || box.Y+box.H > 544 {
				t.Fatalf("tab %d %s outside minimum window: %+v (%v)", tab, name, box, ok)
			}
		}
	}
	a.page = "conns"
	a.openConnectionRule(core.Conn{Host: "api.example.com", Process: "Browser", DestinationIP: "1.2.3.4"})
	tr := ui.NewTester(a.View, 840, 560)
	tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 88})
	tr.Frame()
	box, ok := tr.Find("保存分流规则")
	if !ok || box.Y+box.H > 544 {
		t.Fatalf("connection rule footer clipped: %+v", box)
	}
	a.showConnRule = false
	a.page, a.settingsTab = "settings", 6
	a.diagSteps = diagnostic.Steps()
	tr = ui.NewTester(a.View, 840, 560)
	tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 88})
	tr.Frame()
	for _, name := range []string{"诊断", "诊断测试地址", "复制诊断报告", "重新诊断"} {
		box, ok = tr.Find(name)
		if !ok || box.X+box.W > 824 || box.Y+box.H > 544 {
			t.Fatalf("diagnostic control %s clipped: %+v (%v)", name, box, ok)
		}
	}
}

func TestOverviewDNSChangesOnlyActiveSubscriptionAndKeepsServers(t *testing.T) {
	a, _ := newSettingsTestApp(t, nil)
	p := addFeatureProfile(t, a, "work")
	global, local := patch.DefaultDNSSettings(), patch.DefaultDNSSettings()
	global.Nameserver, local.Nameserver = []string{"1.1.1.1"}, []string{"119.29.29.29"}
	a.ov.DNS = &global
	if _, err := a.commitProfileOverrides(p.ID, patch.ProfileOverrides{DNS: &local}); err != nil {
		t.Fatal(err)
	}
	a.toggleNetworkFeature(true, false)
	saved := a.profileOverrides(p.ID)
	if a.notice == "" || saved.DNS == nil || saved.DNS.Enable || saved.DNS.Nameserver[0] != "119.29.29.29" || !a.ov.DNS.Enable {
		t.Fatal("overview toggle changed global DNS or discarded profile servers")
	}
}

func TestCreateConnectionRuleThroughDialog(t *testing.T) {
	a, _ := newSettingsTestApp(t, nil)
	p := addFeatureProfile(t, a, "work")
	a.page, a.booted, a.ticking = "conns", true, true
	a.connSelected = core.Conn{Host: "api.example.com", Process: "Browser"}
	a.showConn = true
	tr := ui.NewTester(a.View, 840, 560)
	tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 88})
	tr.Advance(200 * time.Millisecond)
	tr.Frame()
	if err := tr.Click("创建规则…"); err != nil {
		t.Fatal(err)
	}
	tr.Advance(200 * time.Millisecond)
	tr.Frame()
	if err := tr.Click("连接规则匹配内容"); err != nil {
		t.Fatal(err)
	}
	tr.Command("selectAll")
	tr.Type("edited.example.com")
	tr.Frame()
	if err := tr.Click("保存分流规则"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if a.showConnRule || a.connRuleError != "" {
		t.Fatalf("dialog did not save: %s", a.connRuleError)
	}
	local := a.profileOverrides(p.ID)
	if len(local.ExtraRules) != 1 || local.ExtraRules[0] != "DOMAIN,edited.example.com,DIRECT" {
		t.Fatalf("dialog saved wrong rule: %v", local.ExtraRules)
	}
}

func TestProfileOverridesRetainIsolatedMeshRoutes(t *testing.T) {
	a, reloads := newSettingsTestApp(t, nil)
	p := addFeatureProfile(t, a, "mesh-work")
	f := fixtureMesh(a)
	f.snap.Status.ProxyAddress, f.snap.Status.ProxyPassword = "127.0.0.1:42123", strings.Repeat("a", 64)
	for _, running := range []bool{true, false} {
		f.snap.Status.Running = running
		f.snap.Status.RouteIPs = []netip.Addr{netip.MustParseAddr("100.70.0.2")}
		if _, err := a.commitProfileOverrides(p.ID, patch.ProfileOverrides{ExtraRules: []string{"DOMAIN,work.example,DIRECT"}}); err != nil {
			t.Fatal(err)
		}
		applied := reloads()[len(reloads())-1]
		if !strings.Contains(applied, "__GOPURR_TAILNET__") || !strings.Contains(applied, "100.70.0.2/32") || !strings.Contains(applied, "work.example") {
			t.Fatal("profile apply removed isolated mesh routes")
		}
	}
	snapshot, err := a.backupSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range snapshot.Files {
		if strings.Contains(string(data), strings.Repeat("a", 64)) {
			t.Fatal("runtime mesh credentials leaked into backup")
		}
	}
}
