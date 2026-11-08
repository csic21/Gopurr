package engine

import (
	"testing"

	"gopurr/internal/core"
	"gopurr/internal/profile"

	"github.com/metacubex/mihomo/config"
)

func TestTunnelPlanRejectsStaleProfileAndModeBeforeAttachment(t *testing.T) {
	store, err := profile.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const source = "https://example.invalid/subscription?token=private"
	raw := []byte("rules: ['MATCH,DIRECT']\n")
	item, err := store.AddDownloadedChecked(source, "Source", raw, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{store: store, core: &core.Core{}, port: 17891, mode: "rule"}
	plan, err := e.tunnelPlanLocked(AppScope{Mode: "exclude", Packages: []string{"com.example.app"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.validatePendingPlanLocked("unknown"); asError(err).Code != "stale_plan" {
		t.Fatalf("unknown token accepted: %v", err)
	}
	if _, err := e.validatePendingPlanLocked(plan.Token); err != nil {
		t.Fatal(err)
	}
	plan.Addresses[0] = "modified"
	if e.pendingPlan.plan.Addresses[0] == "modified" {
		t.Fatal("host result could change engine plan")
	}
	item.Name = "Renamed"
	if err := store.Edit(item); err != nil {
		t.Fatal(err)
	}
	if _, err := e.validatePendingPlanLocked(plan.Token); err != nil {
		t.Fatalf("rename changed source revision: %v", err)
	}
	e.mode = "global"
	if _, err := e.validatePendingPlanLocked(plan.Token); asError(err).Code != "stale_plan" {
		t.Fatalf("routing change accepted old plan: %v", err)
	}
	e.mode = "rule"
	if _, err := store.AddDownloadedChecked(source, "Source", append(raw, []byte("# new source\n")...), nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.validatePendingPlanLocked(plan.Token); asError(err).Code != "stale_plan" {
		t.Fatalf("source change accepted old plan: %v", err)
	}
	if _, err := store.AddDownloadedChecked(source, "Source", raw, nil, nil); err != nil {
		t.Fatal(err)
	}
	other, err := store.AddBytesChecked("Other", raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetActive(other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.validatePendingPlanLocked(plan.Token); asError(err).Code != "stale_plan" {
		t.Fatalf("profile change accepted old plan: %v", err)
	}
}

func TestTunnelPlanIsTheSourceOfParsedCoreConfiguration(t *testing.T) {
	// Mihomo otherwise drops IPv6 TUN addresses when the host has no global
	// IPv6 interface. This fixture checks the plan, not the runner's network.
	t.Setenv("SKIP_SYSTEM_IPV6_CHECK", "true")
	scope, err := normalizeScope(AppScope{Mode: "include", Packages: []string{"com.example.browser", "com.example.browser", "org.example.chat"}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := newTunnelPlan(scope)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Version != 1 || len(plan.Token) != 48 || !plan.ExcludeSelf || len(plan.AppScope.Packages) != 2 {
		t.Fatalf("bad versioned plan: %+v", plan)
	}
	managed, err := vpnConfigurationWithPlan([]byte("mode: rule\nrules: ['MATCH,DIRECT']\n"), 37, plan)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := config.Parse(managed)
	if err != nil {
		t.Fatal(err)
	}
	tun := parsed.General.Tun
	if len(tun.Inet4Address) != 1 || len(tun.Inet6Address) != 1 || tun.MTU != uint32(plan.MTU) || tun.Inet4Address[0].String() != plan.Addresses[0] || tun.Inet6Address[0].String() != plan.Addresses[1] || tun.FileDescriptor != 37 || tun.AutoRoute || tun.AutoDetectInterface {
		t.Fatalf("host plan and actual core diverged: %+v / %+v", plan, tun)
	}
	copy := clonePlan(plan)
	copy.AppScope.Packages[0] = "changed"
	copy.Addresses[0] = "changed"
	if plan.AppScope.Packages[0] == "changed" || plan.Addresses[0] == "changed" {
		t.Fatal("public plan aliases engine state")
	}
	for _, bad := range []AppScope{{Mode: "include"}, {Mode: "wrong"}, {Mode: "exclude", Packages: []string{"../other"}}, {Mode: "include", Packages: []string{"com.example\nattack"}}} {
		if _, err := normalizeScope(bad); err == nil {
			t.Fatalf("accepted invalid scope: %+v", bad)
		}
	}
}
