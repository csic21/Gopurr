package app

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/core"
)

func TestStartupPreferenceRollbackAndSilentPersistence(t *testing.T) {
	t.Setenv("GOPURR_HOME", t.TempDir())
	a := New()
	var calls []bool
	a.applyAutoLaunch = func(enabled bool) error { calls = append(calls, enabled); return nil }
	p := a.Prefs()
	p.AutoLaunch, p.SilentStart = true, true
	if err := a.UpdatePrefs(p); err != nil {
		t.Fatal(err)
	}
	if got := New().Prefs(); !got.AutoLaunch || !got.SilentStart {
		t.Fatal("startup preferences not persisted")
	}
	a.prefsPath = t.TempDir()
	p.AutoLaunch = false
	if err := a.UpdatePrefs(p); err == nil {
		t.Fatal("expected persistence failure")
	}
	if !a.Prefs().AutoLaunch || len(calls) != 3 || calls[1] || !calls[2] {
		t.Fatalf("startup registration not rolled back: %v", calls)
	}
	a.applyAutoLaunch = func(bool) error { return errors.New("registration denied") }
	if err := a.UpdatePrefs(p); err == nil || !a.Prefs().AutoLaunch {
		t.Fatal("registration error was hidden")
	}
}

func TestSubscriptionEditorAndIndependentSchedule(t *testing.T) {
	a := subscriptionTestApp(t)
	// Use a local HTTP proxy fixture via the fake controller-independent source.
	source := startSubscriptionSource(t)
	p, err := a.store.Add(source, "测试")
	if err != nil {
		t.Fatal(err)
	}
	a.openSubscriptionEditor(p)
	a.editSub.Name = "工作订阅"
	a.editSubPolicy = 2
	a.editSubHours = "-1"
	a.saveSubscriptionEditor()
	if a.editSubError == "" || !a.showEditSub {
		t.Fatal("invalid interval accepted")
	}
	a.editSubHours = "1"
	a.saveSubscriptionEditor()
	if a.showEditSub {
		t.Fatal(a.editSubError)
	}
	// Independent intervals work even with global updates disabled.
	a.store.Items[0].Updated = time.Now().Add(-2 * time.Hour)
	a.maybeAutoUpdate()
	if time.Since(a.store.List()[0].Updated) > time.Minute {
		t.Fatal("independent schedule ignored global-off profile")
	}
}
func startSubscriptionSource(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("rules: [MATCH,DIRECT]")) })}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return "http://" + ln.Addr().String()
}

func TestConnectionActionsAndScopedNodeCleanup(t *testing.T) {
	dir, err := os.MkdirTemp("", "gp-conn-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("GOPURR_HOME", dir)
	a := New()
	ln, err := net.Listen("unix", filepath.Join(dir, "core.sock"))
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var deleted []string
	reject := false
	rows := []map[string]any{
		{"id": "old", "metadata": map[string]string{"host": "api.example.com", "network": "tcp", "process": "Browser", "processPath": "/Applications/Browser", "sourceIP": "127.0.0.1", "sourcePort": "1234", "destinationIP": "1.2.3.4", "destinationPort": "443"}, "chains": []string{"节点选择", "旧节点"}, "rule": "Domain", "rulePayload": "example.com"},
		{"id": "other", "metadata": map[string]string{"host": "other.example"}, "chains": []string{"其他策略组", "旧节点"}},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/connections", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			mu.Lock()
			deleted = append(deleted, "all")
			mu.Unlock()
			w.WriteHeader(204)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"connections": rows})
	})
	mux.HandleFunc("/connections/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		deleted = append(deleted, strings.TrimPrefix(r.URL.Path, "/connections/"))
		mu.Unlock()
		w.WriteHeader(204)
	})
	mux.HandleFunc("/proxies", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"proxies":{"节点选择":{"type":"Selector","now":"旧节点","all":["旧节点","新节点"]}}}`))
	})
	mux.HandleFunc("/proxies/", func(w http.ResponseWriter, r *http.Request) {
		if reject {
			http.Error(w, "rejected", 400)
			return
		}
		w.WriteHeader(204)
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	a.refreshConnections()
	if len(filterConnections(a.connRows, "browser")) != 1 || len(filterConnections(a.connRows, "1.2.3.4")) != 1 || a.connRows[0].ID == "" {
		t.Fatal("connection metadata missing")
	}
	a.prefs.CloseOnSwitch = true
	if !a.selectProxy("节点选择", "新节点") {
		t.Fatal(a.notice)
	}
	mu.Lock()
	got := strings.Join(deleted, ",")
	mu.Unlock()
	if got != "old" {
		t.Fatalf("closed unrelated connections: %s", got)
	}
	reject = true
	if a.selectProxy("节点选择", "新节点") {
		t.Fatal("rejected selection succeeded")
	}
	mu.Lock()
	count := len(deleted)
	mu.Unlock()
	if count != 1 {
		t.Fatal("failed selection closed connections")
	}
	a.disconnectConnection("other")
	if err := a.k.CloseAllConnections(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got = strings.Join(deleted, ",")
	mu.Unlock()
	if got != "old,other,all" {
		t.Fatalf("close actions failed: %s", got)
	}
}

func TestRestoreBackupAppliesAndRollsBackRejectedRuntime(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "rejected"}[rejected], func(t *testing.T) {
			a, _ := newSettingsTestApp(t, func(raw string) bool { return rejected && strings.Contains(raw, "restored.example") })
			s, err := a.backupSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			s.Files["local.yaml"] = []byte("rules:\n - DOMAIN,restored.example,DIRECT\n - MATCH,DIRECT\n")
			original := a.cfgText
			err = a.restoreBackup(s)
			if rejected {
				if err == nil || a.cfgText != original {
					t.Fatal("rejected backup changed live config")
				}
				if _, err := os.Stat(a.cfgPath); !os.IsNotExist(err) {
					t.Fatal("original absent local file not restored")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(a.cfgText, "restored.example") {
					t.Fatal("restored config not published")
				}
			}
		})
	}
}
func TestRestoreRejectsMissingProfileAndPreservesMachineSettings(t *testing.T) {
	t.Setenv("GOPURR_HOME", t.TempDir())
	a := New()
	a.ov.MixedPort = 9876
	a.prefs.AutoLaunch = true
	s, err := a.backupSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	p, ov, _, err := a.prepareRestore(s)
	if err != nil {
		t.Fatal(err)
	}
	if !p.AutoLaunch || ov.MixedPort != 9876 {
		t.Fatal("machine preferences not preserved")
	}
	s.Files["profiles.json"] = []byte(`{"profiles":[],"active":"missing"}`)
	if _, _, _, err := a.prepareRestore(s); err == nil {
		t.Fatal("missing active config accepted")
	}
}
func TestProductFeatureControlsFitMinimumWindow(t *testing.T) {
	a := subscriptionTestApp(t)
	a.booted, a.ticking = true, true
	a.page = "conns"
	a.connFetched = time.Now()
	a.connRows = []core.Conn{{ID: "sample", Host: "api.example.com", Process: "Browser", Chain: "节点选择 » 香港 01"}}
	tr := ui.NewTester(a.View, 840, 560)
	tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 88})
	tr.Advance(200 * time.Millisecond)
	tr.Frame()
	for _, label := range []string{"搜索连接", "查看连接 api.example.com", "断开全部…"} {
		box, ok := tr.Find(label)
		if !ok || box.X+box.W > 816 || box.Y+box.H > 544 {
			t.Fatalf("control %s outside minimum window: %+v", label, box)
		}
	}
	if err := tr.Click("查看连接 api.example.com"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	box, ok := tr.Find("断开连接")
	if !ok || box.Y+box.H > 544 {
		t.Fatalf("connection detail footer clipped: %+v", box)
	}
}

func TestChangedSubscriptionSourceRejectedByKernelKeepsCache(t *testing.T) {
	a, reloads := newSettingsTestApp(t, func(raw string) bool { return strings.Contains(raw, "rejected.example") })
	raw := "rules:\n - DOMAIN,original.example,DIRECT\n - MATCH,DIRECT\n"
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(raw)) })}
	go srv.Serve(ln)
	defer srv.Close()
	p, err := a.store.Add("http://"+ln.Addr().String(), "测试订阅")
	if err != nil {
		t.Fatal(err)
	}
	before, err := a.store.Content(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw = "rules:\n - DOMAIN,rejected.example,DIRECT\n - MATCH,DIRECT\n"
	if err := a.refreshManagedProfile(p.ID); err == nil {
		t.Fatal("rejected candidate accepted")
	}
	after, err := a.store.Content(p.ID)
	if err != nil || string(after) != string(before) {
		t.Fatal("rejected source replaced cache")
	}
	if !a.store.List()[0].Updated.Equal(p.Updated) {
		t.Fatal("failed refresh advanced update time")
	}
	requests := reloads()
	if len(requests) != 2 || !strings.Contains(requests[0], "rejected.example") || !strings.Contains(requests[1], "original.example") {
		t.Fatalf("original runtime not restored: %v", requests)
	}
}
