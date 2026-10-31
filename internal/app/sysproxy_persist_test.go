package app

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopurr/internal/core"
	"gopurr/internal/sysproxy"
)

func TestPersistRestoreSystemProxyWritesPrefs(t *testing.T) {
	a, _ := newSettingsTestApp(t, nil)
	if a.Prefs().RestoreSystemProxy {
		t.Fatal("default RestoreSystemProxy should be false")
	}
	a.persistRestoreSystemProxy(true)
	if !a.Prefs().RestoreSystemProxy || !a.restoreProxyOn {
		t.Fatalf("persist true failed: prefs=%v ui=%v", a.Prefs().RestoreSystemProxy, a.restoreProxyOn)
	}
	raw, err := os.ReadFile(a.prefsPath)
	if err != nil {
		t.Fatal(err)
	}
	var disk Prefs
	if err := json.Unmarshal(raw, &disk); err != nil || !disk.RestoreSystemProxy {
		t.Fatalf("prefs.json missing restoreSystemProxy: %s err=%v", raw, err)
	}
	a.persistRestoreSystemProxy(false)
	if a.Prefs().RestoreSystemProxy || a.restoreProxyOn {
		t.Fatalf("persist false failed: prefs=%v ui=%v", a.Prefs().RestoreSystemProxy, a.restoreProxyOn)
	}
}

func TestSetSystemProxyPersistsRestorePreference(t *testing.T) {
	if !sysproxy.Available() {
		t.Skip("system proxy unavailable on this platform")
	}
	a, _ := newSettingsTestApp(t, nil)
	a.proxy = proxyControllerFixture{
		enable:  func(int, *sysproxy.State) error { return nil },
		restore: func(int, *sysproxy.State) error { return nil },
	}
	a.setSystemProxy(true)
	if !a.sysApplied || !a.Prefs().RestoreSystemProxy || !a.restoreProxyOn {
		t.Fatalf("enable did not persist restore pref: applied=%v prefs=%v ui=%v", a.sysApplied, a.Prefs().RestoreSystemProxy, a.restoreProxyOn)
	}
	a.setSystemProxy(false)
	if a.sysApplied || a.Prefs().RestoreSystemProxy || a.restoreProxyOn {
		t.Fatalf("disable did not clear restore pref: applied=%v prefs=%v ui=%v", a.sysApplied, a.Prefs().RestoreSystemProxy, a.restoreProxyOn)
	}
}

func TestEnsureMixedPortAvailableReclaimsStaleCore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPURR_HOME", dir)
	a := New()
	if a.mgr == nil {
		t.Fatal("manager missing")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	a.ov.MixedPort = port

	// Record a dead PID so ReclaimStale clears status; the live listener is a
	// stand-in for a foreign holder and must remain after reclaim.
	st := core.DaemonStatus{PID: 1 << 30, UID: -1, Socket: filepath.Join(dir, "core.sock")}
	body, _ := json.Marshal(st)
	if err := os.WriteFile(filepath.Join(dir, "core.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if a.ensureMixedPortAvailable() {
		t.Fatal("foreign TCP holder should still block mixed-port")
	}
	_ = ln.Close()
	deadline := time.Now().Add(2 * time.Second)
	for !portFree(port) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !a.ensureMixedPortAvailable() {
		t.Fatal("expected mixed-port available after reclaim and release")
	}
}
