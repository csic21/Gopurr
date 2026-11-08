package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopurr/internal/patch"
	"gopurr/internal/profile"
	"gopurr/internal/update"
)

func nativeDispatchFixture(t *testing.T) {
	t.Helper()
	previous := runOnMain
	runOnMain = func(fn func()) { fn() }
	t.Cleanup(func() { runOnMain = previous })
}
func nativeInvoke(t *testing.T, a *App, method string, params any) any {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	value, err := a.NativeCall(ctx, method, raw)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return value
}
func TestNativeDispatchWaitsAndCancelsBeforeMutation(t *testing.T) {
	previous := runOnMain
	queue := make(chan func(), 1)
	runOnMain = func(fn func()) { queue <- fn }
	defer func() { runOnMain = previous }()
	called := false
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _, err := nativeMain(ctx, func() (any, error) { called = true; return 4, nil }); done <- err }()
	queued := <-queue
	select {
	case <-done:
		t.Fatal("returned before main dispatch")
	default:
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	queued()
	if called {
		t.Fatal("canceled pending mutation executed")
	}
}
func TestNativeSnapshotSeparatesPagesAndRedactsBridgeCredentials(t *testing.T) {
	nativeDispatchFixture(t)
	a, id, _ := proxyEditorTestApp(t)
	f := fixtureMesh(a)
	f.snap.Status.ProxyAddress = "127.0.0.1:12345"
	f.snap.Status.ProxyPassword = "private-bridge-secret"
	a.effectiveFeatures()
	s, err := a.NativeSnapshot(context.Background(), "mesh", "")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(s)
	text := string(body)
	if strings.Contains(text, "private-bridge-secret") || strings.Contains(text, "127.0.0.1:12345") {
		t.Fatal("mesh bridge credentials exposed")
	}
	if _, ok := s["connections"]; ok {
		t.Fatal("mesh snapshot fetched connections")
	}
	if _, ok := s["logs"]; ok {
		t.Fatal("mesh snapshot fetched logs")
	}
	if s["activeProfile"] != id || s["apiVersion"] != float64(2) {
		t.Fatal(s)
	}
	rules, err := a.NativeSnapshot(context.Background(), "rules", id)
	if err != nil {
		t.Fatal(err)
	}
	scope := rules["scope"].(map[string]any)
	if scope["id"] != id {
		t.Fatal(scope)
	}
	for _, key := range []string{"scope", "library", "appRules", "effectiveRules", "groups"} {
		if _, ok := rules[key]; !ok {
			t.Fatalf("missing %s", key)
		}
	}
	editor := nativeInvoke(t, a, "desktop.proxies.editor", map[string]any{"id": id}).(map[string]any)
	nodes := editor["nodes"].([]any)
	node := nodes[0].(map[string]any)
	if node["source"] != "One" || node["base"].(map[string]any)["name"] != "One" {
		t.Fatal(node)
	}
}

func TestNativeMeshSnapshotEncodesDeviceAddressesAsStrings(t *testing.T) {
	nativeDispatchFixture(t)
	a, _, _ := proxyEditorTestApp(t)
	f := fixtureMesh(a)
	f.snap.Status.Self.IPs = []netip.Addr{netip.MustParseAddr("100.70.0.1"), netip.MustParseAddr("fd7a:115c:a1e0::1")}
	f.snap.Status.Peers[0].IPs = []netip.Addr{netip.MustParseAddr("100.70.0.2")}
	snapshot, err := a.NativeSnapshot(context.Background(), "mesh", "")
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(snapshot["mesh"])
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Status struct {
			Self  struct{ IPs []string }
			Peers []struct{ IPs []string }
		}
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatalf("mesh addresses cannot be read by the UI: %v", err)
	}
	if !reflect.DeepEqual(wire.Status.Self.IPs, []string{"100.70.0.1", "fd7a:115c:a1e0::1"}) || !reflect.DeepEqual(wire.Status.Peers[0].IPs, []string{"100.70.0.2"}) {
		t.Fatal("mesh address text was lost", wire)
	}
}
func TestNativeRulesTransactionRollsBackRejectedRuntime(t *testing.T) {
	nativeDispatchFixture(t)
	a, reloads := newSettingsTestApp(t, func(raw string) bool { return strings.Contains(raw, "rejected.example") })
	before := a.ov
	raw := json.RawMessage(`{"extraRules":["DOMAIN,rejected.example,DIRECT"]}`)
	if _, err := a.NativeCall(context.Background(), "desktop.rules.save", raw); err == nil {
		t.Fatal("accepted rejected runtime")
	}
	if !reflect.DeepEqual(a.ov, before) || busyNetwork(a) {
		t.Fatal("rejected transaction published or stayed busy")
	}
	if len(reloads()) < 2 {
		t.Fatal("previous runtime was not restored")
	}
	nativeInvoke(t, a, "desktop.rules.save", map[string]any{"extraRules": []string{"DOMAIN,accepted.example,DIRECT"}})
	if len(a.ov.ExtraRules) != 1 || !strings.Contains(a.ov.ExtraRules[0], "accepted.example") {
		t.Fatal(a.ov)
	}
	stored, _ := os.ReadFile(a.ovPath)
	if strings.Contains(string(stored), "rejected.example") {
		t.Fatal("rejected rules persisted")
	}
}
func TestNativeScopedOverridesAndPoliciesPreserveOtherFields(t *testing.T) {
	nativeDispatchFixture(t)
	a, id, _ := proxyEditorTestApp(t)
	nativeInvoke(t, a, "desktop.rules.save", map[string]any{"id": id, "extraRules": []string{"DOMAIN,local.example,DIRECT"}})
	if len(a.ov.ExtraRules) != 0 || len(a.profileOverrides(id).ExtraRules) != 1 {
		t.Fatal("scope escaped into global settings")
	}
	nativeInvoke(t, a, "desktop.features.save", map[string]any{"id": id, "dnsPolicy": false})
	if p := a.profileOverrides(id).DNSEnable; p == nil || *p {
		t.Fatal("scoped policy missing")
	}
	dns := patch.DefaultDNSSettings()
	nativeInvoke(t, a, "desktop.features.save", map[string]any{"id": id, "dns": dns})
	if a.profileOverrides(id).DNSEnable != nil {
		t.Fatal("custom DNS retained separate enable override")
	}
	nativeInvoke(t, a, "desktop.features.save", map[string]any{"id": id, "dns": nil})
	if a.profileOverrides(id).DNS != nil || len(a.profileOverrides(id).ExtraRules) != 1 {
		t.Fatal("restore-follow erased another field")
	}
}
func TestNativeProxyEditsSurviveRefreshAndPreserveOptionsKeys(t *testing.T) {
	nativeDispatchFixture(t)
	a, id, path := proxyEditorTestApp(t)
	nativeInvoke(t, a, "desktop.proxies.save", map[string]any{"id": id, "edits": map[string]any{"nodes": []any{map[string]any{"source": "One", "name": "Local One", "options": map[string]any{"ws-opts": map[string]any{"headers": map[string]any{"Host": "Case-Sensitive.example"}}}}}}})
	next := strings.Replace(proxyEditorSource, "initial-secret", "fresh-secret", 1)
	if err := os.WriteFile(path, []byte(next), 0600); err != nil {
		t.Fatal(err)
	}
	nativeInvoke(t, a, "desktop.profiles.update", map[string]any{"id": id})
	raw, err := a.configForProfile(id, a.profileOverrides(id))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"fresh-secret", "Local One", "Host: Case-Sensitive.example"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %s in %s", want, raw)
		}
	}
	editor := nativeInvoke(t, a, "desktop.proxies.editor", map[string]any{"id": id}).(map[string]any)
	first := editor["nodes"].([]any)[0].(map[string]any)
	if first["source"] != "One" || first["name"] != "Local One" {
		t.Fatal(first)
	}
}
func TestNativeBackupRestoreAndPartialPrefs(t *testing.T) {
	nativeDispatchFixture(t)
	a, _ := newSettingsTestApp(t, nil)
	a.NativeSetAutoLaunchHandler(func(bool) error { return errors.New("fixture: never install a login item") })
	nativeInvoke(t, a, "desktop.prefs.save", map[string]any{"prefs": map[string]any{"appearance": "dark"}})
	path := filepath.Join(a.dir, "native-backup.zip")
	nativeInvoke(t, a, "desktop.backup.export", map[string]any{"path": path})
	inspect := nativeInvoke(t, a, "desktop.backup.inspect", map[string]any{"path": path}).(map[string]any)
	if inspect["count"] != 0 {
		t.Fatal(inspect)
	}
	nativeInvoke(t, a, "desktop.prefs.save", map[string]any{"appearance": "light"})
	nativeInvoke(t, a, "desktop.rules.save", map[string]any{"extraRules": []string{"DOMAIN,temporary.example,DIRECT"}})
	nativeInvoke(t, a, "desktop.backup.restore", map[string]any{"path": path})
	if a.Prefs().Appearance != "dark" || len(a.ov.ExtraRules) != 0 {
		t.Fatal("restore did not publish backup")
	}
	before := a.Prefs()
	if _, err := a.NativeCall(context.Background(), "desktop.prefs.save", json.RawMessage(`{"autoLaunch":true}`)); err == nil {
		t.Fatal("autostart failure ignored")
	}
	if a.Prefs() != before {
		t.Fatal("failed preference save published")
	}
}
func TestNativeMeshUsesRealActionsAndRefusesEmptyGrant(t *testing.T) {
	nativeDispatchFixture(t)
	a, id, _ := proxyEditorTestApp(t)
	_ = id
	f := fixtureMesh(a)
	nativeInvoke(t, a, "desktop.mesh.action", map[string]any{"action": "disconnect"})
	nativeInvoke(t, a, "desktop.mesh.autoConnect", map[string]any{"enabled": true})
	if !reflect.DeepEqual(f.calls, []string{"disconnect", "auto"}) || !f.snap.AutoConnect {
		t.Fatal(f.calls)
	}
	f.snap.Status.Services = nil
	if _, err := a.NativeCall(context.Background(), "desktop.mesh.action", json.RawMessage(`{"action":"allow"}`)); err == nil {
		t.Fatal("empty access grant accepted")
	}
	if len(f.calls) != 2 {
		t.Fatal("empty grant changed access")
	}
	nativeInvoke(t, a, "desktop.mesh.services", map[string]any{"protocol": "22/tcp", "enabled": true})
	if !reflect.DeepEqual(f.calls, []string{"disconnect", "auto", "allow"}) {
		t.Fatal(f.calls)
	}
}

func TestNativeProfilesImportAggregateSwitchEditAndDelete(t *testing.T) {
	nativeDispatchFixture(t)
	a, _ := newSettingsTestApp(t, nil)
	first := nativeInvoke(t, a, "desktop.profiles.import", map[string]any{"name": "Native source one", "content": proxyEditorSource}).(profile.Profile)
	second := nativeInvoke(t, a, "desktop.profiles.import", map[string]any{"name": "Native source two", "content": strings.Replace(proxyEditorSource, "one.example.com", "second.example.com", 1)}).(profile.Profile)
	if a.store.ActiveID() != first.ID || len(a.store.List()) != 2 {
		t.Fatal("import changed activation unexpectedly")
	}
	spec := profile.AggregateSpec{Sources: []profile.AggregateSource{{ID: first.ID, Prefix: "One "}, {ID: second.ID, Prefix: "Two "}}, Dedupe: false}
	merged := nativeInvoke(t, a, "desktop.profiles.aggregate", map[string]any{"name": "Native combined", "spec": spec}).(map[string]any)["profile"].(profile.Profile)
	nativeInvoke(t, a, "desktop.profiles.activate", map[string]any{"id": merged.ID})
	if a.store.ActiveID() != merged.ID {
		t.Fatal("aggregate not activated")
	}
	payload, _ := json.Marshal(map[string]any{"id": merged.ID})
	if _, err := a.NativeCall(context.Background(), "desktop.profiles.delete", payload); err == nil {
		t.Fatal("active profile deleted")
	}
	nativeInvoke(t, a, "desktop.profiles.activate", map[string]any{"id": first.ID})
	first.Name = "Renamed native source"
	nativeInvoke(t, a, "desktop.profiles.edit", map[string]any{"profile": first})
	renamed, err := a.store.Get(first.ID)
	if err != nil || renamed.Name != first.Name {
		t.Fatal(renamed, err)
	}
	nativeInvoke(t, a, "desktop.profiles.delete", map[string]any{"id": merged.ID})
	nativeInvoke(t, a, "desktop.profiles.delete", map[string]any{"id": second.ID})
	if len(a.store.List()) != 1 {
		t.Fatal("profile deletion did not commit")
	}
}
func TestNativeNetworkRejectsCandidateAndPreservesPublishedSettings(t *testing.T) {
	nativeDispatchFixture(t)
	a, _ := newSettingsTestApp(t, func(raw string) bool { return strings.Contains(raw, "allow-lan: true") })
	before := a.ov
	if _, err := a.NativeCall(context.Background(), "desktop.network.save", json.RawMessage(`{"allowLan":true}`)); err == nil {
		t.Fatal("rejected network accepted")
	}
	if !reflect.DeepEqual(a.ov, before) || busyNetwork(a) {
		t.Fatal("failed network save published or retained busy gate")
	}
	nativeInvoke(t, a, "desktop.network.save", map[string]any{"unified": false})
	if a.ov.Unified || a.ov.MixedPort != before.MixedPort {
		t.Fatal(a.ov)
	}
}
func TestNativeImportLinkStagesOnlyValidatedPayload(t *testing.T) {
	a := &App{}
	payload, err := a.NativeImportURL("gopurr://install-config?url=https%3A%2F%2Fexample.com%2Fsub%3Ftoken%3Dprivate&name=Example")
	if err != nil {
		t.Fatal(err)
	}
	if payload["name"] != "Example" || a.page != "subs" || a.showAddSub || a.subURL != "" {
		t.Fatal("legacy dialog or download state was touched")
	}
	if _, err := a.NativeImportURL("gopurr://install-config?url=file%3A%2F%2F%2Fetc%2Fpasswd"); err == nil {
		t.Fatal("invalid source accepted")
	}
}

func TestNativeLocalConfigUsesWorkerAndPreservesAcceptedRuntimeOnFailure(t *testing.T) {
	nativeDispatchFixture(t)
	a, reloads := newSettingsTestApp(t, func(raw string) bool { return strings.Contains(raw, "refused.example") })
	next := strings.Replace(demoConfig, "DOMAIN-SUFFIX,local,DIRECT", "DOMAIN,local.example,DIRECT", 1)
	nativeInvoke(t, a, "desktop.runtime.local.save", map[string]any{"yaml": next})
	if a.cfgText != next || busyNetwork(a) || len(reloads()) == 0 {
		t.Fatal("local YAML was not accepted and published")
	}
	body, err := os.ReadFile(a.cfgPath)
	if err != nil || string(body) != next {
		t.Fatal("local source was not saved", err)
	}
	raw, _ := json.Marshal(map[string]any{"yaml": strings.Replace(next, "local.example", "refused.example", 1)})
	if _, err := a.NativeCall(context.Background(), "desktop.runtime.local.save", raw); err == nil {
		t.Fatal("invalid local runtime accepted")
	}
	body, _ = os.ReadFile(a.cfgPath)
	if a.cfgText != next || string(body) != next || busyNetwork(a) {
		t.Fatal("failed local save changed accepted config")
	}
}

func TestNativeDispatchDoesNotDropCommittedReservationAtDeadline(t *testing.T) {
	previous := runOnMain
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runOnMain = func(fn func()) { fn(); cancel() }
	defer func() { runOnMain = previous }()
	value, err := nativeMain(ctx, func() (any, error) { return "reserved", nil })
	if err != nil || value != "reserved" {
		t.Fatal("committed main-thread result lost to deadline", value, err)
	}
}
func TestNativeMalformedNetworkSwitchDoesNotTouchOS(t *testing.T) {
	a := &App{}
	for _, raw := range []json.RawMessage{json.RawMessage(`{}`), json.RawMessage(`{"enabled":null}`), json.RawMessage(`{"enabled":"false"}`)} {
		if _, err := a.NativeCall(context.Background(), "desktop.systemProxy.set", raw); err == nil {
			t.Fatal("malformed network mutation accepted")
		}
	}
}

func TestNativeUpdateVisibilityIsSharedAcrossPages(t *testing.T) {
	nativeDispatchFixture(t)
	a, _, _ := proxyEditorTestApp(t)
	fixtureSoftwareUpdate(t, a, update.Current)
	snapshot, err := a.NativeSnapshot(context.Background(), "overview", "")
	if err != nil {
		t.Fatal(err)
	}
	state, ok := snapshot["update"].(map[string]any)
	if !ok || state["visible"] != true || state["phase"] != string(update.Current) {
		t.Fatal("update state missing outside settings", snapshot["update"])
	}
	nativeInvoke(t, a, "desktop.update.hide", map[string]any{})
	if a.softwareUpdates.Snapshot().Visible {
		t.Fatal("update dialog did not hide")
	}
}
