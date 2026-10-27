package app

import (
	"encoding/json"
	"fmt"
	"image/png"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gopurr/internal/core"
	"gopurr/internal/update"
)

// TestRenderPages draws every page offscreen. It is the repeatable version of
// "does the UI still build", and it needs no window server.
func TestRenderPages(t *testing.T) {
	previousVersion := mygo.App.Version()
	mygo.App.SetVersion("0.1.0")
	t.Cleanup(func() { mygo.App.SetVersion(previousVersion) })
	dir := t.TempDir()
	t.Setenv("GOPURR_HOME", dir)

	a := New()
	// Fixtures use only the fake controller and never query or modify OS proxy
	// settings. Background sampling is unnecessary for deterministic images.
	a.booted, a.ticking = true, true
	// A fake controller on the socket the app expects, so the pages render with
	// the same data they would show against a real kernel.
	if secret, err := os.ReadFile(filepath.Join(dir, "core.secret")); err == nil {
		startFakeCore(t, dir, string(secret))
		a.k.WatchLogs()
		time.Sleep(250 * time.Millisecond)

		a.bmu.Lock()
		a.delays = map[string]uint16{"香港 01": 42, "日本 02": 138, "新加坡 03": 410, "美国 04": 0}
		a.bmu.Unlock()

		// A local profile, so the subscriptions page shows both entry points.
		local := filepath.Join(dir, "local-demo.yaml")
		if err := os.WriteFile(local, []byte("mixed-port: 7890\nrules:\n  - MATCH,DIRECT\n"), 0o644); err == nil {
			_, _ = a.store.AddFile(local, "本地演示配置")
		}
	}
	outDir := filepath.Join("..", "..", "docs")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	type shot struct {
		name  string
		setup func()
	}
	shots := []shot{
		{"software-update", func() { a.page = "settings"; a.settingsTab = 3; fixtureSoftwareUpdate(t, a, update.Available) }},
		{"software-update-downloading", func() { a.page = "settings"; a.settingsTab = 3; fixtureSoftwareUpdate(t, a, update.Downloading) }},
		{"software-update-ready", func() { a.page = "settings"; a.settingsTab = 3; fixtureSoftwareUpdate(t, a, update.Ready) }},
		{"software-update-error", func() { a.page = "settings"; a.settingsTab = 3; fixtureSoftwareUpdate(t, a, update.Failed) }},
		{"overview", func() { a.page = "overview" }},
		{"subs", func() { a.page = "subs" }},
		{"subs-compact", func() { a.page = "subs" }},
		{"proxies", func() { a.page = "proxies"; a.proxyGroup = "节点选择" }},
		{"proxies-hover", func() { a.page = "proxies"; a.proxyGroup = "节点选择" }},
		{"proxies-testing", func() {
			a.page = "proxies"
			a.bmu.Lock()
			a.testing, a.testDone, a.testTotal, a.testOK = "全部节点", 36, 184, 34
			a.bmu.Unlock()
		}},
		{"proxies-compact", func() { a.page = "proxies"; a.proxyGroup = "节点选择" }},
		{"conns", func() { a.page = "conns" }},
		{"rules", func() { a.page = "rules" }},
		{"rules-compact", func() { a.page = "rules" }},
		{"rules-filtered", func() {
			a.page = "rules"
			a.ruleQuery = "cn"
			a.ruleAdapter = "DIRECT"
		}},
		{"extra-rules", func() {
			a.page, a.extensionTab = "rules", 1
			a.initExtensionDrafts()
			a.extraRulesDraft = "DOMAIN-SUFFIX,example.com,DIRECT\nDOMAIN-SUFFIX,ads.example,REJECT\nIP-CIDR,192.168.0.0/16,DIRECT,no-resolve"
		}},
		{"extra-rules-compact", func() { a.page, a.extensionTab = "rules", 1 }},
		{"yaml-override", func() {
			a.page, a.extensionTab = "rules", 2
			a.mergeYAMLDraft = "# 为所有订阅开启并发连接\ntcp-concurrent: true\n\nprofile:\n  store-selected: true\n"
		}},
		{"logs", func() {
			a.page = "logs"
		}},
		{"logs-compact", func() { a.page = "logs" }},
		{"logs-paused", func() {
			a.page = "logs"
			a.logLevelIdx = 2
			a.logPaused = true
			a.logFrozen = a.k.Logs()
		}},
		{"settings", func() { a.page, a.settingsTab = "settings", 0 }},
		{"settings-dock-hidden", func() {
			a.page, a.settingsTab = "settings", 0
			if err := a.changePrefs(func(p *Prefs) { p.DockVisibility = DockHidden }); err != nil {
				t.Fatal(err)
			}
		}},
		{"settings-dock-follow-window", func() {
			a.page, a.settingsTab = "settings", 0
			if err := a.changePrefs(func(p *Prefs) { p.DockVisibility = DockFollowWindow }); err != nil {
				t.Fatal(err)
			}
		}},
		{"settings-error", func() {
			a.page, a.settingsTab = "settings", 0
			a.generalSettingsError = "设置保存失败：数据目录暂时不可写，请检查权限后重试。"
		}},
		{"settings-network", func() { a.page, a.settingsTab = "settings", 1 }},
		{"settings-network-compact", func() { a.page, a.settingsTab = "settings", 1 }},
		{"settings-testing", func() { a.page, a.settingsTab = "settings", 2 }},
		{"settings-maintenance", func() { a.page, a.settingsTab = "settings", 3 }},
		{"settings-maintenance-compact", func() { a.page, a.settingsTab = "settings", 3 }},
		{"settings-compact", func() { a.page, a.settingsTab = "settings", 2 }},
		{"conns-compact", func() { a.page = "conns" }},
		{"overview-live", func() {
			a.page = "overview"
			a.mu.Lock()
			a.hist = nil
			for i := 0; i < bars; i++ {
				base := float64(1 + 0.9*math.Sin(float64(i)/6))
				a.hist = append(a.hist, sample{
					down: int64(base * 900 * 1024),
					up:   int64(base * 90 * 1024),
				})
			}
			a.last = core.Sample{DownNow: 900 * 1024, UpNow: 90 * 1024, DownTotal: 812 << 20, UpTotal: 61 << 20}
			a.mu.Unlock()
		}},
		{"overview-compact", func() { a.page = "overview" }},
		{"install-dialog", func() { a.page = "overview"; a.confirmInstall = true }},
		{"uninstall-dialog", func() { a.page = "overview"; a.confirmUninstall = true }},
		{"delete-dialog", func() {
			a.page, a.showDeleteSub = "subs", true
			a.deleteSubName = "工作与家庭共用的远程订阅配置 · 长名称示例"
		}},
		{"yaml-error", func() {
			a.page, a.extensionTab = "rules", 2
			a.mergeYAMLError = "YAML 格式错误：请检查缩进，修正后重新保存。"
		}},
		{"raw-config", func() {
			a.page = "settings"
			a.showRaw = true
			a.rawReadOnly = true
			a.rawDraft = "# Gopurr runtime config\nmixed-port: 7890\nmode: rule\nproxies: []\nproxy-groups:\n  - {name: 节点选择, type: select, proxies: [DIRECT]}\nrules:\n  - MATCH,节点选择\n"
		}},
		{"raw-config-error", func() {
			a.page, a.showRaw, a.rawReadOnly = "settings", true, false
			a.rawDraft = "mixed-port: 7890\nmode: rule\nrules:\n  - MATCH,DIRECT\n"
			a.rawError = "配置校验失败：请检查规则与代理组名称后重新保存，原有运行配置已保留。"
		}},
	}

	for _, appearance := range []string{AppearanceDark, AppearanceLight} {
		if err := a.changePrefs(func(p *Prefs) { p.Appearance = appearance }); err != nil {
			t.Fatal(err)
		}
		a.settingsReady = false
		a.mu.Lock()
		a.hist, a.last = nil, core.Sample{}
		a.mu.Unlock()
		for _, shot := range shots {
			// Every fixture is checked at both sizes; legacy compact entries are
			// covered by the matching full-size fixture below.
			if strings.HasSuffix(shot.name, "-compact") {
				continue
			}
			for _, size := range [][2]int{{1060, 720}, {840, 560}} {
				page := shot.name
				a.bmu.Lock()
				a.testing = ""
				a.bmu.Unlock()
				a.confirmInstall, a.confirmUninstall, a.showDeleteSub, a.showRaw = false, false, false, false
				a.logPaused, a.logLevelIdx, a.extensionTab = false, 0, 0
				a.ruleQuery, a.ruleAdapter = "", allAdapters
				a.generalSettingsError, a.mergeYAMLError = "", ""
				a.rawError = ""
				if a.softwareUpdates != nil {
					a.softwareUpdates.Hide()
				}
				if a.Prefs().DockVisibility != DockShown {
					if err := a.changePrefs(func(p *Prefs) { p.DockVisibility = DockShown }); err != nil {
						t.Fatal(err)
					}
				}
				shot.setup()
				width, height := size[0], size[1]
				if width == 840 {
					page += "-compact"
				}
				if appearance == AppearanceLight {
					page = "light-" + page
				}
				tr := ui.NewTester(a.View, width, height)
				tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 88})
				tr.SetDark(appearance == AppearanceDark)
				tr.Advance(200 * time.Millisecond)
				tr.Frame()
				if shot.name == "proxies-hover" {
					if r, ok := tr.Find("测试 香港 01"); ok {
						tr.Move(r.X+r.W/2, r.Y+r.H/2)
					}
				}
				if strings.HasSuffix(shot.name, "-error") && shot.name != "raw-config-error" {
					tr.Scroll(700, 420, 0, 1000)
				}
				img := tr.Image()
				if img == nil {
					t.Fatalf("%s: nil image", page)
				}

				colors := map[uint32]int{}
				b := img.Bounds()
				for y := b.Min.Y; y < b.Max.Y; y += 7 {
					for x := b.Min.X; x < b.Max.X; x += 7 {
						r, g, bl, _ := img.At(x, y).RGBA()
						colors[uint32(r>>8)<<16|uint32(g>>8)<<8|uint32(bl>>8)]++
					}
				}
				if len(colors) < 5 {
					t.Fatalf("%s: looks blank (%d colours)", page, len(colors))
				}

				f, err := os.Create(filepath.Join(outDir, "render-"+page+".png"))
				if err != nil {
					t.Fatal(err)
				}
				if err := png.Encode(f, img); err != nil {
					t.Fatal(err)
				}
				f.Close()
			}
		}
	}
}

func TestProxyPageFitsMinimumWindow(t *testing.T) {
	dir, err := os.MkdirTemp("", "gopurr-ui-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("GOPURR_HOME", dir)
	a := New()
	secret, err := os.ReadFile(filepath.Join(dir, "core.secret"))
	if err != nil {
		t.Fatal(err)
	}
	startFakeCore(t, dir, string(secret))
	a.page, a.proxyGroup = "proxies", "节点选择"
	a.booted, a.ticking = true, true
	tr := ui.NewTester(a.View, 840, 560)
	tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 88})
	tr.Advance(200 * time.Millisecond)
	tr.Frame()
	r, ok := tr.Find("节点列表统计")
	if !ok || r.Y+r.H > 544 {
		t.Fatalf("node list footer is outside minimum window: %+v", r)
	}
	for _, label := range []string{"测试全部", "测试本组", "香港 01"} {
		r, ok := tr.Find(label)
		if !ok || r.X < 190 || r.X+r.W > 820 || r.Y+r.H > 544 {
			t.Fatalf("control %q outside content: %+v", label, r)
		}
	}
}

// startFakeCore serves the handful of mihomo endpoints the UI reads.
func startFakeCore(t *testing.T, dir, secret string) {
	t.Helper()
	sock := filepath.Join(dir, "core.sock")
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}

	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"1.19.33","meta":true}`))
	})
	mux.HandleFunc("/proxies", func(w http.ResponseWriter, r *http.Request) {
		nodes := []string{"香港 01", "日本 02", "新加坡 03", "美国 04"}
		proxies := map[string]any{
			"自动选择":   map[string]any{"type": "URLTest", "now": "日本 02", "all": nodes[:3]},
			"DIRECT": map[string]any{"type": "Direct"},
			"REJECT": map[string]any{"type": "Reject"},
		}
		regions := []string{"香港", "日本", "新加坡", "台湾", "美国", "德国"}
		for i := 0; i < 180; i++ {
			nodes = append(nodes, fmt.Sprintf("%s %02d · 高速专线", regions[i%len(regions)], i+5))
		}
		for _, node := range nodes {
			proxies[node] = map[string]any{"type": "Shadowsocks"}
		}
		proxies["节点选择"] = map[string]any{"type": "Selector", "now": "香港 01", "all": append(append([]string{}, nodes...), "DIRECT")}
		proxies["GLOBAL"] = map[string]any{"type": "Selector", "now": "节点选择", "all": append([]string{"DIRECT", "REJECT", "节点选择", "自动选择"}, nodes...)}
		_ = json.NewEncoder(w).Encode(map[string]any{"proxies": proxies})
	})
	mux.HandleFunc("/proxies/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"mixed-port":7890,"mode":"rule"}`))
	})
	mux.HandleFunc("/rules", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"rules":[
			{"index":0,"type":"DomainSuffix","payload":"local","proxy":"DIRECT"},
			{"index":1,"type":"DomainSuffix","payload":"github.com","proxy":"节点选择"},
			{"index":2,"type":"GeoSite","payload":"cn","proxy":"DIRECT"},
			{"index":3,"type":"Match","payload":"","proxy":"节点选择"}
		]}`))
	})
	mux.HandleFunc("/connections", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"downloadTotal":91234567,"uploadTotal":1234567,"connections":[
			{"metadata":{"host":"api.github.com","network":"tcp"},"rule":"DomainSuffix","chains":["节点选择","香港 01"],"upload":12034,"download":884422,"start":"2026-10-27T13:00:00+08:00"},
			{"metadata":{"host":"www.google.com","network":"tcp"},"rule":"Match","chains":["节点选择","香港 01"],"upload":3200,"download":55211,"start":"2026-10-27T13:00:02+08:00"},
			{"metadata":{"host":"registry.npmjs.org","network":"tcp"},"rule":"Match","chains":["节点选择","日本 02"],"upload":900,"download":2345111,"start":"2026-10-27T13:00:04+08:00"},
			{"metadata":{"host":"a-very-long-service-name.api.production.example.internal.example.com","network":"tcp"},"rule":"DomainSuffix","chains":["手动选择 · 工作与家庭共用的长名称策略组","香港 01 · 低延迟优选线路"],"upload":1234567,"download":98765432,"start":"2026-10-27T13:00:05+08:00"}
		]}`))
	})
	mux.HandleFunc("/traffic", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		for i := 0; i < 400; i++ {
			_, _ = w.Write([]byte(`{"up":92160,"down":921600,"upTotal":1234567,"downTotal":91234567}` + "\n"))
			flusher.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(time.Second):
			}
		}
	})
	mux.HandleFunc("/logs", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		flusher, _ := w.(http.Flusher)
		for _, line := range []string{
			`{"type":"info","payload":"Mixed(http+socks) proxy listening at: 127.0.0.1:7890"}`,
			`{"type":"info","payload":"[TCP] 127.0.0.1:5412 --> api.github.com:443 match DomainSuffix(github.com) using 节点选择[香港 01]"}`,
			`{"type":"warning","payload":"[TCP] dial 美国 04 error: connection refused"}`,
			`{"type":"debug","payload":"[DNS] resolve api.github.com from udp://114.114.114.114:53"}`,
		} {
			_, _ = w.Write([]byte(line + "\n"))
			flusher.Flush()
		}
		<-r.Context().Done()
	})

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
}
