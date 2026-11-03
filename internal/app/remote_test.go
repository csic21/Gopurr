package app

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"gopurr/internal/patch"
	"gopurr/internal/remote"
)

func TestRemoteDraftValidation(t *testing.T) {
	a := &App{ov: patch.Default(), remotePath: filepath.Join(t.TempDir(), "remote.json")}
	a.remoteEnabled = true
	a.remoteBindIdx = 1
	a.remotePortDraft = "7899"
	a.remoteToken = "token-value"

	cfg, err := a.remoteDraft()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bind != "127.0.0.1" || cfg.Port != 7899 || !cfg.Enabled || cfg.Token != "token-value" {
		t.Fatalf("draft = %+v", cfg)
	}

	a.remotePortDraft = "7890"
	if _, err := a.remoteDraft(); err == nil || !strings.Contains(err.Error(), "混合代理端口") {
		t.Fatalf("mixed port error = %v", err)
	}

	a.remotePortDraft = "not-a-port"
	if _, err := a.remoteDraft(); err == nil {
		t.Fatal("invalid port accepted")
	}
}

func TestRemoteSettingsDirty(t *testing.T) {
	a := &App{ov: patch.Default()}
	a.remoteBase = remote.Config{Enabled: true, Bind: "0.0.0.0", Port: 7899, Token: "token"}
	a.remoteEnabled, a.remoteBindIdx, a.remotePortDraft = true, 0, "7899"
	if a.remoteSettingsDirty() {
		t.Fatal("unchanged settings reported dirty")
	}
	a.remotePortDraft = "7900"
	if !a.remoteSettingsDirty() {
		t.Fatal("port change was not dirty")
	}
	a.remotePortDraft = "7899"
	a.remoteEnabled = false
	if !a.remoteSettingsDirty() {
		t.Fatal("enable change was not dirty")
	}
}

func TestRequestRemoteSettingsStartsAndStopsServer(t *testing.T) {
	port := freeTCPPort(t)
	a := &App{
		ov:              patch.Default(),
		remotePath:      filepath.Join(t.TempDir(), "remote.json"),
		remoteEnabled:   true,
		remoteBindIdx:   1,
		remotePortDraft: fmt.Sprint(port),
		remoteToken:     "test-token-123456",
	}
	a.requestRemoteSettings()
	waitFor(t, 3*time.Second, func() bool {
		return a.remoteConfigSnapshot().Enabled && a.remoteServerSnapshot() != nil
	})

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `<div id="root">`) {
		t.Fatalf("index status=%d body=%q", resp.StatusCode, body)
	}
	if _, err := os.Stat(a.remotePath); err != nil {
		t.Fatal(err)
	}

	a.remoteEnabled = false
	a.requestRemoteSettings()
	waitFor(t, 3*time.Second, func() bool {
		cfg := a.remoteConfigSnapshot()
		return !cfg.Enabled && a.remoteServerSnapshot() == nil
	})
	if _, err := os.Stat(a.remotePath); err != nil {
		t.Fatal(err)
	}

	a.stopRemoteServer()
	if a.remoteServerSnapshot() != nil {
		t.Fatal("server still registered after stop")
	}
}

func TestStartRemoteAtBootPersistsGeneratedToken(t *testing.T) {
	port := freeTCPPort(t)
	path := filepath.Join(t.TempDir(), "remote.json")
	a := &App{
		ov:         patch.Default(),
		remotePath: path,
		remoteCfg:  remote.Config{Enabled: true, Bind: "127.0.0.1", Port: port},
	}
	a.remoteCfg = a.remoteCfg.Normalize()
	a.startRemoteAtBoot()
	waitFor(t, 3*time.Second, func() bool {
		return a.remoteServerSnapshot() != nil && a.remoteConfigSnapshot().Token != ""
	})
	defer a.stopRemoteServer()

	cfg, err := remote.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.Token == "" || cfg.Port != port {
		t.Fatalf("persisted config = %+v", cfg)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitFor(t *testing.T, timeout time.Duration, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestRemoteSettingsDialogInteraction(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPURR_HOME", dir)
	a := New()
	a.booted, a.ticking = true, true
	a.page, a.settingsTab = "settings", 1
	a.remotePortDraft = "7899"
	tr := ui.NewTester(a.View, 1060, 720)
	tr.SetPreferences(ui.Preferences{ReduceMotion: true})
	tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 80})
	tr.SetDark(true)
	tr.Frame()
	if err := tr.Click("配置…"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if !a.remoteShow {
		t.Fatal("remote settings dialog did not open")
	}
	if err := tr.Click("启用远程访问开关"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if !a.remoteEnabled {
		t.Fatal("remote switch did not update the draft")
	}
	if !tr.HasText("监听地址") || !tr.HasText("保存并应用") {
		t.Fatal("enabled dialog is missing its settings")
	}
	if err := tr.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if a.remoteShow {
		t.Fatal("remote settings dialog did not close")
	}
}
