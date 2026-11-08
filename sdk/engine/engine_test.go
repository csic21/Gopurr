package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/mihomo/component/profile/cachefile"
	"go.yaml.in/yaml/v3"
)

const directConfig = `mode: rule
log-level: warning
proxies:
  - {name: 直连 A, type: direct}
  - {name: 直连 B, type: direct}
proxy-groups:
  - name: 出口
    type: select
    proxies: [直连 A, 直连 B]
rules: ['MATCH,出口']
`

func call(t *testing.T, e *Engine, method string, params any) json.RawMessage {
	t.Helper()
	body, err := json.Marshal(map[string]any{"id": "test", "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		ID     string          `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *Error          `json:"error"`
	}
	if err := json.Unmarshal(e.Dispatch(context.Background(), body), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != nil {
		t.Fatalf("%s: %s (%s)", method, response.Error.Message, response.Error.Code)
	}
	if response.ID != "test" {
		t.Fatalf("request ID lost: %q", response.ID)
	}
	return response.Result
}

// The integration deliberately uses one engine/home: upstream mihomo caches
// are process singletons. Every HTTP source and destination stays on loopback.
func TestEngineIntegration(t *testing.T) {
	home := t.TempDir()
	e, err := Open(Options{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = e.Close()
		if db := cachefile.Cache().DB; db != nil {
			_ = db.Close()
		}
	})
	if _, err := Open(Options{Home: home}); err == nil {
		t.Fatal("accepted two in-process owners")
	}

	var initial Status
	if err := json.Unmarshal(call(t, e, "engine.status", nil), &initial); err != nil {
		t.Fatal(err)
	}
	if initial.Running || initial.Tun || initial.SystemProxy || initial.VPNSupported != mobileVPNSupported() || initial.MixedPort == 0 {
		t.Fatalf("misreported host capabilities: %+v", initial)
	}
	if _, err := e.Call(context.Background(), "engine.vpn", nil); asError(err).Code != "unsupported" {
		t.Fatalf("VPN available without host: %v", err)
	}
	if _, err := e.Call(context.Background(), "engine.start", nil); asError(err).Code != "no_profile" {
		t.Fatalf("started without a profile: %v", err)
	}

	// The subscription's token, path and userinfo must never reach summaries.
	var sourceBody atomic.Value
	sourceBody.Store(directConfig)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("profile-title", "测试订阅")
		w.Header().Set("subscription-userinfo", "upload=10; download=20; total=1000")
		_, _ = io.WriteString(w, sourceBody.Load().(string))
	}))
	defer source.Close()
	var imported Profile
	result := call(t, e, "profiles.import", map[string]string{"url": source.URL + "/private/subscription?token=secret-value"})
	if strings.Contains(string(result), "secret-value") || strings.Contains(string(result), "/private/") {
		t.Fatalf("summary leaked credentials: %s", result)
	}
	if err := json.Unmarshal(result, &imported); err != nil {
		t.Fatal(err)
	}
	if imported.Name != "测试订阅" || imported.ID == "" || !imported.Active {
		t.Fatalf("import result: %+v", imported)
	}
	var list ProfileList
	_ = json.Unmarshal(call(t, e, "profiles.list", nil), &list)
	if len(list.Profiles) != 1 || list.Active != imported.ID {
		t.Fatalf("profiles: %+v", list)
	}

	call(t, e, "profiles.activate", map[string]string{"id": imported.ID})
	var groups GroupList
	_ = json.Unmarshal(call(t, e, "proxies.groups", nil), &groups)
	if len(groups.Groups) == 0 || groups.Groups[0].Name != "出口" || groups.Groups[0].Now != "直连 A" {
		t.Fatalf("groups: %+v", groups)
	}
	call(t, e, "proxies.select", map[string]string{"group": "出口", "member": "直连 B"})
	call(t, e, "engine.start", nil) // Idempotent: do not reset a live selection.
	_ = json.Unmarshal(call(t, e, "proxies.groups", nil), &groups)
	if groups.Groups[0].Now != "直连 B" {
		t.Fatalf("start reset live node: %+v", groups)
	}
	call(t, e, "engine.stop", nil)
	call(t, e, "engine.start", nil)
	_ = json.Unmarshal(call(t, e, "proxies.groups", nil), &groups)
	if groups.Groups[0].Now != "直连 B" {
		t.Fatalf("reconnect reset manually selected node: %+v", groups)
	}

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "gopurr-native-bridge") }))
	defer origin.Close()
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", initial.MixedPort))
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 4 * time.Second}
	resp, err := client.Get(origin.URL)
	if err != nil {
		t.Fatalf("real proxy request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "gopurr-native-bridge" {
		t.Fatalf("proxy response: %q", body)
	}
	call(t, e, "proxies.delay", map[string]string{"name": "直连 B", "url": origin.URL})
	var status Status
	_ = json.Unmarshal(call(t, e, "engine.status", nil), &status)
	if !status.Running || status.Traffic.UpTotal == 0 || status.Traffic.DownTotal == 0 {
		t.Fatalf("missing real accounting: %+v", status)
	}

	// A rejected import must leave both the active cache and running selection.
	_, err = e.Call(context.Background(), "profiles.import", json.RawMessage(`{"name":"坏配置","yaml":"rules: [MATCH,不存在]\n"}`))
	if err == nil {
		t.Fatal("accepted invalid rule destination")
	}
	_ = json.Unmarshal(call(t, e, "profiles.list", nil), &list)
	if len(list.Profiles) != 1 || list.Active != imported.ID {
		t.Fatalf("failed import changed profiles: %+v", list)
	}
	_ = json.Unmarshal(call(t, e, "proxies.groups", nil), &groups)
	if groups.Groups[0].Now != "直连 B" {
		t.Fatalf("failed import changed live node: %+v", groups)
	}

	// Corrupting a candidate cache simulates an external edit. It must never
	// take the live working profile down when activation fails validation.
	var second Profile
	_ = json.Unmarshal(call(t, e, "profiles.import", map[string]string{"name": "本地配置", "yaml": directConfig + "\n# second\n"}), &second)
	// Identically named groups in another source keep their own preferences.
	call(t, e, "profiles.activate", map[string]string{"id": second.ID})
	_ = json.Unmarshal(call(t, e, "proxies.groups", nil), &groups)
	if groups.Groups[0].Now != "直连 A" {
		t.Fatalf("new profile inherited another profile's node: %+v", groups)
	}
	call(t, e, "proxies.select", map[string]string{"group": "出口", "member": "直连 A"})
	call(t, e, "profiles.activate", map[string]string{"id": imported.ID})
	_ = json.Unmarshal(call(t, e, "proxies.groups", nil), &groups)
	if groups.Groups[0].Now != "直连 B" {
		t.Fatalf("profile switch lost saved node: %+v", groups)
	}
	call(t, e, "profiles.activate", map[string]string{"id": second.ID})
	_ = json.Unmarshal(call(t, e, "proxies.groups", nil), &groups)
	if groups.Groups[0].Now != "直连 A" {
		t.Fatalf("profile selections were mixed: %+v", groups)
	}
	call(t, e, "profiles.activate", map[string]string{"id": imported.ID})
	// A disk commit error happens after the candidate reached the kernel. The
	// rollback must restore the previous node as well as the active profile.
	indexPath := filepath.Join(home, "profiles.json")
	backupPath := indexPath + ".test-backup"
	if err := os.Rename(indexPath, backupPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(indexPath, 0700); err != nil {
		t.Fatal(err)
	}
	_, err = e.Call(context.Background(), "profiles.activate", json.RawMessage(fmt.Sprintf(`{"id":%q}`, second.ID)))
	if err == nil {
		t.Fatal("activation ignored index commit failure")
	}
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backupPath, indexPath); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(call(t, e, "proxies.groups", nil), &groups)
	if groups.Groups[0].Now != "直连 B" {
		t.Fatalf("rollback changed live selection: %+v", groups)
	}
	stored, _ := e.store.Get(second.ID)
	if err := os.WriteFile(filepath.Join(home, "profiles", stored.File), []byte("rules: [MATCH,missing]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = e.Call(context.Background(), "profiles.activate", json.RawMessage(fmt.Sprintf(`{"id":%q}`, second.ID)))
	if err == nil {
		t.Fatal("activated externally corrupted profile")
	}
	_ = json.Unmarshal(call(t, e, "engine.status", nil), &status)
	if !status.Running || status.ActiveProfile != imported.ID {
		t.Fatalf("failed activation lost working profile: %+v", status)
	}

	call(t, e, "engine.mode", map[string]string{"mode": "direct"})
	testLivePreferenceSaveFailure(t, e, imported.ID)
	call(t, e, "engine.stop", nil)
	_ = json.Unmarshal(call(t, e, "engine.status", nil), &status)
	if status.Running || status.Traffic.At != 0 {
		t.Fatalf("stopped engine has live accounting: %+v", status)
	}
	if conn, err := net.DialTimeout("tcp", proxyURL.Host, 200*time.Millisecond); err == nil {
		conn.Close()
		t.Fatal("proxy listener survived stop")
	}
	call(t, e, "engine.start", nil)
	_ = json.Unmarshal(call(t, e, "engine.status", nil), &status)
	if !status.Running || status.Mode != "direct" {
		t.Fatalf("restart failed: %+v", status)
	}

	// Review the exact generated runtime, not only a running flag.
	malicious := directConfig + `external-controller: 0.0.0.0:9090
external-controller-unix: /tmp/unowned-controller.sock
external-ui-url: https://example.invalid/ui.zip
secret: untrusted
allow-lan: true
bind-address: '*'
port: 8000
socks-port: 8001
authentication: [user:password]
tun: {enable: true, auto-route: true}
listeners: [{name: unsafe, type: http, port: 8002, listen: 0.0.0.0}]
dns: {enable: false, listen: '0.0.0.0:53'}
ntp: {enable: true, write-to-system: true}
iptables: {enable: true}
`
	managed, err := e.prepareLocked([]byte(malicious), stored)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(managed, &doc); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"external-controller", "external-controller-unix", "external-ui-url", "secret", "port", "socks-port", "authentication", "listeners"} {
		if _, ok := doc[key]; ok {
			t.Fatalf("unsafe runtime field survived: %s", key)
		}
	}
	if doc["allow-lan"] != false || doc["bind-address"] != "127.0.0.1" || doc["find-process-mode"] != "off" || doc["tun"].(map[string]any)["enable"] != false {
		t.Fatalf("unsafe runtime: %s", managed)
	}
	if _, err := e.prepareLocked([]byte(directConfig+"proxy-providers: {remote: {type: file, path: ../outside.yaml}}\n"), stored); err == nil {
		t.Fatal("accepted provider path traversal")
	}

	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Call(context.Background(), "engine.status", nil); asError(err).Code != "closed" {
		t.Fatalf("closed engine still callable: %v", err)
	}
	reopened, err := Open(Options{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	var reopenedList ProfileList
	_ = json.Unmarshal(call(t, reopened, "profiles.list", nil), &reopenedList)
	if len(reopenedList.Profiles) != 2 || reopenedList.Active != imported.ID {
		t.Fatalf("profiles did not survive host reopen: %+v", reopenedList)
	}
	_ = json.Unmarshal(call(t, reopened, "engine.status", nil), &status)
	if status.Mode != "direct" || status.Running {
		t.Fatalf("host reopen lost routing mode or connected automatically: %+v", status)
	}
	call(t, reopened, "engine.start", nil)
	_ = json.Unmarshal(call(t, reopened, "proxies.groups", nil), &groups)
	if groups.Groups[0].Now != "直连 B" || reopened.manualSelections[second.ID]["出口"] != "直连 A" {
		t.Fatalf("host reopen lost per-profile choices: %+v", groups)
	}
	testProfileChanges(t, reopened, imported.ID, source.URL, &sourceBody)
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchValidation(t *testing.T) {
	// Parameter rejection never needs the process singleton or any listeners.
	for _, input := range []string{"[]", "{", `{"method":"engine.status","extra":true}`, `{"method":"engine.status"} {}`} {
		var response Response
		if err := json.Unmarshal((*Engine)(nil).Dispatch(context.Background(), []byte(input)), &response); err != nil {
			t.Fatal(err)
		}
		if response.Error == nil || response.Error.Code != "invalid_request" {
			t.Fatalf("request accepted: %s => %+v", input, response)
		}
	}
	for _, options := range []Options{{}, {Home: "relative"}, {Home: "/", MixedPort: 1}, {Home: "/tmp/never-created", MixedPort: 65536}} {
		if _, err := Open(options); err == nil {
			t.Fatalf("accepted invalid options: %+v", options)
		}
	}
}
