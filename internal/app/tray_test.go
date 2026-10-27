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
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"go.yaml.in/yaml/v3"
)

func TestTraySwitchesWithoutWindow(t *testing.T) {
	a, controller := newTrayTestApp(t)
	proxyLog := fakeTraySystemProxy(t, a.mixedPort())
	opened := 0
	items := a.trayItems(true, nil, func() { opened++ })
	if runtime.GOOS == "darwin" {
		proxy := findTrayItem(t, items, "system-proxy")
		clickTrayItem(t, proxy)
		if !a.sysApplied || !a.sysOn || !proxy.IsChecked() {
			t.Fatal("tray did not enable system proxy")
		}
		clickTrayItem(t, proxy)
		if a.sysApplied || a.sysOn || proxy.IsChecked() {
			t.Fatal("tray did not disable system proxy")
		}
		calls, err := os.ReadFile(proxyLog)
		if err != nil || !strings.Contains(string(calls), "-setwebproxy") || !strings.Contains(string(calls), "-setwebproxystate") {
			t.Fatalf("missing proxy enable/disable commands: %q, %v", calls, err)
		}
	}
	tun := findTrayItem(t, items, "tun")
	clickTrayItem(t, tun)
	if !a.ov.TunEnable || !a.tunOn || !tun.IsChecked() {
		t.Fatal("tray did not enable TUN")
	}
	clickTrayItem(t, tun)
	if a.ov.TunEnable || a.tunOn || tun.IsChecked() {
		t.Fatal("tray did not disable TUN")
	}
	controller.mu.Lock()
	payloads := append([]string(nil), controller.reloads...)
	controller.mu.Unlock()
	if len(payloads) != 2 {
		t.Fatalf("TUN reloads = %d, want 2", len(payloads))
	}
	for i, payload := range payloads {
		var cfg struct {
			Tun struct{ Enable bool } `yaml:"tun"`
		}
		if err := yaml.Unmarshal([]byte(payload), &cfg); err != nil || cfg.Tun.Enable != (i == 0) {
			t.Fatalf("TUN payload %d not applied: %s, %v", i, payload, err)
		}
	}
	if opened != 0 {
		t.Fatalf("successful tray actions opened %d windows", opened)
	}
	var saved struct{ TunEnable bool }
	body, err := os.ReadFile(a.ovPath)
	if err != nil || json.Unmarshal(body, &saved) != nil || saved.TunEnable {
		t.Fatalf("TUN state not persisted: %s, %v", body, err)
	}
}

func TestTrayFailuresRestoreChecksAndShowErrors(t *testing.T) {
	a, controller := newTrayTestApp(t)
	fakeTraySystemProxy(t, a.mixedPort())
	opened := 0
	openWindow := func() { opened++ }
	if runtime.GOOS == "darwin" {
		t.Setenv("GOPURR_TEST_TRAY_PROXY_CANCEL", "1")
		proxy := findTrayItem(t, a.trayItems(true, nil, openWindow), "system-proxy")
		clickTrayItem(t, proxy)
		if proxy.IsChecked() || a.sysApplied || a.sysOn || !strings.Contains(a.sysErrText(), "已取消授权") || opened != 1 || a.page != "overview" {
			t.Fatalf("canceled proxy authorization not reflected: checked=%v error=%q opened=%d", proxy.IsChecked(), a.sysErrText(), opened)
		}
	}
	controller.mu.Lock()
	controller.reject = true
	controller.mu.Unlock()
	opened = 0
	items := a.trayItems(true, a.k.Groups(), openWindow)
	tun := findTrayItem(t, items, "tun")
	clickTrayItem(t, tun)
	if tun.IsChecked() || a.tunOn || a.ov.TunEnable || a.tunErrText() == "" || opened != 1 {
		t.Fatal("rejected TUN reload did not restore state and show its error")
	}
	mode := findTrayItem(t, items, "mode-global")
	clickTrayItem(t, mode)
	if mode.IsChecked() || !findTrayItem(t, items, "mode-rule").IsChecked() || a.modeToken != "rule" || !strings.Contains(a.notice, "切换模式失败") {
		t.Fatal("rejected mode change left an incorrect check")
	}
	group := findTrayItem(t, items, "select-node").Submenu[0]
	clickTrayItem(t, group.Submenu[1])
	if group.Submenu[1].IsChecked() || !group.Submenu[0].IsChecked() || a.page != "proxies" || !strings.Contains(a.notice, "切换节点失败") {
		t.Fatal("rejected node selection did not restore the original selection")
	}
}

func TestTrayNodesAndModesRoundTrip(t *testing.T) {
	a, controller := newTrayTestApp(t)
	fakeTraySystemProxy(t, a.mixedPort())
	opened := 0
	openWindow := func() { opened++ }
	items := a.trayItems(true, a.k.Groups(), openWindow)
	nodes := findTrayItem(t, items, "select-node").Submenu
	if len(nodes) != 4 || !strings.HasPrefix(nodes[0].Label, "出口 / 主组") || !strings.HasPrefix(nodes[1].Label, "自动选择") {
		t.Fatalf("rule menu must exclude GLOBAL and hidden groups: %+v", nodes)
	}
	manual, automatic := nodes[0], nodes[1]
	if len(manual.Submenu) != 2 || !manual.Submenu[0].IsChecked() {
		t.Fatal("duplicate nodes or missing current check")
	}
	for _, node := range automatic.Submenu {
		if node.Type == mygo.MenuItemRadio && !node.Disabled {
			t.Fatal("automatically scheduled group must not permit manual selection")
		}
	}
	selectedName := manual.Submenu[1].ToolTip
	if !strings.HasSuffix(manual.Submenu[1].Label, "…") || utf8.RuneCountInString(manual.Submenu[1].Label) > 32 {
		t.Fatal("long node label was not bounded")
	}
	clickTrayItem(t, manual.Submenu[1])
	controller.mu.Lock()
	selection := append([]string(nil), controller.selected...)
	controller.mu.Unlock()
	if !reflect.DeepEqual(selection, []string{"出口 / 主组", selectedName}) {
		t.Fatalf("controller did not receive full group/node names: %q", selection)
	}
	updated := a.TrayMenu(openWindow).ItemByID("select-node").Submenu[0]
	if !updated.Submenu[1].IsChecked() || updated.Submenu[0].IsChecked() {
		t.Fatal("reopened menu retained stale node selection")
	}
	clickTrayItem(t, findTrayItem(t, items, "mode-global"))
	global := a.TrayMenu(openWindow)
	if !global.ItemByID("mode-global").IsChecked() || global.ItemByID("mode-rule").IsChecked() {
		t.Fatal("reopened menu retained stale routing mode")
	}
	globalNodes := global.ItemByID("select-node").Submenu
	if len(globalNodes) != 3 || !strings.HasPrefix(globalNodes[0].Label, "GLOBAL") {
		t.Fatal("global mode must use GLOBAL's selection")
	}
	clickTrayItem(t, findTrayItem(t, items, "mode-direct"))
	direct := a.TrayMenu(openWindow).ItemByID("select-node").Submenu
	if direct[0].Label != "直连模式无需选择节点" || !direct[0].Disabled || opened != 0 {
		t.Fatal("direct mode did not explain node selection availability")
	}
}

func TestTrayUnavailableStates(t *testing.T) {
	a, _ := newTrayTestApp(t)
	if err := os.Remove(filepath.Join(a.dir, "core.json")); err != nil {
		t.Fatal(err)
	}
	opened := 0
	items := a.trayItems(false, nil, func() { opened++ })
	if !findTrayItem(t, items, "tun").Disabled || findTrayItem(t, items, "reload").Label != "启动内核" {
		t.Fatal("unavailable service/kernel states not reflected")
	}
	clickTrayItem(t, findTrayItem(t, items, "tun-service"))
	if opened != 1 || a.page != "overview" || a.confirmInstall {
		t.Fatal("service setup must open overview without starting installation")
	}
	stopped := findTrayItem(t, items, "select-node").Submenu[0]
	if !stopped.Disabled || stopped.Label != "启动内核后可选择节点" {
		t.Fatal("stopped kernel has no explanation")
	}
	a.svcBusy = true
	items = a.trayItems(true, nil, func() {})
	for _, id := range []string{"system-proxy", "tun", "reload", "mode-rule", "tun-service"} {
		if !findTrayItem(t, items, id).Disabled {
			t.Fatalf("%s remains actionable during a service transition", id)
		}
	}
	a.k = nil
	items = a.trayItems(false, nil, func() {})
	if !findTrayItem(t, items, "system-proxy").Disabled || !findTrayItem(t, items, "reload").Disabled {
		t.Fatal("missing kernel must disable network actions")
	}
}

func TestTrayRefreshesSystemProxyState(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS system proxy command fixtures")
	}
	a, _ := newTrayTestApp(t)
	fakeTraySystemProxy(t, a.mixedPort())
	for _, state := range []struct {
		server string
		on     bool
	}{
		{"127.0.0.1", true},
		{"another-client.example", false},
	} {
		body := fmt.Sprintf("Enabled: Yes\nServer: %s\nPort: %d\n", state.server, a.mixedPort())
		if err := os.WriteFile(os.Getenv("GOPURR_TEST_TRAY_PROXY_STATE"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		menu := a.TrayMenu(func() {})
		if menu.ItemByID("system-proxy").IsChecked() != state.on || a.sysOn != state.on || a.sysApplied != state.on {
			t.Fatalf("menu did not reflect the current system proxy owner %q", state.server)
		}
	}
}

func findTrayItem(t *testing.T, items []*mygo.MenuItem, id string) *mygo.MenuItem {
	t.Helper()
	var find func([]*mygo.MenuItem) *mygo.MenuItem
	find = func(list []*mygo.MenuItem) *mygo.MenuItem {
		for _, item := range list {
			if item.ID == id {
				return item
			}
			if child := find(item.Submenu); child != nil {
				return child
			}
		}
		return nil
	}
	if item := find(items); item != nil {
		return item
	}
	t.Fatalf("tray item %q missing", id)
	return nil
}

// Match mygo's pre-click checkbox/radio behavior without a native event loop.
func clickTrayItem(t *testing.T, item *mygo.MenuItem) {
	t.Helper()
	if item.Disabled || item.Click == nil {
		t.Fatalf("tray item %q is not actionable", item.Label)
	}
	if item.Type == mygo.MenuItemCheckbox {
		item.SetChecked(!item.IsChecked())
	} else if item.Type == mygo.MenuItemRadio {
		item.SetChecked(true)
	}
	item.Click(item, nil)
}

type trayTestController struct {
	mu       sync.Mutex
	mode     string
	now      string
	reject   bool
	selected []string
	reloads  []string
}

func newTrayTestApp(t *testing.T) (*App, *trayTestController) {
	t.Helper()
	dir, err := os.MkdirTemp("", "gopurr-tray-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("GOPURR_HOME", dir)
	a := New()
	if err := os.WriteFile(filepath.Join(dir, "core.json"), []byte(`{"uid":0}`), 0o600); err != nil {
		t.Fatal(err)
	}
	controller := &trayTestController{mode: "rule", now: "香港 01"}
	longNode := "日本 / 专线 " + strings.Repeat("高速低延迟节点", 9)
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"version":"test"}`)
	})
	mux.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
		controller.mu.Lock()
		defer controller.mu.Unlock()
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]string{"mode": controller.mode})
			return
		}
		if controller.reject {
			http.Error(w, "test rejection", http.StatusBadRequest)
			return
		}
		var request struct{ Mode, Payload string }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if request.Mode != "" {
			controller.mode = request.Mode
		} else {
			controller.reloads = append(controller.reloads, request.Payload)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/proxies", func(w http.ResponseWriter, r *http.Request) {
		controller.mu.Lock()
		defer controller.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"proxies": map[string]any{
			"GLOBAL":  map[string]any{"type": "Selector", "now": "出口 / 主组", "all": []string{"出口 / 主组", "DIRECT"}},
			"出口 / 主组": map[string]any{"type": "Selector", "now": controller.now, "all": []string{"香港 01", longNode, longNode}},
			"自动选择":    map[string]any{"type": "URLTest", "now": "香港 01", "all": []string{"香港 01", longNode}},
			"隐藏":      map[string]any{"type": "Selector", "hidden": true, "all": []string{"DIRECT"}},
		}})
	})
	mux.HandleFunc("/proxies/", func(w http.ResponseWriter, r *http.Request) {
		controller.mu.Lock()
		defer controller.mu.Unlock()
		if controller.reject {
			http.Error(w, "test rejection", http.StatusBadRequest)
			return
		}
		var request struct{ Name string }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		controller.now = request.Name
		controller.selected = []string{strings.TrimPrefix(r.URL.Path, "/proxies/"), request.Name}
		w.WriteHeader(http.StatusNoContent)
	})
	ln, err := net.Listen("unix", filepath.Join(dir, "core.sock"))
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() { _ = server.Close() })
	return a, controller
}

// All OS proxy commands are fixtures: no authorization prompt or system change.
func fakeTraySystemProxy(t *testing.T, port int) string {
	t.Helper()
	if runtime.GOOS != "darwin" {
		return ""
	}
	dir := t.TempDir()
	state, logPath := filepath.Join(dir, "state"), filepath.Join(dir, "calls")
	if err := os.WriteFile(state, []byte("Enabled: No\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	scripts := map[string]string{
		"route": "#!/bin/sh\nprintf 'interface: en0\\n'\n",
		"networksetup": `#!/bin/sh
case "$1" in
-listnetworkserviceorder) printf '(1) Wi-Fi\n(Hardware Port: Wi-Fi, Device: en0)\n';;
-getwebproxy) /bin/cat "$GOPURR_TEST_TRAY_PROXY_STATE";;
esac
`,
		"osascript": `#!/bin/sh
printf '%s\n' "$*" >> "$GOPURR_TEST_TRAY_PROXY_LOG"
if [ "$GOPURR_TEST_TRAY_PROXY_CANCEL" = 1 ]; then printf 'User canceled.\n'; exit 1; fi
case "$*" in
*-setwebproxystate*) printf 'Enabled: No\n' > "$GOPURR_TEST_TRAY_PROXY_STATE";;
*-setwebproxy*) printf 'Enabled: Yes\nServer: 127.0.0.1\nPort: %s\n' "$GOPURR_TEST_TRAY_PROXY_PORT" > "$GOPURR_TEST_TRAY_PROXY_STATE";;
esac
`,
	}
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("GOPURR_TEST_TRAY_PROXY_STATE", state)
	t.Setenv("GOPURR_TEST_TRAY_PROXY_LOG", logPath)
	t.Setenv("GOPURR_TEST_TRAY_PROXY_PORT", fmt.Sprint(port))
	t.Setenv("GOPURR_TEST_TRAY_PROXY_CANCEL", "0")
	return logPath
}
