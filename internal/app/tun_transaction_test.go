package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopurr/internal/core"
)

func TestTunSaveFailureRestoresRuntimeAndSwitch(t *testing.T) {
	a, reloads := newSettingsTestApp(t, nil)
	status, _ := json.Marshal(core.DaemonStatus{UID: 0})
	if err := os.WriteFile(filepath.Join(a.dir, "core.json"), status, 0600); err != nil {
		t.Fatal(err)
	}
	savedPath := a.ovPath
	before, _ := os.ReadFile(savedPath)
	a.ovPath = t.TempDir() // atomic rename onto a directory fails
	a.tunOn = true         // the bound native switch changes before applyTun is called
	a.applyTun(true)
	after, _ := os.ReadFile(savedPath)
	if a.ov.TunEnable || a.tunOn || len(reloads()) != 2 || string(before) != string(after) ||
		!strings.Contains(a.tunErrText(), "保存失败") {
		t.Fatalf("failed TUN save did not roll back: enabled=%v error=%s reloads=%d", a.tunOn, a.tunErrText(), len(reloads()))
	}
	runtime, _ := os.ReadFile(a.mgr.ConfigPath())
	if strings.Contains(string(runtime), "enable: true") {
		t.Fatalf("new TUN configuration survived rollback:\n%s", runtime)
	}
}

func TestTunRollbackFailureKeepsLastAcceptedStateVisible(t *testing.T) {
	a, reloads := newSettingsTestApp(t, func(raw string) bool { return strings.Contains(raw, "enable: false") })
	status, _ := json.Marshal(core.DaemonStatus{UID: 0})
	if err := os.WriteFile(filepath.Join(a.dir, "core.json"), status, 0600); err != nil {
		t.Fatal(err)
	}
	a.ovPath = t.TempDir()
	a.tunOn = true
	a.applyTun(true)
	if !a.ov.TunEnable || !a.tunOn || len(reloads()) != 2 || !strings.Contains(a.tunErrText(), "恢复原内核配置失败") {
		t.Fatalf("failed rollback hid the accepted kernel state: enabled=%v error=%s", a.tunOn, a.tunErrText())
	}
}
