package kernel

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopurr/internal/core"
)

func TestStartupReportsExitImmediatelyWithCoreOutput(t *testing.T) {
	m := &Manager{logPath: filepath.Join(t.TempDir(), "core.log")}
	if err := os.WriteFile(m.logPath, []byte("bind: address already in use"), 0600); err != nil {
		t.Fatal(err)
	}
	exit := &processExit{done: make(chan struct{}), err: errors.New("exit status 1")}
	close(exit.done)
	start := time.Now()
	err := m.waitReady(exit, 10*time.Second)
	if err == nil || !strings.Contains(err.Error(), "address already in use") || time.Since(start) > time.Second {
		t.Fatalf("early exit not reported promptly: %v", err)
	}
}

func TestHiddenWindowReducesConnectionPolling(t *testing.T) {
	r := NewRemote("unused", "secret")
	r.SetUIActive(false)
	if r.connectionPollInterval() < 10*time.Second {
		t.Fatal("hidden window still polls frequently")
	}
	r.SetUIActive(true)
	if r.connectionPollInterval() != 2*time.Second {
		t.Fatal("reopened window did not resume live polling")
	}
}

func TestReapStaleClearsDeadStatusAndSocket(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	st := core.DaemonStatus{PID: 1 << 30, UID: -1, Socket: m.socket} // unlikely live pid
	body, _ := json.Marshal(st)
	if err := os.WriteFile(m.statusPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.socket, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.reapStaleLocked()
	m.mu.Unlock()
	if _, err := os.Stat(m.statusPath); !os.IsNotExist(err) {
		t.Fatalf("status not cleared: %v", err)
	}
	if _, err := os.Stat(m.socket); !os.IsNotExist(err) {
		t.Fatalf("socket not cleared: %v", err)
	}
}

func TestWaitServiceReadyFailsQuicklyWithReinstallGuidance(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = m.waitServiceReady(400 * time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "重新安装") {
		t.Fatalf("expected quick reinstall guidance, got %v", err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("service wait took too long: %v", took)
	}
}

func TestServiceUpdateRequiredIgnoresMarketingVersion(t *testing.T) {
	compatible := core.DaemonStatus{
		SecurityVersion:   core.SecurityVersion,
		ControllerVersion: core.ControllerVersion,
		Version:           "0.1.0",
	}
	if serviceUpdateRequired(compatible) {
		t.Fatal("marketing version skew must not force service reinstall")
	}
	if !serviceVersionSkew(compatible, "0.6.7") {
		t.Fatal("expected soft version skew for API-compatible service")
	}
	if serviceVersionSkew(compatible, "dev") || serviceVersionSkew(compatible, "0.1.0") {
		t.Fatal("dev builds and matching versions must not report skew")
	}

	securityBreak := compatible
	securityBreak.SecurityVersion = core.SecurityVersion - 1
	if !serviceUpdateRequired(securityBreak) {
		t.Fatal("security version mismatch must force update")
	}
	if serviceVersionSkew(securityBreak, "0.6.7") {
		t.Fatal("forced security update must not also report soft skew")
	}

	controllerBreak := compatible
	controllerBreak.ControllerVersion = core.ControllerVersion - 1
	if !serviceUpdateRequired(controllerBreak) {
		t.Fatal("controller version mismatch must force update")
	}
}

func TestReclaimStaleIsSafeWithoutStatus(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.ReclaimStale() // must not panic when nothing is recorded
}
