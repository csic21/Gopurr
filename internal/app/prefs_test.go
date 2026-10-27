package app

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"gopurr/internal/patch"
	"gopurr/internal/sysproxy"
)

func TestPrefsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPURR_HOME", dir)

	a := New()
	if a.Prefs().LightMode {
		t.Fatal("light mode should default to off")
	}
	if d := a.Prefs().LightDelay; d != 60 {
		t.Fatalf("default delay = %d", d)
	}

	p := a.Prefs()
	p.LightMode, p.LightDelay = true, 30
	p.RestoreSystemProxy = true
	p.TestURL = "https://example.com/check"
	p.TestTimeoutMS, p.TestConcurrency = 10000, 4
	if err := a.UpdatePrefs(p); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(filepath.Join(dir, "prefs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Fatal("prefs not written")
	}

	again := New()
	if !again.Prefs().LightMode || again.Prefs().LightDelay != 30 {
		t.Fatalf("reload = %+v", again.Prefs())
	}
	if again.lightDelayIdx != 0 {
		t.Fatalf("delay index = %d", again.lightDelayIdx)
	}
	if got := again.Prefs(); got != p {
		t.Fatalf("round trip = %+v, want %+v", got, p)
	}
}

func TestPrefsClampsDelay(t *testing.T) {
	t.Setenv("GOPURR_HOME", t.TempDir())
	a := New()
	if err := a.UpdatePrefs(Prefs{LightMode: true, LightDelay: 0}); err != nil {
		t.Fatal(err)
	}
	if got := a.Prefs().LightDelay; got != 60 {
		t.Fatalf("zero delay = %d, want the default", got)
	}
}

func TestPrefsHookFires(t *testing.T) {
	t.Setenv("GOPURR_HOME", t.TempDir())
	a := New()

	var got Prefs
	a.SetPrefsHook(func(p Prefs) { got = p })
	if err := a.UpdatePrefs(Prefs{LightMode: true, LightDelay: 300}); err != nil {
		t.Fatal(err)
	}
	if !got.LightMode || got.LightDelay != 300 {
		t.Fatalf("hook saw %+v", got)
	}
}

func TestPrefsDefaults(t *testing.T) {
	a := &App{}
	a.loadPrefs()
	p := a.Prefs()
	if p.LightMode || p.RestoreSystemProxy || p.AutoUpdateHours != 0 {
		t.Fatalf("opt-in preferences enabled by default: %+v", p)
	}
	if p.LightDelay != 60 || p.TestURL != "https://www.gstatic.com/generate_204" || p.TestTimeoutMS != 6000 || p.TestConcurrency != 8 {
		t.Fatalf("unexpected defaults: %+v", p)
	}
}

func TestPrefsMigrationMergesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := prefsPathFor(dir)
	legacy := `{"lightMode":true,"lightDelaySeconds":300,"autoUpdateHours":12,"lastAutoUpdate":"2026-10-26T12:30:00+08:00"}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &App{prefsPath: path}
	a.loadPrefs()
	p := a.Prefs()
	if !p.LightMode || p.LightDelay != 300 || p.AutoUpdateHours != 12 || p.LastAutoUpdate.IsZero() {
		t.Fatalf("legacy preferences lost: %+v", p)
	}
	if p.RestoreSystemProxy || p.TestURL != defaultPrefs().TestURL || p.TestTimeoutMS != 6000 || p.TestConcurrency != 8 {
		t.Fatalf("new defaults missing after migration: %+v", p)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != legacy {
		t.Fatalf("loading rewrote the original file: %q, %v", b, err)
	}
}

func TestPrefsLoadRepairsOnlyInvalidFields(t *testing.T) {
	cases := []struct {
		name string
		body string
		want Prefs
	}{
		{"empty", `{}`, defaultPrefs()},
		{"malformed", `{"lightMode":true,`, defaultPrefs()},
		{"invalid-new-fields", `{"lightMode":true,"lightDelaySeconds":30,"autoUpdateHours":24,"testURL":"file:///tmp/test","testTimeoutMS":-1,"testConcurrency":100}`, func() Prefs {
			p := defaultPrefs()
			p.LightMode, p.LightDelay, p.AutoUpdateHours = true, 30, 24
			return p
		}()},
		{"valid-custom-values", `{"restoreSystemProxy":true,"testURL":" https://example.com/204?q=1 ","testTimeoutMS":7500,"testConcurrency":3}`, func() Prefs {
			p := defaultPrefs()
			p.RestoreSystemProxy = true
			p.TestURL, p.TestTimeoutMS, p.TestConcurrency = "https://example.com/204?q=1", 7500, 3
			return p
		}()},
		{"invalid-old-fields", `{"lightDelaySeconds":-10,"autoUpdateHours":-1,"testTimeoutMS":15000}`, func() Prefs {
			p := defaultPrefs()
			p.TestTimeoutMS = 15000
			return p
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := prefsPathFor(t.TempDir())
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			a := &App{prefsPath: path}
			a.loadPrefs()
			if got := a.Prefs(); got != tc.want {
				t.Fatalf("load = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestPrefsTestSettingsValidation(t *testing.T) {
	cases := []struct {
		name        string
		url         string
		timeout     int
		concurrency int
		valid       bool
	}{
		{"default", defaultPrefs().TestURL, 6000, 8, true},
		{"http", "http://127.0.0.1:8080/204", 1000, 1, true},
		{"upper-limits", "https://example.com/204?key=value", 30000, 32, true},
		{"empty", "", 6000, 8, false},
		{"relative", "/generate_204", 6000, 8, false},
		{"missing-scheme", "www.example.com", 6000, 8, false},
		{"missing-host", "https:///204", 6000, 8, false},
		{"unsupported-scheme", "file:///tmp/test", 6000, 8, false},
		{"credentials", "https://user:pass@example.com/204", 6000, 8, false},
		{"fragment", "https://example.com/204#ignored", 6000, 8, false},
		{"whitespace", "https://example.com/a b", 6000, 8, false},
		{"invalid-escape", "https://example.com/%zz", 6000, 8, false},
		{"short-timeout", "https://example.com", 999, 8, false},
		{"long-timeout", "https://example.com", 30001, 8, false},
		{"zero-workers", "https://example.com", 6000, 0, false},
		{"too-many-workers", "https://example.com", 6000, 33, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTestSettings(tc.url, tc.timeout, tc.concurrency)
			if (err == nil) != tc.valid {
				t.Fatalf("validation error = %v, valid = %v", err, tc.valid)
			}
		})
	}
}

func TestPrefsInvalidUpdateDoesNotPublish(t *testing.T) {
	a := &App{prefs: defaultPrefs(), prefsPath: prefsPathFor(t.TempDir())}
	if err := a.UpdatePrefs(a.Prefs()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(a.prefsPath)
	if err != nil {
		t.Fatal(err)
	}
	called := 0
	a.SetPrefsHook(func(Prefs) { called++ })
	called = 0
	p := a.Prefs()
	p.TestURL = "javascript:invalid"
	if err := a.UpdatePrefs(p); err == nil {
		t.Fatal("invalid update succeeded")
	}
	after, err := os.ReadFile(a.prefsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := a.Prefs(); got != defaultPrefs() || called != 0 || string(before) != string(after) {
		t.Fatalf("invalid preferences were published: prefs=%+v hooks=%d", got, called)
	}
}

func TestPrefsSaveFailureDoesNotPublish(t *testing.T) {
	// Renaming a regular file onto a directory fails on all supported systems,
	// including when tests run as root and chmod would not prevent a write.
	a := &App{prefs: defaultPrefs(), prefsPath: t.TempDir()}
	called := 0
	a.SetPrefsHook(func(Prefs) { called++ })
	called = 0
	p := a.Prefs()
	p.LightMode = true
	if err := a.UpdatePrefs(p); err == nil {
		t.Fatal("write to directory succeeded")
	}
	if a.Prefs() != defaultPrefs() || called != 0 {
		t.Fatalf("failed write was published: prefs=%+v hooks=%d", a.Prefs(), called)
	}
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(a.prefsPath), ".settings-*.json"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary files left behind: %v, %v", leftovers, err)
	}
}

func TestPrefsReconcileCombinesChanges(t *testing.T) {
	a := &App{prefs: defaultPrefs(), prefsPath: prefsPathFor(t.TempDir()), ov: patch.Default()}
	a.initSettings()
	called := 0
	a.SetPrefsHook(func(Prefs) { called++ })
	called = 0
	a.lightOn, a.lightDelayIdx, a.autoIdx, a.restoreProxyOn = true, 2, 1, true
	before := time.Now()
	a.reconcileSettings()
	p := a.Prefs()
	if !p.LightMode || p.LightDelay != 300 || p.AutoUpdateHours != 6 || !p.RestoreSystemProxy || p.LastAutoUpdate.Before(before) {
		t.Fatalf("simultaneous edits lost: %+v", p)
	}
	if called != 1 {
		t.Fatalf("one frame saved %d times", called)
	}
	a.reconcileSettings()
	if called != 1 {
		t.Fatalf("unchanged settings saved again: %d", called)
	}
	reloaded := &App{prefsPath: a.prefsPath}
	reloaded.loadPrefs()
	if got := reloaded.Prefs(); !got.LastAutoUpdate.Equal(p.LastAutoUpdate) || got.LightMode != p.LightMode || got.LightDelay != p.LightDelay || got.AutoUpdateHours != p.AutoUpdateHours || got.RestoreSystemProxy != p.RestoreSystemProxy {
		t.Fatalf("saved edits lost on reload: %+v", got)
	}
}

func TestPrefsReconcilePreservesBackgroundUpdates(t *testing.T) {
	p := defaultPrefs()
	p.AutoUpdateHours, p.LightDelay = 1, 120 // valid custom values not in UI choices
	a := &App{prefs: p, ov: patch.Default()}
	a.initSettings()
	updated := time.Now().Add(-time.Hour).Round(time.Second)
	if err := a.changePrefs(func(p *Prefs) {
		p.LastAutoUpdate = updated
		p.TestURL = "https://example.com/check"
		p.TestConcurrency = 3
	}); err != nil {
		t.Fatal(err)
	}
	a.lightOn, a.restoreProxyOn = true, true
	a.reconcileSettings()
	got := a.Prefs()
	if !got.LastAutoUpdate.Equal(updated) || got.TestURL != "https://example.com/check" || got.TestConcurrency != 3 || got.AutoUpdateHours != 1 || got.LightDelay != 120 || !got.LightMode || !got.RestoreSystemProxy {
		t.Fatalf("UI overwrote background/custom values: %+v", got)
	}
}

func TestPrefsReconcileShowsSaveError(t *testing.T) {
	a := &App{prefs: defaultPrefs(), prefsPath: t.TempDir(), ov: patch.Default()}
	a.initSettings()
	a.lightOn, a.restoreProxyOn = true, true
	a.reconcileSettings()
	if a.generalSettingsError == "" || a.notice == "" || a.lightOn || a.restoreProxyOn || a.Prefs() != defaultPrefs() {
		t.Fatalf("failed save not reflected: error=%q prefs=%+v", a.generalSettingsError, a.Prefs())
	}
}

func TestPrefsMeasurementFormValidatesAndPreservesPreferences(t *testing.T) {
	p := defaultPrefs()
	p.LightMode, p.AutoUpdateHours, p.RestoreSystemProxy = true, 12, true
	a := &App{prefs: p, prefsPath: prefsPathFor(t.TempDir()), ov: patch.Default()}
	a.initSettings()
	a.testURLDraft = ""
	a.saveTestSettings()
	if a.testSettingsError == "" || a.Prefs() != p {
		t.Fatal("empty form URL was silently replaced with default")
	}
	a.testURLDraft, a.testTimeoutPick, a.testConcurrencyPick = " https://example.com/204 ", "15000", "4"
	a.saveTestSettings()
	got := a.Prefs()
	if a.testSettingsError != "" || got.TestURL != "https://example.com/204" || got.TestTimeoutMS != 15000 || got.TestConcurrency != 4 || !got.LightMode || got.AutoUpdateHours != 12 || !got.RestoreSystemProxy {
		t.Fatalf("measurement save lost settings: prefs=%+v error=%q", got, a.testSettingsError)
	}
	a.prefsPath = t.TempDir()
	a.testConcurrencyPick = "16"
	a.saveTestSettings()
	if a.testSettingsError == "" || a.Prefs() != got || !a.testSettingsDirty() {
		t.Fatal("measurement write failure did not preserve preferences and draft")
	}
}

func TestPrefsNetworkPortValidation(t *testing.T) {
	for _, raw := range []string{"", "0", "65536", "-1", "+7890", "78.90", "abc", "７８９０"} {
		if _, err := parseSettingsPort(raw); err == nil {
			t.Errorf("accepted invalid port %q", raw)
		}
	}
	for _, raw := range []string{"1", "65535", " 7890 "} {
		if _, err := parseSettingsPort(raw); err != nil {
			t.Errorf("rejected valid port %q: %v", raw, err)
		}
	}
}

func TestPrefsNetworkSaveValidatesAndPreservesOverrides(t *testing.T) {
	a := &App{prefs: defaultPrefs(), cfgText: demoConfig, ov: patch.Default(), ovPath: filepath.Join(t.TempDir(), "overrides.json")}
	a.initSettings()
	a.mixedPortDraft = "invalid"
	if a.saveNetworkSettings() || a.networkError == "" || !reflect.DeepEqual(a.ov, patch.Default()) {
		t.Fatal("invalid port changed overrides")
	}
	a.mixedPortDraft = "7890"
	a.lanOn, a.ipv6On, a.unifiedOn, a.tunStackPick = true, true, false, "system"
	// These are owned by the rest of the application and can change after the
	// page initializes. Saving network settings must preserve the live values.
	a.ov.Mode, a.ov.TunEnable = "global", true
	if !a.saveNetworkSettings() {
		t.Fatal(a.networkError)
	}
	var saved patch.Overrides
	b, err := os.ReadFile(a.ovPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if !saved.AllowLAN || !saved.IPv6 || saved.Unified || saved.TunStack != "system" || saved.Mode != "global" || !saved.TunEnable {
		t.Fatalf("network save lost values: %+v", saved)
	}
	a.ovPath = t.TempDir()
	a.lanOn = false
	if a.saveNetworkSettings() || a.networkError == "" || !reflect.DeepEqual(a.ov, saved) || !a.networkSettingsDirty() {
		t.Fatal("network write failure changed overrides or discarded draft")
	}
}

func TestPrefsNetworkRejectsOccupiedPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	a := &App{prefs: defaultPrefs(), ov: patch.Default()}
	a.initSettings()
	a.mixedPortDraft = strings.TrimPrefix(ln.Addr().String(), "127.0.0.1:")
	if a.saveNetworkSettings() || !strings.Contains(a.networkError, "已被占用") || !reflect.DeepEqual(a.ov, patch.Default()) {
		t.Fatalf("occupied port accepted: %q", a.networkError)
	}
}

func TestPrefsNetworkRejectsInvalidConfigBeforeSaving(t *testing.T) {
	a := &App{prefs: defaultPrefs(), cfgText: "rules: [broken", ov: patch.Default(), ovPath: filepath.Join(t.TempDir(), "overrides.json")}
	if err := writeSettingsFile(a.ovPath, a.ov); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(a.ovPath)
	if err != nil {
		t.Fatal(err)
	}
	a.initSettings()
	a.lanOn = true
	if a.saveNetworkSettings() || !strings.Contains(a.networkError, "校验失败") || !reflect.DeepEqual(a.ov, patch.Default()) || !a.networkSettingsDirty() {
		t.Fatalf("invalid candidate not rolled back: error=%q overrides=%+v", a.networkError, a.ov)
	}
	after, err := os.ReadFile(a.ovPath)
	if err != nil || string(before) != string(after) {
		t.Fatalf("invalid config persisted: %q, %v", after, err)
	}
}

func TestPrefsNetworkRollsBackRejectedReload(t *testing.T) {
	a, reloads := newSettingsTestApp(t, func(raw string) bool { return strings.Contains(raw, "allow-lan: true") })
	before, err := os.ReadFile(a.ovPath)
	if err != nil {
		t.Fatal(err)
	}
	a.lanOn = true
	if a.saveNetworkSettings() || !reflect.DeepEqual(a.ov, patch.Default()) || !a.networkSettingsDirty() {
		t.Fatalf("rejected reload not rolled back: error=%q overrides=%+v", a.networkError, a.ov)
	}
	if !strings.Contains(a.networkError, "重载失败") || !strings.Contains(a.networkError, "test rejection") {
		t.Fatalf("missing reload error: %q", a.networkError)
	}
	after, err := os.ReadFile(a.ovPath)
	if err != nil || string(before) != string(after) {
		t.Fatalf("rejected config persisted: %q, %v", after, err)
	}
	requests := reloads()
	if len(requests) != 2 || !strings.Contains(requests[0], "allow-lan: true") || !strings.Contains(requests[1], "allow-lan: false") {
		t.Fatalf("runtime was not restored: %q", requests)
	}
	raw, err := os.ReadFile(a.mgr.ConfigPath())
	if err != nil || strings.Contains(string(raw), "allow-lan: true") || !a.k.Running() {
		t.Fatalf("working runtime lost: %q, %v", raw, err)
	}
}

func TestPrefsNetworkRollsBackAfterSaveFailure(t *testing.T) {
	a, reloads := newSettingsTestApp(t, nil)
	previousPath := a.ovPath
	before, err := os.ReadFile(previousPath)
	if err != nil {
		t.Fatal(err)
	}
	a.ovPath = t.TempDir() // fail persistence after the candidate was applied
	a.lanOn = true
	if a.saveNetworkSettings() || !reflect.DeepEqual(a.ov, patch.Default()) || !a.networkSettingsDirty() {
		t.Fatalf("write failure not rolled back: error=%q overrides=%+v", a.networkError, a.ov)
	}
	requests := reloads()
	if !strings.Contains(a.networkError, "保存失败") || len(requests) != 2 || !strings.Contains(requests[0], "allow-lan: true") || !strings.Contains(requests[1], "allow-lan: false") {
		t.Fatalf("write failure did not restore runtime: error=%q requests=%q", a.networkError, requests)
	}
	after, err := os.ReadFile(previousPath)
	if err != nil || string(before) != string(after) || !a.k.Running() {
		t.Fatalf("previous saved config or kernel lost: %q, %v", after, err)
	}
}

func TestPrefsNetworkReportsRollbackFailure(t *testing.T) {
	a, _ := newSettingsTestApp(t, func(string) bool { return true })
	a.lanOn = true
	if a.saveNetworkSettings() || !reflect.DeepEqual(a.ov, patch.Default()) || !strings.Contains(a.networkError, "恢复原内核配置失败") {
		t.Fatalf("rollback error hidden: %q", a.networkError)
	}
}

func TestPrefsNetworkPortChangeResyncsProxyWithoutReplacingPreviousOwner(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS system proxy integration")
	}
	a, _ := newSettingsTestApp(t, nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	if port == a.mixedPort() {
		t.Skip("ephemeral port happened to equal the existing port")
	}
	logPath := fakeSettingsSystemProxy(t, a, port, false)
	previous := &sysproxy.State{Enabled: true, Server: "previous.example", Port: 8080}
	a.prev, a.sysApplied, a.sysOn = previous, true, true
	a.savePrevProxy()
	previousFile, err := os.ReadFile(a.prevPath)
	if err != nil {
		t.Fatal(err)
	}
	a.mixedPortDraft = strconv.Itoa(port)
	if !a.saveNetworkSettings() {
		t.Fatal(a.networkError)
	}
	call, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(call), "-setwebproxy") || !strings.Contains(string(call), "-setsocksfirewallproxy") || !strings.Contains(string(call), strconv.Itoa(port)) {
		t.Fatalf("proxy was not repointed: %q, %v", call, err)
	}
	after, err := os.ReadFile(a.prevPath)
	if err != nil || string(after) != string(previousFile) || a.prev != previous || !a.sysApplied || a.sysState.Port != port {
		t.Fatalf("previous proxy owner overwritten: prev=%+v state=%+v err=%v", a.prev, a.sysState, err)
	}
}

func TestPrefsNetworkRollsBackWhenProxyAuthorizationIsCanceled(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS system proxy integration")
	}
	a, reloads := newSettingsTestApp(t, nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	if port == a.mixedPort() {
		t.Skip("ephemeral port happened to equal the existing port")
	}
	logPath := fakeSettingsSystemProxy(t, a, port, true)
	previous := &sysproxy.State{Enabled: true, Server: "previous.example", Port: 8080}
	a.prev, a.sysApplied, a.sysOn = previous, true, true
	a.mixedPortDraft = strconv.Itoa(port)
	if a.saveNetworkSettings() || !strings.Contains(a.networkError, "已取消授权") || !reflect.DeepEqual(a.ov, patch.Default()) || !a.networkSettingsDirty() {
		t.Fatalf("canceled authorization did not roll back network: error=%q overrides=%+v", a.networkError, a.ov)
	}
	var saved patch.Overrides
	b, err := os.ReadFile(a.ovPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &saved); err != nil || !reflect.DeepEqual(saved, patch.Default()) {
		t.Fatalf("candidate remained on disk: %s, %v", b, err)
	}
	requests := reloads()
	if len(requests) != 2 || patch.MixedPort([]byte(requests[0])) != port || patch.MixedPort([]byte(requests[1])) != 7890 {
		t.Fatalf("kernel listener was not restored: %q", requests)
	}
	call, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(call), " 7890;") || a.prev != previous {
		t.Fatalf("original system proxy was not restored: call=%q prev=%+v err=%v", call, a.prev, err)
	}
}

func fakeSettingsSystemProxy(t *testing.T, a *App, port int, cancelFirst bool) string {
	t.Helper()
	a.proxy = scriptedProxyFixture{}
	binDir := t.TempDir()
	logPath := filepath.Join(binDir, "proxy-call.txt")
	scripts := map[string]string{
		"route":        "#!/bin/sh\nprintf 'interface: en0\\n'\n",
		"networksetup": "#!/bin/sh\ncase \"$1\" in\n-listnetworkserviceorder) printf '(1) Wi-Fi\\n(Hardware Port: Wi-Fi, Device: en0)\\n';;\n-getwebproxy) printf 'Enabled: Yes\\nServer: 127.0.0.1\\nPort: %s\\n' \"$GOPURR_TEST_PROXY_PORT\";;\nesac\n",
		"osascript":    "#!/bin/sh\nfirst=0\nif [ ! -e \"$GOPURR_TEST_PROXY_LOG\" ]; then first=1; fi\nprintf '%s\\n' \"$*\" >> \"$GOPURR_TEST_PROXY_LOG\"\nif [ \"$GOPURR_TEST_PROXY_CANCEL\" = 1 ] && [ \"$first\" = 1 ]; then printf 'User canceled.\\n'; exit 1; fi\n",
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// All OS commands are fakes. The test never changes the machine's proxy
	// settings or invokes an authorization dialog.
	t.Setenv("PATH", binDir)
	t.Setenv("GOPURR_TEST_PROXY_PORT", strconv.Itoa(port))
	t.Setenv("GOPURR_TEST_PROXY_LOG", logPath)
	if cancelFirst {
		t.Setenv("GOPURR_TEST_PROXY_CANCEL", "1")
	} else {
		t.Setenv("GOPURR_TEST_PROXY_CANCEL", "0")
	}
	return logPath
}

// A controller fake exercises the real startKernel/configBytes/file paths
// without spawning a process. The optional rejection models a kernel that
// refuses the candidate while retaining the previously running config.
func newSettingsTestApp(t *testing.T, reject func(string) bool) (*App, func() []string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "gopurr-settings-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("GOPURR_HOME", dir)
	a := New()
	if err := writeSettingsFile(a.ovPath, a.ov); err != nil {
		t.Fatal(err)
	}
	raw, err := a.configBytes()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.mgr.ConfigPath(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", filepath.Join(dir, "core.sock"))
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var payloads []string
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"version":"1.19.33"}`)
	})
	mux.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Payload string `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		payloads = append(payloads, request.Payload)
		mu.Unlock()
		if reject != nil && reject(request.Payload) {
			http.Error(w, "test rejection", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	a.initSettings()
	return a, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), payloads...)
	}
}

func TestPrefsSettingsTabsPreserveDraftsAt840Width(t *testing.T) {
	a := &App{prefs: defaultPrefs(), cfgText: demoConfig, ov: patch.Default(), page: "settings", dir: "/tmp/Gopurr"}
	tr := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme(c.Theme().Dark))
		ui.Row(c).Fill().Children(func() {
			ui.Box(c).Width(226).FillHeight() // reserve the real sidebar's width
			ui.Column(c).FillWidth().FillHeight().Padding(22, 24).Gap(16).Children(func() {
				a.settingsPage(c)
			})
		})
	}, 840, 720)
	tabs := []struct {
		label string
		shown string
	}{
		{"常规", "内核自动运行"},
		{"网络", "保存网络设置"},
		{"延迟测试", "保存测试设置"},
		{"维护", "查看运行配置…"},
	}
	for index, tab := range tabs {
		if err := tr.Click(tab.label); err != nil {
			t.Fatal(err)
		}
		tr.Frame()
		if a.settingsTab != index {
			t.Fatalf("tab %q selected %d", tab.label, a.settingsTab)
		}
		box, ok := tr.Find(tab.shown)
		if !ok || box.X < 250 || box.X+box.W > 816 || box.Y < 0 || box.Y+box.H > 720 {
			t.Fatalf("%q is not usable at 840px: %+v, found=%v", tab.shown, box, ok)
		}
		for other, hidden := range tabs {
			if other != index && tr.HasText(hidden.shown) {
				t.Fatalf("%q leaked into %q tab", hidden.shown, tab.label)
			}
		}
		if index == 1 {
			a.mixedPortDraft, a.lanOn = "9000", true
		}
		if index == 2 {
			a.testURLDraft, a.testTimeoutPick, a.testConcurrencyPick = "https://example.com/204", "15000", "4"
		}
	}
	if err := tr.Click("网络"); err != nil {
		t.Fatal(err)
	}
	if a.mixedPortDraft != "9000" || !a.lanOn {
		t.Fatal("network draft lost across tabs")
	}
	if err := tr.Click("延迟测试"); err != nil {
		t.Fatal(err)
	}
	if a.testURLDraft != "https://example.com/204" || a.testTimeoutPick != "15000" || a.testConcurrencyPick != "4" || a.Prefs() != defaultPrefs() {
		t.Fatal("test draft lost or prematurely saved across tabs")
	}
}

func TestDelayIndexHelpers(t *testing.T) {
	for _, secs := range []int{30, 60, 300} {
		if idx := delayIndexOf(secs); delayIndexOf(lightDelays[idx]) != idx {
			t.Fatalf("delayIndexOf(%d) does not round-trip", secs)
		}
		if delayLabelOf(secs) == "" {
			t.Fatalf("no label for %d", secs)
		}
	}
}
