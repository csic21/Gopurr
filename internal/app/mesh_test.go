package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/core"
	"gopurr/internal/mesh"
)

// Fixtures never execute a local Tailscale binary or change network settings.
type meshFixture struct {
	snap        mesh.Snapshot
	calls       []string
	shutdownErr error
	nativeURLs  []string
	probeFn     func(context.Context, string, string, uint16) mesh.ProbeResult
}

func (f *meshFixture) Snapshot() mesh.Snapshot { return f.snap }
func (*meshFixture) Start()                    {}
func (f *meshFixture) Refresh()                { f.calls = append(f.calls, "refresh") }
func (f *meshFixture) Do(action string) {
	f.calls = append(f.calls, action)
	if action == "allow" {
		f.snap.Status.ShieldsUp = false
	}
	if action == "block" {
		f.snap.Status.ShieldsUp = true
	}
	if action == "disconnect" {
		f.snap.Status.Running = false
		f.snap.Status.State = "Stopped"
	}
}
func (f *meshFixture) SetServices(raw string, allow bool) {
	f.snap.Status.Services, _ = mesh.ParseServices(raw)
	if allow {
		f.Do("allow")
	} else {
		f.Do("block")
	}
}
func (f *meshFixture) SetDeviceBlocked(id string, blocked bool) {
	f.calls = append(f.calls, "deny:"+id)
}
func (f *meshFixture) SetAutoConnect(enabled bool) {
	f.snap.AutoConnect = enabled
	f.calls = append(f.calls, "auto")
}
func (f *meshFixture) Probe(ctx context.Context, id, kind string, port uint16) mesh.ProbeResult {
	if f.probeFn != nil {
		return f.probeFn(ctx, id, kind, port)
	}
	return mesh.ProbeResult{DeviceID: id, Port: port, State: "verified", Message: "SSH 服务有响应；登录仍由客户端验证"}
}
func (f *meshFixture) Shutdown() error { return f.shutdownErr }
func (f *meshFixture) ClearHistory()   { f.calls = append(f.calls, "clear") }

func fixtureMesh(a *App) *meshFixture {
	self := mesh.Device{ID: "self", Name: "Karl 的 MacBook", OS: "macOS", Owner: "Karl", Login: "karl@example.com", Online: true, IPs: []netip.Addr{netip.MustParseAddr("100.70.0.1")}}
	f := &meshFixture{snap: mesh.Snapshot{Started: true, Managed: true, Updated: time.Now(), Status: mesh.Status{Installed: true, Official: true, Running: true, ShieldsKnown: true, ShieldsUp: true, State: "Running", Version: "embedded", Services: []mesh.Service{{Protocol: "tcp", Port: 22}}, Self: self, Tailnet: "karl.example"}}}
	for i, name := range []string{"家里的 Mac mini", "办公室 Windows", "云服务器 · 新加坡", "NAS · 家庭存储", "Lin 的笔记本", "一台名称非常长的工作站 · 设计与构建专用 · 长文本示例"} {
		d := mesh.Device{ID: fmt.Sprintf("device-%d", i), Name: name, OS: []string{"macOS", "windows", "linux", "linux", "macOS", "windows"}[i], Owner: "Karl", Login: "karl@example.com", Online: i < 4, IPs: []netip.Addr{netip.MustParseAddr(fmt.Sprintf("100.70.0.%d", i+2))}}
		if i == 4 {
			d.Owner, d.Login = "Lin", "lin@example.com"
		}
		f.snap.Status.Peers = append(f.snap.Status.Peers, d)
	}
	now := time.Date(2026, 10, 30, 15, 42, 8, 0, time.Local)
	f.snap.Visits = []mesh.Visit{
		{Key: "a", Protocol: "tcp", DeviceID: "device-4", Device: "Lin 的笔记本", Owner: "Lin", Login: "lin@example.com", Remote: "100.70.0.6:52318", Local: "100.70.0.1:22", FirstSeen: now.Add(-2 * time.Minute), LastSeen: now, Active: true},
		{Key: "b", Protocol: "tcp", DeviceID: "device-1", Device: "办公室 Windows", Owner: "Karl", Login: "karl@example.com", Remote: "100.70.0.3:61420", Local: "100.70.0.1:5900", FirstSeen: now.Add(-30 * time.Minute), LastSeen: now.Add(-25 * time.Minute)},
		{Key: "c", Protocol: "udp", DeviceID: "device-3", Device: "NAS · 家庭存储", Owner: "Karl", Login: "karl@example.com", Remote: "100.70.0.5:51111", Local: "100.70.0.1:53", FirstSeen: now.Add(-time.Hour), LastSeen: now.Add(-time.Hour)},
	}
	a.page = "mesh"
	a.meshState = meshState{meshClient: f}
	a.meshSSHLaunch = func(d mesh.Device, s mesh.Shortcut) error {
		uri, err := s.URI(d)
		if err == nil {
			f.nativeURLs = append(f.nativeURLs, uri)
		}
		return err
	}
	return f
}

func TestMeshInviteOpensOfficialUsersPageWithoutChangingAccess(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(fmt.Sprint(running), func(t *testing.T) {
			a := subscriptionTestApp(t)
			f := fixtureMesh(a)
			f.snap.Status.Running = running
			if !running {
				f.snap.Managed, f.snap.Status.State = false, "Stopped"
			}
			tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme(false)); a.meshPage(c) }, 840, 560)
			tr.Frame()
			if err := tr.Click("邀请成员"); err != nil {
				t.Fatal(err)
			}
			tr.Advance(time.Second)
			if !a.meshInvite || len(tr.OpenedURLs()) != 0 {
				t.Fatal("invitation guidance was skipped")
			}
			if err := tr.Click("打开邀请页面"); err != nil {
				t.Fatal(err)
			}
			if urls := tr.OpenedURLs(); len(urls) != 1 || urls[0] != mesh.InviteURL {
				t.Fatalf("wrong invitation destination: %v", urls)
			}
			if len(f.calls) != 0 || !f.snap.Status.ShieldsUp {
				t.Fatal("invitation changed local network or service access")
			}
			if err := tr.Click("关闭"); err != nil {
				t.Fatal(err)
			}
			if a.meshInvite {
				t.Fatal("invitation guidance did not close")
			}
		})
	}
}

func TestMeshInviteBrowserFailureCanRecoverByCopyingAddress(t *testing.T) {
	a := subscriptionTestApp(t)
	fixtureMesh(a)
	a.meshInvite = true
	tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme(false)); a.meshPage(c) }, 840, 560)
	tr.FailOpenURL(errors.New("browser unavailable"))
	tr.Frame()
	if err := tr.Click("打开邀请页面"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if a.meshInviteError == "" {
		t.Fatal("browser failure was hidden")
	}
	if err := tr.Click("复制控制台地址"); err != nil {
		t.Fatal(err)
	}
	if tr.Clipboard() != mesh.InviteURL {
		t.Fatal("invitation fallback copied the wrong address")
	}
}

func TestMeshOpeningRequiresConfirmationClosingDoesNot(t *testing.T) {
	a := subscriptionTestApp(t)
	f := fixtureMesh(a)
	tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme(false)); a.meshPage(c) }, 840, 560)
	tr.Frame()
	if err := tr.Click("允许组网访问本机"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if !a.meshConfirmAllow || len(f.calls) != 0 {
		t.Fatal("access opened before confirmation")
	}
	if err := tr.Click("取消"); err != nil {
		t.Fatal(err)
	}
	tr.Advance(time.Second)
	if !f.snap.Status.ShieldsUp {
		t.Fatal("cancel opened access")
	}
	if err := tr.Click("允许组网访问本机"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if err := tr.Click("允许访问本机"); err != nil {
		t.Fatal(err)
	}
	tr.Advance(time.Second)
	if len(f.calls) != 1 || f.calls[0] != "allow" {
		t.Fatalf("confirmation failed: %v", f.calls)
	}
	if err := tr.Click("允许组网访问本机"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if len(f.calls) != 2 || f.calls[1] != "block" {
		t.Fatalf("closing access was delayed: %v", f.calls)
	}
}

func TestMeshEmergencyDisconnectAndUnknownState(t *testing.T) {
	a := subscriptionTestApp(t)
	f := fixtureMesh(a)
	f.snap.Status.ShieldsKnown = false
	tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme(false)); a.meshPage(c) }, 840, 560)
	tr.Frame()
	_ = tr.Click("允许组网访问本机")
	if len(f.calls) != 0 {
		t.Fatal("unknown state allowed access")
	}
	if err := tr.Click("断开组网"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || f.calls[0] != "disconnect" {
		t.Fatal("emergency disconnect unavailable")
	}
}

func TestMeshDetailsShowIdentityAndCopyIP(t *testing.T) {
	a := subscriptionTestApp(t)
	fixtureMesh(a)
	tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme(false)); a.meshPage(c) }, 1060, 720)
	tr.Frame()
	if err := tr.Click("组网设备详情 self"); err != nil {
		t.Fatal(err)
	}
	tr.Advance(time.Second)
	if _, ok := tr.Find("所属账号"); !ok {
		t.Fatal("device identity hidden")
	}
	if err := tr.Click("复制虚拟 IP"); err != nil {
		t.Fatal(err)
	}
	if tr.Clipboard() != "100.70.0.1" {
		t.Fatal("wrong copied IP")
	}
}

func TestMeshLoginCannotSkipInitialProtection(t *testing.T) {
	a := subscriptionTestApp(t)
	f := fixtureMesh(a)
	f.snap.Managed = false
	f.snap.Status.Running = false
	f.snap.Status.State = "NeedsLogin"
	f.snap.Status.ShieldsUp = false
	f.snap.Status.AuthURL = "https://login.tailscale.com/a/fixture"
	tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme(false)); a.meshPage(c) }, 840, 560)
	tr.Frame()
	if err := tr.Click("继续登录 Tailscale"); err != nil {
		t.Fatal(err)
	}
	if !a.meshGuide {
		t.Fatal("login bypassed protective setup")
	}
	if len(tr.OpenedURLs()) != 0 {
		t.Fatal("unprotected login URL opened")
	}
}

func TestMeshEmbeddedOnboardingDoesNotRequireAnotherClient(t *testing.T) {
	a := subscriptionTestApp(t)
	f := fixtureMesh(a)
	f.snap.Managed = false
	f.snap.Status = mesh.Status{Installed: true, Official: true, ShieldsKnown: true, ShieldsUp: true, State: "Stopped"}
	tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme(false)); a.meshPage(c) }, 840, 560)
	tr.Frame()
	if _, ok := tr.Find("下载 Tailscale"); ok {
		t.Fatal("external installer still required")
	}
	if err := tr.Click("连接内置组网"); err != nil {
		t.Fatal(err)
	}
	tr.Advance(time.Second)
	if err := tr.Click("连接并登录"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || f.calls[0] != "connect" || len(tr.OpenedURLs()) != 0 {
		t.Fatal("embedded enrollment did not start")
	}
}

func TestMeshGuideConnectShowsProgressAndCanCancel(t *testing.T) {
	a := subscriptionTestApp(t)
	f := fixtureMesh(a)
	f.snap.Status.Running, f.snap.Status.State = false, "NeedsLogin"
	a.meshGuide = true
	tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme(false)); a.meshPage(c) }, 840, 560)
	tr.Frame()
	if err := tr.Click("连接并登录"); err != nil {
		t.Fatal(err)
	}
	f.snap.Busy, f.snap.Action = true, "connect"
	tr.Advance(time.Second)
	if _, ok := tr.Find("正在连接…"); !ok {
		t.Fatal("connection progress is hidden behind the modal")
	}
	_ = tr.Click("正在连接…")
	if len(f.calls) != 1 {
		t.Fatal("pending login can be started twice")
	}
	if err := tr.Click("取消连接"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 || f.calls[1] != "disconnect" || a.meshOpenAuth {
		t.Fatal("cancel did not stop enrollment and browser opening")
	}
	f.snap.Busy, f.snap.Action = false, ""
	f.snap.Status.AuthURL = "https://login.tailscale.com/a/late-result"
	tr.Frame()
	if len(tr.OpenedURLs()) != 0 {
		t.Fatal("canceled connection opened a late login link")
	}
}

func TestMeshEmptyPortAuthorizationCannotOpenAccess(t *testing.T) {
	a := subscriptionTestApp(t)
	f := fixtureMesh(a)
	tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme(false)); a.meshPage(c) }, 840, 560)
	tr.Frame()
	if err := tr.Click("允许组网访问本机"); err != nil {
		t.Fatal(err)
	}
	a.meshServicesDraft = ""
	tr.Advance(time.Second)
	if err := tr.Click("允许访问本机"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if len(f.calls) != 0 || a.meshServicesError == "" || !f.snap.Status.ShieldsUp {
		t.Fatal("empty authorization opened services")
	}
}

func TestMeshDeviceBanIsAvailableInsideIdentityDetails(t *testing.T) {
	a := subscriptionTestApp(t)
	f := fixtureMesh(a)
	// Find the target explicitly; native font metrics can move its sorted row below the viewport.
	a.meshQuery = f.snap.Status.Peers[0].Name
	tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme(false)); a.meshPage(c) }, 840, 560)
	tr.Frame()
	if err := tr.Click("组网设备详情 device-0"); err != nil {
		t.Fatal(err)
	}
	tr.Advance(time.Second)
	if err := tr.Click("禁止此设备访问本机"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || f.calls[0] != "deny:device-0" {
		t.Fatal("wrong device revoked")
	}
}

func meshTestPrepared(a *App) {
	a.tunOn, a.ov.TunEnable, a.ov.TunAutoRout = true, true, true
	a.modeToken, a.ov.Mode = "rule", "rule"
}

func TestMeshServicePresetsDoNotOpenBeforeConfirmation(t *testing.T) {
	a := subscriptionTestApp(t)
	f := fixtureMesh(a)
	f.snap.Status.Services = nil
	tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme(false)); a.meshPage(c) }, 840, 560)
	tr.Frame()
	if err := tr.Click("允许组网访问本机"); err != nil {
		t.Fatal(err)
	}
	tr.Advance(time.Second)
	if err := tr.Click("授权 SSH"); err != nil {
		t.Fatal(err)
	}
	if a.meshServicesDraft != "22/tcp" || len(f.calls) != 0 {
		t.Fatal("preset either failed or opened too early")
	}
	if err := tr.Click("授权 Windows 远程桌面"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.meshServicesDraft, "3389/tcp") {
		t.Fatal("RDP port missing")
	}
	if err := tr.Click("允许访问本机"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || f.calls[0] != "allow" {
		t.Fatal("confirmed presets did not apply")
	}
}

func TestMeshNetworkPreparationStaysOnPageAndCancelChangesNothing(t *testing.T) {
	a := subscriptionTestApp(t)
	fixtureMesh(a)
	a.tunOn, a.ov.TunEnable = false, false
	tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme(false)); a.meshPage(c) }, 840, 560)
	tr.Frame()
	if err := tr.Click("准备访问"); err != nil {
		t.Fatal(err)
	}
	tr.Advance(time.Second)
	if a.page != "mesh" || !a.meshPrepareConfirm {
		t.Fatal("preparation navigated away")
	}
	if err := tr.Click("取消"); err != nil {
		t.Fatal(err)
	}
	_, busy, _ := a.svcState()
	if a.ov.TunEnable || busy || a.meshPreparePending {
		t.Fatal("cancel changed networking")
	}
}

func TestMeshNetworkPreparationAppliesTogetherAndRollsBackSaveFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			a, reloads := newSettingsTestApp(t, nil)
			f := fixtureMesh(a)
			status, _ := json.Marshal(core.DaemonStatus{UID: 0})
			if err := os.WriteFile(filepath.Join(a.dir, "core.json"), status, 0600); err != nil {
				t.Fatal(err)
			}
			a.modeToken, a.ov.Mode, a.ov.TunAutoRout = "global", "global", false
			a.tunOn, a.ov.TunEnable = false, false
			if fail {
				a.ovPath = t.TempDir()
			}
			applied := a.applyMeshPreparation()
			if fail {
				if applied || a.tunOn || a.ov.TunAutoRout || a.ov.Mode != "global" || len(reloads()) != 2 {
					t.Fatalf("rollback failed: %s", a.meshPrepareError)
				}
			} else {
				if !applied || !a.meshAccessReady() || a.ov.Mode != "rule" || !a.ov.TunAutoRout || len(reloads()) != 1 {
					t.Fatalf("prepare failed: %s", a.meshPrepareError)
				}
			}
			if !f.snap.Status.ShieldsUp {
				t.Fatal("outbound setup opened inbound services")
			}
		})
	}
}

func TestMeshQuickLaunchRemembersSettingsAndUsesCurrentDeviceIP(t *testing.T) {
	a := subscriptionTestApp(t)
	f := fixtureMesh(a)
	meshTestPrepared(a)
	f.snap.Status.Peers = []mesh.Device{f.snap.Status.Peers[2]}
	d := f.snap.Status.Peers[0]
	tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme(false)); a.meshPage(c) }, 840, 560)
	tr.Frame()
	if err := tr.Click("快捷连接 " + d.ID); err != nil {
		t.Fatal(err)
	}
	a.meshQuickUser = "ubuntu"
	tr.Advance(time.Second)
	if err := tr.Click("打开客户端"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if urls := meshOpenedTestURLs(tr, f); len(urls) != 1 || urls[0] != "ssh://ubuntu@100.70.0.4:22" {
		t.Fatalf("incorrect launch %v", urls)
	}
	if _, ok := a.meshShortcuts[d.ID]; !ok {
		t.Fatal("shortcut not remembered")
	}
	info, err := os.Stat(filepath.Join(a.dir, "mesh-shortcuts.json"))
	if err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("private shortcut storage")
	}
	if err := tr.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	tr.Advance(time.Second)
	f.snap.Status.Peers[0].IPs = []netip.Addr{netip.MustParseAddr("100.70.0.44")}
	tr.Frame()
	if err := tr.Click("快捷连接 " + d.ID); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if urls := meshOpenedTestURLs(tr, f); len(urls) != 2 || urls[1] != "ssh://ubuntu@100.70.0.44:22" || a.meshQuick {
		t.Fatal("saved shortcut did not use fresh address directly")
	}
}

func TestMeshMissingClientShowsRecoveryWithoutSavingFailedLaunch(t *testing.T) {
	a := subscriptionTestApp(t)
	f := fixtureMesh(a)
	meshTestPrepared(a)
	a.openMeshQuick(f.snap.Status.Peers[2])
	tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme(false)); a.meshPage(c) }, 840, 560)
	tr.FailOpenURL(errors.New("no handler"))
	a.meshSSHLaunch = func(mesh.Device, mesh.Shortcut) error { return errors.New("no handler") }
	tr.Frame()
	if err := tr.Click("打开客户端"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if a.meshQuickError == "" || len(a.meshShortcuts) != 0 || !a.meshQuick {
		t.Fatal("missing client not recoverable")
	}
	if err := tr.Click("复制地址"); err != nil {
		t.Fatal(err)
	}
	if tr.Clipboard() != "100.70.0.4:22" {
		t.Fatal("address recovery failed")
	}
}

func TestMeshProbeRunsOnlyOnRequestAndIgnoresCanceledResult(t *testing.T) {
	a := subscriptionTestApp(t)
	f := fixtureMesh(a)
	entered, done := make(chan struct{}), make(chan struct{})
	f.probeFn = func(ctx context.Context, id, kind string, port uint16) mesh.ProbeResult {
		close(entered)
		<-ctx.Done()
		defer close(done)
		return mesh.ProbeResult{State: "verified", Message: "stale"}
	}
	a.openMeshQuick(f.snap.Status.Peers[2])
	tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme(false)); a.meshPage(c) }, 840, 560)
	tr.Frame()
	select {
	case <-entered:
		t.Fatal("probe ran without request")
	default:
	}
	if err := tr.Click("检测连接"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("probe not started")
	}
	if err := tr.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("probe not canceled")
	}
	time.Sleep(5 * time.Millisecond)
	busy, result := a.meshProbeSnapshot()
	if busy || result.Message != "" {
		t.Fatal("stale canceled probe replaced current UI")
	}
}

func TestMeshReconnectDoesNotRepeatConsentAndAuthOpensOnce(t *testing.T) {
	a := subscriptionTestApp(t)
	f := fixtureMesh(a)
	f.snap.Status.Running, f.snap.Status.State = false, "Stopped"
	tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme(false)); a.meshPage(c) }, 840, 560)
	tr.Frame()
	if err := tr.Click("连接组网"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || f.calls[0] != "connect" || a.meshGuide {
		t.Fatal("repeated unnecessary setup")
	}
	f.snap.Status.AuthURL, f.snap.Status.State = "https://login.tailscale.com/a/test", "NeedsLogin"
	tr.Frame()
	tr.Frame()
	if len(tr.OpenedURLs()) != 1 {
		t.Fatal("auth URL opened repeatedly or not at all")
	}
}

func meshOpenedTestURLs(tr *ui.Tester, f *meshFixture) []string {
	if runtime.GOOS == "windows" {
		return f.nativeURLs
	}
	return tr.OpenedURLs()
}

func TestMeshRemoteDesktopHandoffUsesPrivateFileWithoutCredentials(t *testing.T) {
	a := subscriptionTestApp(t)
	f := fixtureMesh(a)
	meshTestPrepared(a)
	d := f.snap.Status.Peers[1]
	a.openMeshQuick(d)
	tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme(false)); a.meshPage(c) }, 840, 560)
	tr.Frame()
	if err := tr.Click("打开客户端"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	urls := tr.OpenedURLs()
	if len(urls) != 1 {
		t.Fatalf("missing RDP handoff: %v", urls)
	}
	u, err := url.Parse(urls[0])
	if err != nil || u.Scheme != "file" || u.Host != "" {
		t.Fatal("invalid RDP file URL")
	}
	path := filepath.FromSlash(u.Path)
	if runtime.GOOS == "windows" {
		path = filepath.FromSlash(strings.TrimPrefix(u.Path, "/"))
	}
	if filepath.Dir(path) != filepath.Join(a.dir, "mesh-shortcuts") {
		t.Fatal("RDP file outside private directory")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "full address:s:100.70.0.3:3389") || strings.Contains(string(raw), "password") || !strings.Contains(string(raw), "redirectclipboard:i:0") {
		t.Fatal("unsafe RDP handoff")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("RDP settings not private")
		}
	}
}

func TestMeshUncheckingRememberRemovesExistingShortcut(t *testing.T) {
	a := subscriptionTestApp(t)
	f := fixtureMesh(a)
	meshTestPrepared(a)
	d := f.snap.Status.Peers[2]
	if err := a.rememberMeshShortcut(d, mesh.DefaultShortcut("linux")); err != nil {
		t.Fatal(err)
	}
	a.openMeshQuick(d)
	tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme(false)); a.meshPage(c) }, 840, 560)
	tr.Frame()
	if err := tr.Click("记住组网连接方式"); err != nil {
		t.Fatal(err)
	}
	if err := tr.Click("打开客户端"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if _, ok := a.meshShortcuts[d.ID]; ok {
		t.Fatal("unremembered shortcut retained in memory")
	}
	var saved map[string]mesh.Shortcut
	raw, err := os.ReadFile(filepath.Join(a.dir, "mesh-shortcuts.json"))
	if err != nil || json.Unmarshal(raw, &saved) != nil || len(saved) != 0 {
		t.Fatal("unremembered shortcut persisted")
	}
}

func TestMeshQuickPreparationCancelReturnsToDraft(t *testing.T) {
	a := subscriptionTestApp(t)
	f := fixtureMesh(a)
	a.openMeshQuick(f.snap.Status.Peers[2])
	a.meshQuickUser = "ubuntu"
	a.meshQuickPort = "2222"
	tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme(false)); a.meshPage(c) }, 840, 560)
	tr.Frame()
	a.requestMeshPreparation()
	tr.Advance(time.Second)
	if !a.meshPrepareConfirm || a.meshQuick {
		t.Fatal("preparation did not replace quick dialog")
	}
	if err := tr.Click("取消"); err != nil {
		t.Fatal(err)
	}
	tr.Advance(time.Second)
	if !a.meshQuick || a.meshQuickPort != "2222" || a.meshQuickUser != "ubuntu" || a.ov.TunEnable {
		t.Fatal("cancel discarded connection draft or changed networking")
	}
}

func TestMeshBackgroundReconnectDoesNotOpenLoginBrowser(t *testing.T) {
	a := subscriptionTestApp(t)
	f := fixtureMesh(a)
	f.snap.AutoConnect = true
	f.snap.Status.Running = false
	f.snap.Status.State = "NeedsLogin"
	f.snap.Status.AuthURL = "https://login.tailscale.com/a/test"
	tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme(false)); a.meshPage(c) }, 840, 560)
	tr.Frame()
	tr.Frame()
	if len(tr.OpenedURLs()) != 0 || len(f.calls) != 0 {
		t.Fatal("background reconnect opened browser or changed access")
	}
}

func TestMeshQuickLaunchAppliesRoutesBeforeNativeHandoffAndStopsOnFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			a, payloads := newSettingsTestApp(t, func(raw string) bool { return fail && strings.Contains(raw, "__GOPURR_TAILNET__") })
			f := fixtureMesh(a)
			meshTestPrepared(a)
			f.snap.Status.ProxyAddress = "127.0.0.1:47654"
			f.snap.Status.ProxyPassword = strings.Repeat("x", 32)
			a.openMeshQuick(f.snap.Status.Peers[2])
			tr := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme(false)); a.meshPage(c) }, 840, 560)
			tr.Frame()
			if err := tr.Click("打开客户端"); err != nil {
				t.Fatal(err)
			}
			tr.Frame()
			if len(payloads()) == 0 || !strings.Contains(payloads()[0], "__GOPURR_TAILNET__") {
				t.Fatal("native launch did not first apply current routes")
			}
			urls := meshOpenedTestURLs(tr, f)
			if fail {
				if len(urls) != 0 || a.meshQuickError == "" || a.meshRouteError == "" {
					t.Fatal("native client opened after route failure")
				}
			} else {
				if len(urls) != 1 || a.meshRouteKey == "" {
					t.Fatal("prepared native handoff missing")
				}
			}
		})
	}
}
