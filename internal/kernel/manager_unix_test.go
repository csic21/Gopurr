//go:build !windows

package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopurr/internal/core"
)

// The daemon that recorded itself in core.json is alive but its published
// controller link points at a deleted temporary directory: waiting the full
// timeout and quoting last week's log would hide the real cause.
func TestWaitServiceReadyReportsLostControllerSocket(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "gone.sock"), m.socket); err != nil {
		t.Fatal(err)
	}
	writeDaemonStatus(t, m, core.DaemonStatus{
		SecurityVersion:   core.SecurityVersion,
		ControllerVersion: core.ControllerVersion,
		PID:               os.Getpid(),
		UID:               0,
		Socket:            m.socket,
	})
	start := time.Now()
	err = m.waitServiceReady(3 * time.Second)
	if err == nil || !strings.Contains(err.Error(), "控制套接字") {
		t.Fatalf("lost controller socket not reported: %v", err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("took %v to report a dead controller", took)
	}
}

// The installer copies the service log once. A line from days ago must not be
// presented as the reason this start failed.
func TestWaitServiceReadyReportsStaleServiceLog(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-72 * time.Hour).Format(time.RFC3339Nano)
	body := `time="` + stamp + `" level=error msg="[TUN] default interface lost by monitor"` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "service.log"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	err = m.waitServiceReady(200 * time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "服务日志最后更新于") {
		t.Fatalf("stale service log not reported: %v", err)
	}
	if strings.Contains(err.Error(), "default interface lost") {
		t.Fatalf("benign TUN noise quoted as the failure: %v", err)
	}
}

func writeDaemonStatus(t *testing.T, m *Manager, status core.DaemonStatus) {
	t.Helper()
	body, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.statusPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
}
