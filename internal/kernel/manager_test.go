package kernel

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopurr/internal/core"
	"gopurr/internal/service"
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

func TestWaitServiceReadySuggestsRecoveryBeforeReinstall(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = m.waitServiceReady(400 * time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "重启服务") {
		t.Fatalf("expected service recovery guidance, got %v", err)
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

func TestPrivilegeCapabilityDoesNotDependOnQuickControllerProbe(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// No controller is listening. A temporarily busy/unreachable service must
	// retain its privilege capability while health is reported independently.
	raw, _ := json.Marshal(core.DaemonStatus{UID: 0, PID: os.Getpid(), Socket: m.socket})
	if err := os.WriteFile(m.statusPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if !m.IsRoot() {
		t.Fatal("transient controller failure erased service privileges")
	}
	if m.Health(20*time.Millisecond) != HealthUnresponsive {
		t.Fatal("readiness must still report the failed probe")
	}
}

func TestHealthDistinguishesExitUnresponsiveAndBusy(t *testing.T) {
	dir, err := os.MkdirTemp("", "gp-health-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Health(200 * time.Millisecond); got != HealthUnresponsive {
		t.Fatalf("no daemon = %v", got)
	}
	exit := &processExit{done: make(chan struct{})}
	m.exit = exit
	if got := m.Health(200 * time.Millisecond); got != HealthUnresponsive {
		t.Fatalf("live unanswering child = %v", got)
	}
	close(exit.done)
	start := time.Now()
	if got := m.Health(5 * time.Second); got != HealthExited || time.Since(start) > time.Second {
		t.Fatalf("exited child = %v after %v", got, time.Since(start))
	}
	m.mu.Lock()
	got := m.Health(200 * time.Millisecond)
	m.mu.Unlock()
	if got != HealthBusy {
		t.Fatalf("locked manager = %v", got)
	}
}

// A core whose controller answers in 0.7–2.5s is busy, not dead: Ensure must
// keep it rather than reap it and spawn a replacement.
func TestEnsureKeepsSlowButAliveCore(t *testing.T) {
	if service.Installed() {
		t.Skip("an installed system service owns the controller socket")
	}
	dir, err := os.MkdirTemp("", "gp-slow-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", m.socket)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"version":"test"}`))
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	if m.Running() {
		t.Fatal("fixture should miss the quick 700ms probe")
	}
	if err := m.Ensure(); err != nil {
		t.Fatalf("Ensure on a slow core: %v", err)
	}
	m.mu.Lock()
	spawned := m.cmd != nil
	m.mu.Unlock()
	if spawned {
		t.Fatal("Ensure reaped a slow-but-alive core and spawned another")
	}
	if got := m.Health(slowProbeTimeout); got != HealthOK {
		t.Fatalf("slow core health after Ensure = %v", got)
	}
}

// An installed service from an older controller interface can never serve this
// build: report the repair path at once instead of waiting for a timeout and
// blaming the TUN configuration.
func TestWaitServiceReadyRejectsOutdatedInterface(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(core.DaemonStatus{
		SecurityVersion:   core.SecurityVersion,
		ControllerVersion: core.ControllerVersion - 1,
		PID:               os.Getpid(),
		UID:               0,
		Socket:            m.socket,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.statusPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = m.waitServiceReady(5 * time.Second)
	if err == nil || !strings.Contains(err.Error(), "接口版本过旧") {
		t.Fatalf("outdated service interface not reported: %v", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("outdated interface took %v to report", took)
	}
}
