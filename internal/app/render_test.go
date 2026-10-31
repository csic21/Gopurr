package app

import (
	"encoding/json"
	"fmt"
	"image/png"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"gopurr/internal/applications"
	"gopurr/internal/backup"
	"gopurr/internal/core"
	"gopurr/internal/diagnostic"
	"gopurr/internal/mesh"
	"gopurr/internal/patch"
	"gopurr/internal/profile"
	"gopurr/internal/update"
)

// TestRenderPages draws every page offscreen. It is the repeatable version of
// "does the UI still build", and it needs no window server.
func TestRenderPages(t *testing.T) {
	text := tr
	t.Cleanup(func() { i18n.SetLocaleFunc(func() string { return LanguageChinese }) })
	previousVersion := mygo.App.Version()
	configRaw, err := os.ReadFile(filepath.Join("..", "..", "mygo.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct{ Version string }
	if err := json.Unmarshal(configRaw, &config); err != nil || config.Version == "" {
		t.Fatalf("invalid app version in render fixture: %v", err)
	}
	mygo.App.SetVersion(config.Version)
	t.Cleanup(func() { mygo.App.SetVersion(previousVersion) })
	dir := t.TempDir()
	t.Setenv("GOPURR_HOME", dir)

	a := New()
	// Fixtures use only the fake controller and never query or modify OS proxy
	// settings. Background sampling is unnecessary for deterministic images.
	a.booted, a.ticking = true, true
	a.applyAutoLaunch = func(bool) error { return nil }
	// A fake controller on the socket the app expects, so the pages render with
	// the same data they would show against a real kernel.
	if secret, err := os.ReadFile(filepath.Join(dir, "core.secret")); err == nil {
		startFakeCore(t, dir, string(secret))
		a.k.WatchLogs()
		time.Sleep(250 * time.Millisecond)

		a.bmu.Lock()
		a.delays = map[string]uint16{"香港 01": 42, "日本 02": 138, "新加坡 03": 410, "美国 04": 0}
		a.bmu.Unlock()

		// Local and remote metadata cover active, quota and switchable profiles.
		// The remote entry is a fixture only; no public subscription is fetched.
		local := filepath.Join(dir, "local-demo.yaml")
		if err := os.WriteFile(local, []byte("mixed-port: 7890\nrules:\n  - MATCH,DIRECT\n"), 0o644); err == nil {
			_, _ = a.store.AddFile(local, "本地演示配置")
		}
		a.store.Items[0].Updated = time.Date(2026, 10, 28, 9, 30, 0, 0, time.Local)
		a.store.Items = append(a.store.Items, profile.Profile{
			ID: "remote-demo", Name: "日常连接", Kind: profile.KindURL,
			URL: "https://subscription.example.com/api/v1/subscribe?token=fixture", File: "remote-demo.yaml",
			Updated: time.Date(2026, 10, 28, 10, 20, 0, 0, time.Local),
			Info:    profile.Info{Download: 24 << 30, Upload: 3 << 30, Total: 200 << 30, Expire: time.Date(2026, 12, 1, 0, 0, 0, 0, time.Local).Unix()},
		})
		if err := os.WriteFile(filepath.Join(dir, "profiles", "remote-demo.yaml"), []byte("mixed-port: 7890\nrules:\n  - MATCH,DIRECT\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		a.store.Active = "remote-demo"
	}
	profiles, activeProfile := a.store.List(), a.store.ActiveID()
	outDir := filepath.Join("..", "..", "docs")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	type shot struct {
		name  string
		setup func()
	}
	shots := []shot{
		{"aggregation-sources", func() { fixtureAggregation(t, a, 0) }},
		{"aggregation-filters", func() { fixtureAggregation(t, a, 1) }},
		{"aggregation-preview", func() { fixtureAggregation(t, a, 2); a.previewAggregation() }},
		{"aggregation-error", func() {
			fixtureAggregation(t, a, 1)
			a.aggregationError = text("筛选后没有节点，请调整聚合来源或筛选条件")
		}},
		{"proxy-editor-nodes", func() { fixtureProxyEditor(t, a); a.proxyEditorNodes["香港 01"].Name = "香港 01 · 工作线路" }},
		{"proxy-editor-groups", func() { fixtureProxyEditor(t, a); a.proxyEditorTab = 1 }},
		{"proxy-editor-new-group", func() {
			fixtureProxyEditor(t, a)
			a.addProxyEditorGroup()
			a.proxyEditorGroups[a.proxyEditorGroup].Name = "工作出口"
		}},
		{"proxy-editor-error", func() {
			fixtureProxyEditor(t, a)
			a.proxyEditorNodes["香港 01"].Port = "70000"
			a.proxyEditorError = text("节点端口须为 1–65535")
		}},
		{"override-library", func() { fixtureOverrideLibrary(t, a, 0) }},
		{"override-library-order", func() { fixtureOverrideLibrary(t, a, 1) }},
		{"override-library-error", func() {
			fixtureOverrideLibrary(t, a, 0)
			a.libraryError = text("所选覆写模板已不存在，请重新选择")
		}},
		{"settings-shortcuts", func() { a.page, a.settingsTab = "settings", 0; a.openShortcutSettings() }},
		{"settings-shortcuts-configured", func() {
			a.page, a.settingsTab = "settings", 0
			a.openShortcutSettings()
			a.shortcutDraft = [6]string{"Ctrl+Alt+Space", "Ctrl+Alt+P", "Ctrl+Alt+T", "Ctrl+Alt+1", "Ctrl+Alt+2", "Ctrl+Alt+3"}
		}},
		{"settings-shortcuts-error", func() {
			a.page, a.settingsTab = "settings", 0
			a.openShortcutSettings()
			a.shortcutDraft[0] = "Ctrl+Alt+P"
			a.shortcutError = text("快捷键无法启用，请检查组合是否被占用：Ctrl+Alt+P")
		}},
		{"subs-import-link", func() {
			a.OpenSubscriptionURL("gopurr://install-config?url=https%3A%2F%2Fsubscription.example.com%2Fsubscribe&name=Imported%20profile")
		}},
		{"profile-rules", func() { fixtureProfileEditor(a, 1) }},
		{"profile-rules-error", func() {
			fixtureProfileEditor(a, 1)
			a.extraRulesError = text("第 1 条额外规则无效：目标策略“工作出口”不存在，请选择当前订阅中的策略组。")
		}},
		{"profile-yaml", func() { fixtureProfileEditor(a, 2) }},
		{"profile-dns", func() { fixtureProfileEditor(a, 3) }},
		{"profile-dns-servers", func() { fixtureProfileEditor(a, 3) }},
		{"profile-preview", func() { fixtureProfileEditor(a, 2); a.previewExtensionConfig() }},
		{"connection-rule", func() {
			a.page = "conns"
			a.openConnectionRule(core.Conn{ID: "demo", Host: "api.example.com", Process: "Browser", DestinationIP: "203.0.113.12"})
		}},
		{"connection-rule-preview", func() {
			a.page = "conns"
			a.openConnectionRule(core.Conn{ID: "demo", Host: "api.example.com", Process: "Browser", DestinationIP: "203.0.113.12"})
			a.connRuleTarget, a.connRuleDisconnect = "REJECT", true
		}},
		{"connection-rule-error", func() {
			a.page = "conns"
			a.openConnectionRule(core.Conn{Host: "api.example.com", Process: "Browser"})
			a.connRuleError = text("内核未接受配置，原有规则和连接已保留，请检查目标策略。")
		}},
		{"diagnostics", func() { fixtureDiagnostics(a, "idle") }},
		{"diagnostics-running", func() { fixtureDiagnostics(a, "running") }},
		{"diagnostics-result", func() { fixtureDiagnostics(a, "result") }},
		{"diagnostics-result-bottom", func() { fixtureDiagnostics(a, "result") }},
		{"diagnostics-canceled", func() { fixtureDiagnostics(a, "canceled") }},
		{"software-update", func() { a.page = "settings"; a.settingsTab = 3; fixtureSoftwareUpdate(t, a, update.Available) }},
		{"software-update-downloading", func() { a.page = "settings"; a.settingsTab = 3; fixtureSoftwareUpdate(t, a, update.Downloading) }},
		{"software-update-ready", func() { a.page = "settings"; a.settingsTab = 3; fixtureSoftwareUpdate(t, a, update.Ready) }},
		{"software-update-restarting", func() {
			a.page = "overview"
			a.shutdown.relaunch = true
			a.shutdownPending.Store(true)
		}},
		{"software-update-restart-error", func() {
			a.page = "overview"
			a.shutdown.relaunch = true
			a.shutdown.err = "无法恢复系统代理，应用保持运行: 等待系统代理恢复操作超时，请稍后重试: context deadline exceeded"
		}},
		{"software-update-error", func() { a.page = "settings"; a.settingsTab = 3; fixtureSoftwareUpdate(t, a, update.Failed) }},
		{"overview", func() { a.page = "overview" }},
		{"overview-live", func() { a.page = "overview"; fixtureOverviewTraffic(a) }},
		{"overview-idle", func() {
			a.page = "overview"
			a.mu.Lock()
			a.hist, a.last = make([]sample, bars), core.Sample{}
			a.mu.Unlock()
		}},
		{"overview-service-update", func() { a.page, a.svcInstalled = "overview", true }},
		{"overview-global", func() { a.page, a.modeToken, a.modeIdx = "overview", "global", 1; fixtureOverviewTraffic(a) }},
		{"overview-direct", func() { a.page, a.modeToken, a.modeIdx = "overview", "direct", 2 }},
		{"overview-long-exit", func() {
			a.page, a.proxyGroup = "overview", "漏网之鱼 · 工作与家庭共用的长名称策略组"
		}},
		{"overview-picker", func() { a.page, a.showOverviewNodes = "overview", true }},
		{"overview-picker-auto", func() { a.page, a.proxyGroup, a.showOverviewNodes = "overview", "自动选择", true }},
		{"overview-picker-empty", func() { a.page, a.showOverviewNodes, a.overviewQuery = "overview", true, "no-matching-node" }},
		{"overview-picker-error", func() {
			a.page, a.showOverviewNodes = "overview", true
			a.notice = "切换节点失败：内核暂时无法响应，请稍后重试。"
		}},
		{"overview-features", func() { a.page = "overview" }},
		{"overview-proxy-error", func() {
			a.page = "overview"
			a.sysApplied, a.sysOn = true, true
			a.setSysErr(text("恢复系统代理失败，应用保持运行。请重新开启系统代理后重试。"))
		}},
		{"subs", func() { a.page = "subs" }},
		{"subs-empty", func() { a.page, a.store.Items, a.store.Active = "subs", nil, "" }},
		{"subs-add", func() { a.page, a.showAddSub = "subs", true }},
		{"subs-add-error", func() {
			a.page, a.showAddSub = "subs", true
			a.subURL = "https://subscription.example.com/subscribe"
			a.setSubErr("订阅下载失败：服务器暂时无法访问，请检查链接后重试。")
		}},
		{"subs-long-name", func() {
			a.page = "subs"
			a.store.Items[0].Name = "工作与家庭共用的远程订阅配置 · 自动更新与备用线路 · 长名称示例"
		}},
		{"subs-compact", func() { a.page = "subs" }},
		{"proxies", func() { a.page = "proxies"; a.proxyGroup = "节点选择" }},
		{"proxies-no-results", func() { a.page, a.proxyQuery, a.proxyOnlyLive = "proxies", "no-matching-node", true }},
		{"proxies-hover", func() { a.page = "proxies"; a.proxyGroup = "节点选择" }},
		{"proxies-groups-scrolled", func() {
			a.page, a.proxyGroup = "proxies", "漏网之鱼 · 工作与家庭共用的长名称策略组"
		}},
		{"proxies-testing", func() {
			a.page, a.proxyGroup = "proxies", "节点选择"
			a.bmu.Lock()
			a.testing, a.testDone, a.testTotal, a.testOK = "全部节点", 36, 184, 34
			a.bmu.Unlock()
		}},
		{"proxies-compact", func() { a.page = "proxies"; a.proxyGroup = "节点选择" }},
		{"conns", func() { a.page = "conns" }},
		{"mesh", func() { fixtureMesh(a) }},
		{"mesh-offline-empty", func() {
			f := fixtureMesh(a)
			f.snap.Status = mesh.Status{Installed: true, Official: true, ShieldsKnown: true, ShieldsUp: true, State: "Stopped"}
		}},
		{"mesh-empty", func() {
			f := fixtureMesh(a)
			f.snap.Status.Self, f.snap.Status.Peers = mesh.Device{}, nil
		}},
		{"mesh-no-results", func() { fixtureMesh(a); a.meshQuery = "不存在的设备" }},
		{"mesh-visits-empty", func() { f := fixtureMesh(a); a.meshTab = 1; f.snap.Visits = nil }},
		{"mesh-login-empty", func() {
			f := fixtureMesh(a)
			f.snap.Status.Self, f.snap.Status.Peers = mesh.Device{}, nil
			f.snap.Status.Running, f.snap.Status.State = false, "NeedsLogin"
			f.snap.Status.AuthURL = "https://login.tailscale.com/a/render-fixture"
		}},
		{"mesh-invite", func() { fixtureMesh(a); a.meshInvite = true }},
		{"mesh-invite-long-network", func() {
			f := fixtureMesh(a)
			f.snap.Status.Tailnet = "工作与家庭共用的协作网络 · 设计开发与远程维护 · 很长的网络名称@example.com"
			a.meshInvite = true
		}},
		{"mesh-invite-offline", func() {
			f := fixtureMesh(a)
			f.snap.Status = mesh.Status{Installed: true, Official: true, ShieldsKnown: true, ShieldsUp: true, State: "Stopped"}
			a.meshInvite = true
		}},
		{"mesh-invite-browser-error", func() {
			fixtureMesh(a)
			a.meshInvite = true
			a.meshInviteError = "未能打开浏览器，请复制控制台地址后手动访问。"
		}},
		{"mesh-open", func() { f := fixtureMesh(a); f.snap.Status.ShieldsUp = false }},
		{"mesh-visits", func() { f := fixtureMesh(a); a.meshTab = 1; f.snap.Status.ShieldsUp = false }},
		{"mesh-monitor-error", func() {
			f := fixtureMesh(a)
			a.meshTab = 1
			f.snap.MonitorError = "内置组网访问记录暂不可用，请刷新或断开组网"
		}},
		{"mesh-monitor-scope", func() {
			f := fixtureMesh(a)
			a.meshTab = 1
			f.snap.Status.ShieldsUp = false
			f.snap.MonitorScope = "内置组网实际接收的 TCP / UDP 服务连接；账号来自加密网络的设备身份"
		}},
		{"mesh-security", func() { fixtureMesh(a); a.meshTab = 2 }},
		{"mesh-security-bottom", func() { fixtureMesh(a); a.meshTab = 2 }},
		{"mesh-setup", func() { f := fixtureMesh(a); f.snap.Status = mesh.Status{} }},
		{"mesh-login", func() {
			f := fixtureMesh(a)
			f.snap.Status.Running = false
			f.snap.Status.State = "NeedsLogin"
			f.snap.Status.AuthURL = "https://login.tailscale.com/a/render-fixture"
		}},
		{"mesh-error", func() {
			f := fixtureMesh(a)
			f.snap.Status.ShieldsKnown = false
			f.snap.Error = "无法确认内置组网防护状态，请断开后重试"
		}},
		{"mesh-detail", func() { f := fixtureMesh(a); a.meshShowDevice = true; a.meshSelected = f.snap.Status.Peers[5] }},
		{"mesh-allow-dialog", func() { fixtureMesh(a); a.meshConfirmAllow = true; a.meshServicesDraft = "22/tcp, 3389/tcp" }},
		{"mesh-ports-dialog", func() {
			fixtureMesh(a)
			a.meshEditServices = true
			a.meshAdvancedServices = true
			a.meshServicesDraft = "22/tcp, 3389/tcp, 5353/udp"
		}},
		{"mesh-blocked-device", func() {
			f := fixtureMesh(a)
			a.meshShowDevice = true
			a.meshSelected = f.snap.Status.Peers[4]
			f.snap.Status.Blocked = []string{a.meshSelected.ID}
		}},
		{"mesh-connect-dialog", func() { fixtureMesh(a); a.meshGuide = true }},
		{"mesh-guide-first", func() {
			f := fixtureMesh(a)
			f.snap.Status.Running = false
			f.snap.Status.State = "Stopped"
			a.meshGuide = true
		}},
		{"mesh-guide-ready", func() { f := fixtureMesh(a); meshTestPrepared(a); f.snap.AutoConnect = true; a.meshGuide = true }},
		{"mesh-guide-connecting", func() {
			f := fixtureMesh(a)
			f.snap.Status.Running, f.snap.Status.State = false, "NeedsLogin"
			f.snap.Busy, f.snap.Action = true, "connect"
			a.meshGuide = true
		}},
		{"mesh-guide-timeout", func() {
			f := fixtureMesh(a)
			f.snap.Status.Running, f.snap.Status.State = false, "Stopped"
			f.snap.Error = "连接 Tailscale 超时，请检查网络或代理设置后重试"
			a.meshGuide = true
		}},
		{"mesh-guide-error", func() {
			f := fixtureMesh(a)
			f.snap.Error = "连接失败，请检查网络后重试"
			a.meshGuide = true
		}},
		{"mesh-guide-services", func() { f := fixtureMesh(a); f.snap.Status.ShieldsUp = false; a.meshGuide = true }},
		{"mesh-quick-ssh", func() {
			f := fixtureMesh(a)
			meshTestPrepared(a)
			a.openMeshQuick(f.snap.Status.Peers[2])
			a.meshQuickUser = "ubuntu"
		}},
		{"mesh-quick-rdp", func() { f := fixtureMesh(a); meshTestPrepared(a); a.openMeshQuick(f.snap.Status.Peers[1]) }},
		{"mesh-quick-vnc", func() { f := fixtureMesh(a); meshTestPrepared(a); a.openMeshQuick(f.snap.Status.Peers[0]) }},
		{"mesh-quick-unprepared", func() { f := fixtureMesh(a); a.openMeshQuick(f.snap.Status.Peers[2]) }},
		{"mesh-quick-checked", func() {
			f := fixtureMesh(a)
			meshTestPrepared(a)
			a.openMeshQuick(f.snap.Status.Peers[2])
			a.meshProbeResult = mesh.ProbeResult{State: "verified", Message: "SSH 服务有响应；登录仍由客户端验证"}
		}},
		{"mesh-quick-checking", func() {
			f := fixtureMesh(a)
			meshTestPrepared(a)
			a.openMeshQuick(f.snap.Status.Peers[2])
			a.meshProbeBusy = true
		}},
		{"mesh-quick-handshake", func() {
			f := fixtureMesh(a)
			meshTestPrepared(a)
			a.openMeshQuick(f.snap.Status.Peers[1])
			a.meshProbeResult = mesh.ProbeResult{State: "connected", Message: "TCP 端口已连接；尚未验证服务类型、授权或登录"}
		}},
		{"mesh-quick-error", func() {
			f := fixtureMesh(a)
			meshTestPrepared(a)
			a.openMeshQuick(f.snap.Status.Peers[1])
			a.meshQuickError = "未能打开客户端；请确认已安装对应客户端，也可复制地址手动连接"
		}},
		{"mesh-prepare-dialog", func() { fixtureMesh(a); a.meshPrepareConfirm = true }},
		{"mesh-prepare-error", func() { fixtureMesh(a); a.meshPrepareError = "系统授权被取消，请重试准备访问" }},
		{"mesh-recent", func() {
			f := fixtureMesh(a)
			meshTestPrepared(a)
			a.meshShortcutsReady = true
			a.meshShortcuts = map[string]mesh.Shortcut{f.snap.Status.Peers[2].ID: {Kind: "ssh", Port: 22, User: "ubuntu", LastUsed: time.Now()}, f.snap.Status.Peers[1].ID: {Kind: "rdp", Port: 3389, LastUsed: time.Now().Add(-time.Hour)}}
		}},
		{"mesh-working", func() { f := fixtureMesh(a); f.snap.Busy = true; f.snap.Action = "block" }},
		{"mesh-long-name", func() { fixtureMesh(a); a.meshQuery = "工作站" }},
		{"conns-filtered", func() { a.page = "conns"; a.refreshConnections(); a.connQuery = "Browser" }},
		{"conns-empty", func() { a.page = "conns"; a.connQuery = "no-match.example" }},
		{"connection-detail", func() { a.page = "conns"; a.refreshConnections(); a.connSelected = a.connRows[0]; a.showConn = true }},
		{"connection-error", func() {
			a.page = "conns"
			a.connError = text("读取连接失败：内核暂时无法响应，请稍后刷新。")
			a.connFetched = time.Now()
		}},
		{"disconnect-dialog", func() { a.page = "conns"; a.confirmCloseAll = true }},
		{"subscription-edit-top", func() {
			a.page = "subs"
			a.openSubscriptionEditor(profile.Profile{ID: "demo", Name: "工作与家庭共用的远程订阅配置 · 长名称示例", URL: "https://subscription.example.com/api/v1/client/subscribe?token=demo", UserAgent: "clash.meta/mihomo", UseProxy: true})
			a.editSubPolicy = 2
			a.editSubHours = "6"
		}}, {"subscription-edit", func() {
			a.page = "subs"
			a.openSubscriptionEditor(profile.Profile{ID: "demo", Name: "工作与家庭共用的远程订阅配置 · 长名称示例", URL: "https://subscription.example.com/api/v1/client/subscribe?token=demo", UserAgent: "clash.meta/mihomo", UseProxy: true})
			a.editSubPolicy = 2
			a.editSubHours = "6"
		}},
		{"subscription-edit-error", func() {
			a.page = "subs"
			a.openSubscriptionEditor(profile.Profile{ID: "demo", Name: "测试订阅", URL: "https://subscription.example.com"})
			a.editSubPolicy = 2
			a.editSubHours = "0"
			a.editSubError = text("更新间隔须为 1–8760 小时")
		}},
		{"backup-restore", func() {
			a.page = "settings"
			a.settingsTab = 3
			a.restoreName = "Gopurr-2026-10-27.zip"
			a.restoreCount = 3
			a.restorePreview = &backup.Snapshot{Version: 1, Created: time.Date(2026, 10, 27, 9, 30, 0, 0, time.Local)}
			a.showRestore = true
		}},
		{"backup-error", func() {
			a.page = "settings"
			a.settingsTab = 3
			a.backupError = text("备份文件损坏或版本不受支持，当前配置未更改。")
		}},
		{"settings-startup", func() { a.page = "settings"; a.settingsTab = 0; a.settingsReady = false }},
		{"settings-startup-enabled", func() {
			a.page = "settings"
			a.settingsTab = 0
			if err := a.changePrefs(func(p *Prefs) { p.AutoLaunch, p.SilentStart = true, true }); err != nil {
				t.Fatal(err)
			}
			a.settingsReady = false
		}},
		{"settings-cleanup", func() { a.page = "settings"; a.settingsTab = 1 }},

		{"rules", func() { a.page = "rules" }},
		{"rules-no-results", func() { a.page, a.ruleQuery = "rules", "no-matching-rule" }},
		{"rules-compact", func() { a.page = "rules" }},
		{"rules-filtered", func() {
			a.page = "rules"
			a.ruleQuery = "cn"
			a.ruleAdapter = "DIRECT"
		}},
		{"extra-rules", func() {
			a.page, a.extensionTab = "rules", 2
			a.initExtensionDrafts()
			a.extraRulesDraft = "DOMAIN-SUFFIX,example.com,DIRECT\nDOMAIN-SUFFIX,ads.example,REJECT\nIP-CIDR,192.168.0.0/16,DIRECT,no-resolve"
		}},
		{"extra-rules-compact", func() { a.page, a.extensionTab = "rules", 2 }},
		{"yaml-override", func() {
			a.page, a.extensionTab = "rules", 3
			a.mergeYAMLDraft = "# 为所有订阅开启并发连接\ntcp-concurrent: true\n\nprofile:\n  store-selected: true\n"
		}},
		{"application-rules", func() { fixtureApplicationRules(a) }},
		{"application-rules-empty", func() { fixtureApplicationRules(a); a.appRulesDraft = nil }},
		{"application-rules-no-results", func() { fixtureApplicationRules(a); a.appRulesQuery = "no-matching-app" }},
		{"application-rules-global", func() { fixtureApplicationRules(a); a.ov.Mode = "global" }},
		{"application-rules-error", func() {
			fixtureApplicationRules(a)
			a.appRulesError = "配置验证失败：代理组已被移除，请编辑应用分流并选择当前配置中的代理组。"
		}},
		{"application-picker", func() { fixtureApplicationRules(a); a.showAppPicker = true }},
		{"application-picker-search", func() { fixtureApplicationRules(a); a.showAppPicker = true; a.appCatalogQuery = "browser" }},
		{"application-picker-empty", func() { fixtureApplicationRules(a); a.showAppPicker = true; a.appCatalogQuery = "no-such-app" }},
		{"application-picker-loading", func() { fixtureApplicationRules(a); a.showAppPicker = true; a.appCatalog, a.appCatalogBusy = nil, true }},
		{"application-editor", func() {
			fixtureApplicationRules(a)
			a.openApplicationEditor(a.appCatalog[0])
			a.appEditGroups, a.appEditKind, a.appEditGroup = []string{"节点选择", "自动选择", "漏网之鱼 · 工作与家庭共用的长名称策略组"}, 2, "漏网之鱼 · 工作与家庭共用的长名称策略组"
		}},
		{"application-editor-single", func() {
			fixtureApplicationRules(a)
			a.openApplicationEditor(applications.Application{Name: "独立更新程序", Path: "/usr/local/bin/work-updater", Executable: "/usr/local/bin/work-updater"})
		}},
		{"application-editor-missing-group", func() {
			fixtureApplicationRules(a)
			a.openApplicationEditor(a.appCatalog[0])
			a.appEditGroups, a.appEditKind, a.appEditGroup = nil, 2, "已移除的代理组"
		}},
		{"logs", func() {
			a.page = "logs"
		}},
		{"logs-search", func() { a.page, a.logQuery = "logs", "TLS" }},
		{"logs-no-results", func() { a.page, a.logQuery, a.logLevelIdx = "logs", "no-matching-log", 2 }},
		{"logs-empty", func() { a.page, a.logPaused, a.logFrozen = "logs", true, []core.LogLine{} }},
		{"logs-compact", func() { a.page = "logs" }},
		{"logs-paused", func() {
			a.page = "logs"
			a.logLevelIdx = 2
			a.logPaused = true
			a.logFrozen = a.k.Logs()
		}},
		{"settings", func() { a.page, a.settingsTab = "settings", 0 }},
		{"settings-general-bottom", func() { a.page, a.settingsTab = "settings", 0 }},
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
			a.generalSettingsError = text("设置保存失败：数据目录暂时不可写，请检查权限后重试。")
		}},
		{"settings-network", func() { a.page, a.settingsTab = "settings", 1 }},
		{"settings-network-dirty", func() {
			a.page, a.settingsTab = "settings", 1
			a.initSettings()
			a.mixedPortDraft = "7891"
		}},
		{"settings-network-compact", func() { a.page, a.settingsTab = "settings", 1 }},
		{"settings-dns-follow", func() {
			a.page, a.settingsTab = "settings", 4
			a.dnsError, a.dnsStatus = "", ""
			a.resetDNSDraft()
		}},
		{"settings-dns", func() {
			a.page, a.settingsTab = "settings", 4
			a.resetDNSDraft()
			a.dnsDraft.Source, a.dnsDraft.Enable = 1, true
		}},
		{"settings-dns-servers", func() {
			a.page, a.settingsTab = "settings", 4
			a.resetDNSDraft()
			a.dnsDraft.Source, a.dnsDraft.Enable = 1, true
			a.dnsDraft.Servers = "https://resolver-with-a-long-hostname.example.com/dns-query#DIRECT\n1.1.1.1"
		}},
		{"settings-dns-lists", func() {
			a.page, a.settingsTab = "settings", 4
			a.resetDNSDraft()
			a.dnsDraft.Source, a.dnsDraft.Enable = 1, true
			a.dnsDraft.Mode = "fake-ip"
			a.dnsDraft.Servers = "https://resolver-with-a-long-hostname.example.com/dns-query#DIRECT\n1.1.1.1"
			a.dnsDraft.Filter = "*.lan\n+.local\n+.example.com"
		}},
		{"settings-dns-error", func() {
			a.page, a.settingsTab = "settings", 4
			a.resetDNSDraft()
			a.dnsDraft.Source, a.dnsDraft.Enable = 1, true
			a.dnsDraft.Bootstrap = "https://bootstrap.example.com/dns-query"
			a.dnsError = text("引导 DNS 的服务器必须使用 IP 地址或 system，请修改后重新保存。")
		}},
		{"settings-sniffer-follow", func() {
			a.page, a.settingsTab = "settings", 5
			a.snifferError, a.snifferStatus = "", ""
			a.resetSnifferDraft()
		}},
		{"settings-sniffer", func() {
			a.page, a.settingsTab = "settings", 5
			a.resetSnifferDraft()
			a.snifferDraft.Source, a.snifferDraft.Enable = 1, true
		}},
		{"settings-sniffer-ports", func() {
			a.page, a.settingsTab = "settings", 5
			a.resetSnifferDraft()
			a.snifferDraft.Source, a.snifferDraft.Enable = 1, true
		}},
		{"settings-sniffer-lists", func() {
			a.page, a.settingsTab = "settings", 5
			a.resetSnifferDraft()
			a.snifferDraft.Source, a.snifferDraft.Enable = 1, true
			a.snifferDraft.SkipDomain = "+.push.apple.com\n+.long-domain-for-compatibility-check.example.com"
			a.snifferDraft.SkipDst = "91.105.192.0/23\n91.108.4.0/22\n91.108.8.0/21"
		}},
		{"settings-testing", func() { a.page, a.settingsTab = "settings", 2 }},
		{"settings-maintenance", func() { a.page, a.settingsTab = "settings", 3 }},
		{"settings-maintenance-compact", func() { a.page, a.settingsTab = "settings", 3 }},
		{"settings-compact", func() { a.page, a.settingsTab = "settings", 2 }},
		{"conns-compact", func() { a.page = "conns" }},
		{"overview-live", func() {
			a.page = "overview"
			fixtureOverviewTraffic(a)
		}},
		{"overview-compact", func() { a.page = "overview" }},
		{"install-dialog", func() { a.page = "overview"; a.confirmInstall = true }},
		{"uninstall-dialog", func() { a.page = "overview"; a.confirmUninstall = true }},
		{"delete-dialog", func() {
			a.page, a.showDeleteSub = "subs", true
			a.deleteSubName = "工作与家庭共用的远程订阅配置 · 长名称示例"
		}},
		{"yaml-error", func() {
			a.page, a.extensionTab = "rules", 3
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
			a.rawError = text("配置校验失败：请检查规则与代理组名称后重新保存，原有运行配置已保留。")
		}},
	}

	for _, language := range []string{LanguageChinese, LanguageEnglish} {
		i18n.SetLocaleFunc(func() string { return language })
		for _, appearance := range []string{AppearanceDark, AppearanceLight} {
			if err := a.changePrefs(func(p *Prefs) { p.Appearance = appearance }); err != nil {
				t.Fatal(err)
			}
			a.settingsReady = false
			a.mu.Lock()
			a.hist, a.last = nil, core.Sample{}
			a.mu.Unlock()
			for _, collapsed := range []bool{false, true} {
				if err := a.changePrefs(func(p *Prefs) { p.SidebarCollapsed = collapsed }); err != nil {
					t.Fatal(err)
				}
				for _, shot := range shots {
					if only := os.Getenv("GOPURR_RENDER_ONLY"); only != "" && !strings.HasPrefix(shot.name, only) {
						continue
					}
					// Every fixture is checked at both sizes; legacy compact entries are
					// covered by the matching full-size fixture below.
					if strings.HasSuffix(shot.name, "-compact") {
						continue
					}
					for _, size := range [][2]int{{1060, 720}, {840, 560}} {
						page := shot.name
						a.shutdown = shutdownState{}
						a.shortcutState = shortcutState{}
						a.aggregationState, a.proxyEditorState, a.overrideLibraryState = aggregationState{}, proxyEditorState{}, overrideLibraryState{}
						a.ov.Library, a.ov.TemplateIDs = nil, nil
						a.shutdownPending.Store(false)
						a.bmu.Lock()
						a.testing = ""
						a.bmu.Unlock()
						a.confirmInstall, a.confirmUninstall, a.showDeleteSub, a.showRaw = false, false, false, false
						a.rawPreview, a.showConnRule = false, false
						a.extensionScope, a.extensionProfileID, a.extensionReady, a.extensionDrafts = 0, "", false, nil
						a.initExtensionDrafts()
						a.diagSteps, a.diagRunning, a.diagError, a.diagNotice = nil, false, "", ""
						a.logPaused, a.logLevelIdx, a.extensionTab = false, 0, 0
						a.applicationRulesState = applicationRulesState{}
						a.meshState = meshState{meshClient: &meshFixture{}}
						a.tunOn, a.ov.TunEnable, a.ov.TunAutoRout = false, false, true
						a.ov.AppRules, a.ov.Mode = nil, "rule"
						a.ruleQuery, a.ruleAdapter = "", allAdapters
						a.generalSettingsError, a.mergeYAMLError = "", ""
						a.dnsError, a.snifferError = "", ""
						a.rawError = ""
						a.showConn, a.confirmCloseAll, a.showEditSub, a.showRestore = false, false, false, false
						a.showOverviewNodes, a.showAddSub, a.subAddPending = false, false, false
						a.overviewQuery, a.logQuery, a.subURL, a.subName, a.notice = "", "", "", "", ""
						a.overviewList, a.ruleList, a.nodeList, a.connList = ui.ListState{}, ui.ListState{}, ui.ListState{}, ui.ListState{}
						a.logScroll = ui.ScrollState{}
						a.store.Items, a.store.Active = append([]profile.Profile(nil), profiles...), activeProfile
						a.modeToken, a.modeIdx, a.proxyGroup = "rule", 0, "节点选择"
						a.proxyQuery, a.proxySort, a.proxyOnlyLive = "", "订阅顺序", false
						a.setSubErr("")
						a.mu.Lock()
						a.hist, a.last = nil, core.Sample{}
						a.mu.Unlock()
						a.pmu.Lock()
						a.prefs.AutoLaunch, a.prefs.SilentStart = false, false
						a.pmu.Unlock()
						a.settingsReady = false
						a.featureState = featureState{}
						a.networkError, a.networkStatus, a.testSettingsError, a.testSettingsStatus = "", "", "", ""
						a.connQuery, a.connError, a.backupError, a.backupStatus = "", "", "", ""
						a.setSysErr("")
						a.sysApplied, a.sysOn = false, false
						a.svcInstalled = false
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
						if collapsed {
							page = "top-" + page
						}
						if appearance == AppearanceLight {
							page = "light-" + page
						}
						if language == LanguageEnglish {
							page = "en-" + page
						}
						tr := ui.NewTester(a.View, width, height)
						// Static fixtures show settled UI; motion tests cover animated frames.
						tr.SetPreferences(ui.Preferences{ReduceMotion: true})
						tr.SetLocale(language)
						tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 80})
						tr.SetDark(appearance == AppearanceDark)
						tr.Advance(400 * time.Millisecond)
						tr.Frame()
						if strings.HasPrefix(shot.name, "mesh") {
							controls := []string{"邀请成员", "刷新组网", "允许组网访问本机", "断开组网"}
							switch shot.name {
							case "mesh-offline-empty":
								controls = []string{"连接组网", "允许组网访问本机", "搜索组网设备", "连接组网，查看你的设备"}
							case "mesh-no-results":
								controls = append(controls, "没有匹配的设备", "清空搜索")
							case "mesh-invite", "mesh-invite-long-network", "mesh-invite-offline", "mesh-invite-browser-error":
								controls = []string{"复制控制台地址", "关闭", "打开邀请页面"}
							case "mesh-setup":
								controls = []string{"连接内置组网"}
							case "mesh-allow-dialog":
								controls = []string{"取消", "允许访问本机"}
							case "mesh-ports-dialog":
								controls = []string{"取消", "保存并关闭访问", "授权端口"}
							case "mesh-blocked-device":
								controls = []string{"解除本机封禁", "复制虚拟 IP", "完成"}
							case "mesh-connect-dialog":
								controls = []string{"稍后设置", "完成设置", "组网启动自动连接"}
							case "mesh-detail":
								controls = []string{"复制虚拟 IP", "在控制台管理", "完成"}
							case "mesh-guide-first", "mesh-guide-ready", "mesh-guide-error", "mesh-guide-services":
								controls = []string{"稍后设置", "完成设置", "组网启动自动连接"}
							case "mesh-guide-connecting":
								controls = []string{"正在连接…", "取消连接", "稍后设置", "完成设置", "组网启动自动连接"}
							case "mesh-guide-timeout":
								controls = []string{"连接并登录", "稍后设置", "完成设置", "组网启动自动连接"}
							case "mesh-quick-ssh", "mesh-quick-rdp", "mesh-quick-vnc", "mesh-quick-unprepared", "mesh-quick-checked", "mesh-quick-error", "mesh-quick-handshake":
								controls = []string{"关闭", "复制地址", "检测连接", "打开客户端"}
							case "mesh-quick-checking":
								controls = []string{"关闭", "取消检测", "打开客户端"}
							case "mesh-prepare-dialog":
								controls = []string{"取消", "安装并准备"}
							case "mesh-login", "mesh-login-empty":
								controls = append(controls, "继续登录 Tailscale")
							}
							for _, label := range controls {
								r, ok := tr.Find(text(label))
								if !ok || r.X < 0 || r.Y < 38 || r.X+r.W > float32(width) || r.Y+r.H > float32(height) {
									t.Fatalf("%s: mesh control %q missing or clipped: %+v", page, label, r)
								}
							}
						}
						if strings.HasPrefix(shot.name, "software-update-restart") {
							controls := []string{"正在重启…"}
							if shot.name == "software-update-restart-error" {
								controls = []string{"返回应用", "重试重启"}
							}
							for _, label := range controls {
								r, ok := tr.Find(text(label))
								if !ok || r.X < 0 || r.Y < 38 || r.X+r.W > float32(width) || r.Y+r.H > float32(height) {
									t.Fatalf("%s: control %q is missing or clipped: %+v", page, label, r)
								}
								if label == "重试重启" {
									tr.Move(r.X+r.W/2, r.Y+r.H/2)
								}
							}
						}
						if strings.HasPrefix(shot.name, "application-") {
							controls := []string{"添加应用…", "保存应用分流"}
							switch {
							case strings.HasPrefix(shot.name, "application-picker"):
								controls = []string{"搜索应用名称或路径…", "重新扫描", "选择文件…", "取消"}
							case strings.HasPrefix(shot.name, "application-editor"):
								controls = []string{"应用连接方式", "包含应用 Helper", "取消"}
								if shot.name == "application-editor-single" {
									controls = append(controls, "添加到草稿")
								} else {
									controls = append(controls, "更新草稿", "应用代理组")
								}
							}
							for _, label := range controls {
								r, ok := tr.Find(text(label))
								if !ok || r.X < 0 || r.Y < 38 || r.X+r.W > float32(width) || r.Y+r.H > float32(height) {
									t.Fatalf("%s: control %q is missing or clipped: %+v (found=%v)", page, label, r, ok)
								}
							}
							if shot.name == "application-picker" {
								if r, ok := tr.Find(text("选择应用 Mail")); ok {
									tr.Move(r.X+r.W/2, r.Y+r.H/2)
								}
							}
						}
						if shot.name == "overview" || shot.name == "overview-live" || shot.name == "overview-proxy-error" {
							controls := []string{"系统代理开关", "TUN 模式", "概览DNS开关", "概览域名嗅探开关", "DNS 设置", "域名嗅探 设置"}
							for _, label := range controls {
								r, ok := tr.Find(text(label))
								if !ok || r.Y < 38 || r.Y+r.H > float32(height-24) || r.X+r.W > float32(width-24) {
									t.Fatalf("%s: control %q is outside the overview: %+v (found=%v)", page, label, r, ok)
								}
							}
						}
						switch shot.name {
						case "mesh-security-bottom":
							if r, ok := tr.Find(text("立即断开组网")); ok {
								tr.Scroll(float32(width-100), float32(height-100), 0, r.Y+r.H-float32(height-40))
							}
						case "settings-general-bottom":
							tr.Scroll(float32(width-100), float32(height-100), 0, 3000)
						case "profile-dns-servers":
							tr.Scroll(700, 360, 0, 330)
						case "connection-rule-preview":
							tr.Scroll(float32(width/2), float32(height/2), 0, 1000)
						case "diagnostics-result-bottom":
							tr.Scroll(700, 360, 0, 2000)
						case "subscription-edit":
							tr.Scroll(float32(width/2), float32(height/2), 0, 1000)
						case "settings-cleanup":
							tr.Scroll(700, 420, 0, 1000)
						case "overview-features":
							tr.Scroll(700, 420, 0, 1000)
						case "settings-dns-lists", "settings-sniffer-lists":
							tr.Scroll(700, 420, 0, 2000)
						case "settings-sniffer-ports":
							tr.Scroll(700, 420, 0, 290)
						case "settings-dns-servers":
							tr.Scroll(700, 420, 0, 330)
						}
						if shot.name == "settings-dns" {
							if r, ok := tr.Find(text("保存 DNS 设置")); ok {
								tr.Move(r.X+r.W/2, r.Y+r.H/2)
							}
						}
						if shot.name == "conns" {
							if r, ok := tr.Find(text("查看连接 api.github.com")); ok {
								tr.Move(r.X+r.W/2, r.Y+r.H/2)
							}
						}
						if shot.name == "proxies-hover" {
							if r, ok := tr.Find(text("选择 日本 02")); ok {
								tr.Move(r.X+20, r.Y+r.H/2)
							}
						}
						if shot.name == "overview-service-update" {
							if r, ok := tr.Find(text("重新安装…")); ok {
								tr.Move(r.X+r.W/2, r.Y+r.H/2)
							}
						}
						if shot.name == "proxies-groups-scrolled" {
							if r, ok := tr.Find(text("策略组列表")); ok {
								tr.Scroll(r.X+r.W/2, r.Y+r.H/2, 0, 10000)
								tr.Move(r.X+r.W-6, r.Y+r.H-24)
							}
						}
						if strings.HasSuffix(shot.name, "-error") && shot.name != "raw-config-error" && shot.name != "backup-error" {
							tr.Scroll(700, 420, 0, 1000)
						}
						// Stable fixtures show the settled hover/selection appearance;
						// motion_test.go separately captures and checks transient frames.
						tr.Advance(200 * time.Millisecond)
						if strings.HasSuffix(shot.name, "-no-results") || shot.name == "conns-empty" || shot.name == "mesh-no-results" {
							if r, ok := tr.Find(text("重置筛选")); !ok || r.Y < 38 || r.Y+r.H > float32(height-16) || r.X+r.W > float32(width-16) {
								t.Fatalf("%s: filter recovery is missing or clipped: %+v", page, r)
							}
						}
						if shot.name == "mesh-security-bottom" {
							for _, label := range []string{"关闭本机访问", "立即断开组网"} {
								r, ok := tr.Find(text(label))
								if !ok || r.Y < 38 || r.Y+r.H > float32(height) {
									t.Fatalf("%s: scrolled security control %q is clipped: %+v", page, label, r)
								}
							}
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
	}

}

func fixtureManagedProfile(t *testing.T, a *App, id, name, server string) profile.Profile {
	t.Helper()
	raw := "proxies:\n  - {name: 香港 01, type: ss, server: " + server + ", port: 443, cipher: aes-128-gcm, password: fixture}\n  - {name: 日本 02, type: trojan, server: japan.example.com, port: 443, password: fixture}\nproxy-groups:\n  - {name: 节点选择, type: select, proxies: [香港 01, 日本 02, DIRECT]}\n  - {name: 自动选择, type: url-test, proxies: [香港 01, 日本 02], url: 'https://www.gstatic.com/generate_204', interval: 300}\nrules:\n  - MATCH,节点选择\n"
	p := profile.Profile{ID: id, Name: name, Kind: profile.KindFile, File: id + ".yaml", Updated: time.Date(2026, 10, 31, 12, 0, 0, 0, time.Local)}
	p.Path = filepath.Join(a.dir, "profiles", p.File)
	if err := os.WriteFile(filepath.Join(a.dir, "profiles", p.File), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	a.store.Items = append(a.store.Items, p)
	return p
}

func fixtureAggregation(t *testing.T, a *App, tab int) {
	a.page = "subs"
	first := fixtureManagedProfile(t, a, "fixture-work", "工作与家庭共用的长期订阅来源 · 多地区备用线路", "work.example.com")
	second := fixtureManagedProfile(t, a, "fixture-home", "家庭备用订阅", "home.example.com")
	a.openAggregationEditor("")
	a.aggregationName, a.aggregationTab = "工作与家庭", tab
	for i := range a.aggregationSources {
		switch a.aggregationSources[i].ID {
		case first.ID:
			a.aggregationSources[i].Selected = true
			a.aggregationSources[i].Prefix = "工作 | "
		case second.ID:
			a.aggregationSources[i].Selected = true
			a.aggregationSources[i].Prefix = "家庭 | "
		}
	}
}

func fixtureProxyEditor(t *testing.T, a *App) {
	p := fixtureManagedProfile(t, a, "fixture-proxy-editor", "工作与家庭共用的代理配置 · 本地修改保留最新订阅参数", "hongkong.example.com")
	a.store.Active, a.page = p.ID, "proxies"
	a.openProfileProxyEditor(p.ID)
}

func fixtureOverrideLibrary(t *testing.T, a *App, tab int) {
	a.page, a.extensionTab, a.extensionScope = "rules", 3, 0
	a.ov.Library = []patch.OverrideTemplate{
		{ID: "work-dns", Name: "工作网络 DNS", YAML: "dns:\n  enable: true\n  nameserver:\n    - https://doh.pub/dns-query\n"},
		{ID: "common", Name: "工作与家庭共用的连接设置 · 保留节点选择", YAML: "tcp-concurrent: true\nprofile:\n  store-selected: true\n"},
	}
	a.ov.TemplateIDs = []string{"common", "work-dns"}
	a.openOverrideLibrary()
	a.libraryTab = tab
}

func fixtureOverviewTraffic(a *App) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// A deterministic burst trace, with independent upload activity.
	trace := []int64{48, 52, 40, 85, 220, 640, 530, 850, 1024, 1640, 1880, 1450, 740, 420, 530, 220, 70, 45, 55, 160, 390, 850, 1450, 930, 710, 220, 85, 70, 55, 310, 620, 1020, 880, 730, 920, 900}
	a.hist = nil
	for i := 0; i < bars; i++ {
		a.hist = append(a.hist, sample{down: trace[i/2] * 1024, up: int64(18+(i*31)%100) * 1024})
	}
	a.last = core.Sample{DownNow: 900 * 1024, UpNow: a.hist[len(a.hist)-1].up, DownTotal: 812 << 20, UpTotal: 61 << 20, Conns: 4}
	a.sysOn, a.sysApplied = true, true
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
	tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 80})
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
	var selectionMu sync.Mutex
	selected := map[string]string{}
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
		nodes[len(nodes)-1] = "德国 184 · 工作与家庭共用的低延迟优选线路 · 长名称示例"
		for _, node := range nodes {
			proxies[node] = map[string]any{"type": "Shadowsocks"}
		}
		proxies["节点选择"] = map[string]any{"type": "Selector", "now": "香港 01", "all": append(append([]string{}, nodes...), "DIRECT")}
		groups := []string{"节点选择", "自动选择", "Netflix", "Ⓜ️ Microsoft", "Telegram", "Apple", "BiliBili", "Direct", "Block", "漏网之鱼 · 工作与家庭共用的长名称策略组"}
		for _, group := range groups[2:] {
			members, now := []string{"节点选择", "DIRECT"}, "节点选择"
			if group == "Direct" {
				members, now = []string{"DIRECT"}, "DIRECT"
			} else if group == "Block" {
				members, now = []string{"REJECT", "DIRECT"}, "REJECT"
			} else if strings.HasPrefix(group, "漏网之鱼") {
				members, now = nodes, nodes[len(nodes)-1]
			}
			proxies[group] = map[string]any{"type": "Selector", "now": now, "all": members}
		}
		global := append([]string{"DIRECT", "REJECT"}, nodes...)
		global = append(global, groups...)
		proxies["GLOBAL"] = map[string]any{"type": "Selector", "now": "节点选择", "all": global}
		selectionMu.Lock()
		for name, member := range selected {
			if group, ok := proxies[name].(map[string]any); ok {
				group["now"] = member
			}
		}
		selectionMu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"proxies": proxies})
	})
	mux.HandleFunc("/proxies/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			var body struct {
				Name string `json:"name"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Name == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			selectionMu.Lock()
			selected[strings.TrimPrefix(r.URL.Path, "/proxies/")] = body.Name
			selectionMu.Unlock()
		}
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
			{"id":"conn-github","metadata":{"host":"api.github.com","network":"tcp","process":"Browser","processPath":"/Applications/Browser.app/Contents/MacOS/Browser","sourceIP":"127.0.0.1","sourcePort":"52140","destinationIP":"140.82.112.6","destinationPort":"443"},"rule":"DomainSuffix","chains":["节点选择","香港 01"],"upload":12034,"download":884422,"start":"2026-10-27T13:00:00+08:00"},
			{"id":"conn-google","metadata":{"host":"www.google.com","network":"tcp","process":"Browser"},"rule":"Match","chains":["节点选择","香港 01"],"upload":3200,"download":55211,"start":"2026-10-27T13:00:02+08:00"},
			{"id":"conn-npm","metadata":{"host":"registry.npmjs.org","network":"tcp","process":"node"},"rule":"Match","chains":["节点选择","日本 02"],"upload":900,"download":2345111,"start":"2026-10-27T13:00:04+08:00"},
			{"id":"conn-long","metadata":{"host":"a-very-long-service-name.api.production.example.internal.example.com","network":"tcp","process":"Example Application With A Long Name"},"rule":"DomainSuffix","chains":["手动选择 · 工作与家庭共用的长名称策略组","香港 01 · 低延迟优选线路"],"upload":1234567,"download":98765432,"start":"2026-10-27T13:00:05+08:00"}
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
			`{"type":"error","payload":"[TCP] dial 节点选择 error: TLS handshake failed for a-very-long-service-name.api.production.example.internal.example.com:443; retry exhausted after 3 attempts; check the upstream certificate and DNS resolution. This complete diagnostic remains selectable and can be copied."}`,
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

func fixtureProfileEditor(a *App, tab int) {
	items := a.store.List()
	if len(items) == 0 {
		return
	}
	a.openProfileOverrides(items[0])
	a.extensionTab = tab + 1
	a.initExtensionDrafts()
	a.extraRulesDraft = "DOMAIN-SUFFIX,work.example.com,DIRECT\nDOMAIN,ads.example.com,REJECT"
	a.mergeYAMLDraft = "# 只影响这个订阅\ntcp-concurrent: true\nprofile:\n  store-selected: true\n"
	a.profileDNSDraft = dnsDraft{Source: 1, Enable: true, Mode: "fake-ip", Servers: "https://doh.pub/dns-query\n223.5.5.5", Bootstrap: "223.5.5.5\n1.1.1.1", Filter: "*.lan\n+.local"}
}

func fixtureDiagnostics(a *App, phase string) {
	a.page, a.settingsTab, a.diagURL = "settings", 6, "https://www.gstatic.com/generate_204"
	if phase == "idle" {
		return
	}
	a.diagSteps = diagnostic.Steps()
	for i := range a.diagSteps {
		a.diagSteps[i].State, a.diagSteps[i].Duration = diagnostic.Passed, 45*time.Millisecond
		a.diagSteps[i].Detail = "检测通过"
	}
	a.diagSteps[2].State, a.diagSteps[2].Detail = diagnostic.Info, "系统代理未开启；使用 TUN 时可忽略"
	if phase == "running" {
		a.diagRunning = true
		a.diagSteps[4].State, a.diagSteps[4].Detail = diagnostic.Running, "正在查询内核 DNS"
		for i := 5; i < len(a.diagSteps); i++ {
			a.diagSteps[i].State, a.diagSteps[i].Detail = diagnostic.Pending, "等待前项检测完成"
		}
	} else if phase == "canceled" {
		for i := 3; i < len(a.diagSteps); i++ {
			a.diagSteps[i].State, a.diagSteps[i].Detail = diagnostic.Skipped, "已取消"
		}
	} else {
		a.diagSteps[5].State, a.diagSteps[5].Detail, a.diagSteps[5].Duration = diagnostic.Failed, "请求超时，请检查网络或更换测试地址", 5*time.Second
		a.diagSteps[6].Detail = "收到 HTTP 204 响应"
	}
}
