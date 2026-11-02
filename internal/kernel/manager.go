package kernel

import "gopurr/internal/localize"

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"gopurr/internal/buildinfo"
	"gopurr/internal/core"
	"gopurr/internal/process"
	"gopurr/internal/securefs"
	"gopurr/internal/service"
)

// Manager owns the daemon's files and, when the app started it, its process.
// A daemon installed as a LaunchDaemon is used as-is; the app only talks to it.
type Manager struct {
	dir string

	socket     string
	secretFile string
	configPath string
	statusPath string
	home       string
	logPath    string

	mu     sync.Mutex
	cmd    *exec.Cmd // non-nil when this app spawned the daemon
	exit   *processExit
	remote *Remote
}

type processExit struct {
	done chan struct{}
	err  error // written before done closes
}

func NewManager(dir string) (*Manager, error) {
	if err := securefs.PrivateDir(dir); err != nil {
		return nil, err
	}
	for _, name := range []string{"local.yaml", "runtime.yaml", "prefs.json", "overrides.json", "sysproxy.json"} {
		if err := securefs.PrivateFile(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	m := &Manager{
		dir:        dir,
		socket:     filepath.Join(dir, "core.sock"),
		secretFile: filepath.Join(dir, "core.secret"),
		configPath: filepath.Join(dir, "runtime.yaml"),
		statusPath: filepath.Join(dir, "core.json"),
		home:       filepath.Join(dir, "mihomo"),
		logPath:    filepath.Join(dir, "core.log"),
	}
	if err := securefs.PrivateDir(m.home); err != nil {
		return nil, err
	}
	secret, err := m.ensureSecret()
	if err != nil {
		return nil, err
	}
	m.remote = NewRemote(m.socket, secret)
	return m, nil
}

// ConfigPath is where the app writes the config the daemon runs.
func (m *Manager) ConfigPath() string { return m.configPath }

// Remote is the controller client.
func (m *Manager) Remote() *Remote { return m.remote }

func (m *Manager) ensureSecret() (string, error) {
	if b, err := os.ReadFile(m.secretFile); err == nil && len(b) >= 16 {
		if err := securefs.PrivateFile(m.secretFile); err != nil {
			return "", err
		}
		return string(b), nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(buf)
	if err := securefs.WriteFile(m.secretFile, []byte(secret), 0o600); err != nil {
		return "", err
	}
	return secret, nil
}

func (m *Manager) healthy() bool { return m.answersWithin(700 * time.Millisecond) }

// slowProbeTimeout matches the app watchdog's probe: a controller that answers
// within it is alive, just busy (large reload, GC pause, loaded service).
const slowProbeTimeout = 2500 * time.Millisecond

func (m *Manager) answersWithin(timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return m.remote.Probe(ctx) == nil
}

// Health classifies the daemon for the app's crash-recovery watchdog. Unlike
// Running, which is a quick UI liveness hint, Health distinguishes a process
// that really exited from a controller that merely did not answer in time.
type Health int

const (
	// HealthOK means the controller answered the probe.
	HealthOK Health = iota
	// HealthBusy means the manager is spawning, reaping or reloading the
	// daemon, so a failed probe would say nothing about a crash.
	HealthBusy
	// HealthUnresponsive means the probe failed but no process exit was
	// observed (service-managed daemon, or a wedged / slow child).
	HealthUnresponsive
	// HealthExited means the daemon this app spawned has exited.
	HealthExited
)

func (h Health) String() string {
	switch h {
	case HealthOK:
		return "ok"
	case HealthBusy:
		return "busy"
	case HealthUnresponsive:
		return "unresponsive"
	case HealthExited:
		return "exited"
	}
	return fmt.Sprintf("health(%d)", int(h))
}

// Health probes the controller with the given timeout. A self-spawned core's
// real exit is reported without waiting for the probe. While Ensure holds the
// manager lock (spawn / reap / startup wait) it reports HealthBusy instead of
// blocking.
func (m *Manager) Health(timeout time.Duration) Health {
	if !m.mu.TryLock() {
		return HealthBusy
	}
	exit := m.exit
	m.mu.Unlock()
	if exit != nil {
		select {
		case <-exit.done:
			return HealthExited
		default:
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if m.remote.Probe(ctx) == nil {
		return HealthOK
	}
	if exit != nil {
		select {
		case <-exit.done:
			return HealthExited
		default:
		}
	}
	return HealthUnresponsive
}

// Status reads what the daemon wrote about itself.
func (m *Manager) Status() (core.DaemonStatus, error) {
	var st core.DaemonStatus
	b, err := os.ReadFile(m.statusPath)
	if err != nil {
		return st, err
	}
	err = json.Unmarshal(b, &st)
	return st, err
}

// Running reports a daemon that answers.
func (m *Manager) Running() bool { return m.healthy() }

// IsRoot reports whether the answering daemon has privileges (needed for TUN).
func (m *Manager) IsRoot() bool {
	st, err := m.Status()
	return err == nil && st.UID == 0 && (st.PID == 0 || process.Alive(st.PID)) && m.healthy()
}

// ServiceNeedsUpdate reports that an installed privileged service must be
// reinstalled before the UI can rely on it. Only SecurityVersion /
// ControllerVersion mismatches force this: those constants change when the
// daemon's trust boundary or controller API is no longer compatible.
//
// A marketing/app Version skew alone does not force reinstall — tray-only or
// UI-only releases keep an API-compatible service running. See
// ServiceVersionSkew for the soft suggestion, and docs/releasing.md.
// Never copy into Program Files / LaunchDaemons without an explicit UAC /
// admin authorization path (Install/Reinstall).
func (m *Manager) ServiceNeedsUpdate() bool {
	if !service.ForDataDir(m.dir) {
		return false
	}
	st, err := m.Status()
	return err == nil && serviceUpdateRequired(st)
}

// ServiceVersionSkew is true when the installed service answers with an older
// (or different) marketing Version but remains API-compatible. The overview
// may soft-suggest reinstall; it must not block TUN or force "重新安装".
func (m *Manager) ServiceVersionSkew() bool {
	if !service.ForDataDir(m.dir) {
		return false
	}
	st, err := m.Status()
	return err == nil && serviceVersionSkew(st, buildinfo.Version)
}

// serviceUpdateRequired is the security/API gate shared by ServiceNeedsUpdate
// and tests. Marketing Version is intentionally ignored here.
func serviceUpdateRequired(st core.DaemonStatus) bool {
	return st.SecurityVersion < core.SecurityVersion || st.ControllerVersion < core.ControllerVersion
}

// serviceVersionSkew detects app/service Version mismatch when the service is
// still Security/Controller compatible. Dev builds never report skew.
func serviceVersionSkew(st core.DaemonStatus, appVersion string) bool {
	if appVersion == "" || appVersion == "dev" || st.Version == "" {
		return false
	}
	if serviceUpdateRequired(st) {
		return false
	}
	return st.Version != appVersion
}

// ReclaimStale stops an unowned daemon recorded in core.json and clears leftover
// socket/status files. Safe to call before mixed-port binding or after Shutdown
// when a previous session may still hold the port.
func (m *Manager) ReclaimStale() {
	if m.ServiceOwnsRuntime() {
		return
	} // the SCM/launchd owns its PID and socket
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reapStaleLocked()
	m.remote.resetTransport()
}

func (m *Manager) ServiceOwnsRuntime() bool {
	if !service.ForDataDir(m.dir) {
		return false
	}
	st, err := m.Status()
	return err == nil && st.UID == 0 && st.Socket == m.socket
}

// Ensure makes sure a daemon is up. If one is already listening (installed as a
// system service) it is left alone; otherwise the app spawns it with the
// current user's rights.
func (m *Manager) Ensure() error {
	if m.healthy() {
		return nil
	}
	// An installed system service owns the socket. Spawning a child here would
	// just make the two fight over it.
	if service.ForDataDir(m.dir) {
		if err := service.EnsureRunning(); err != nil {
			return fmt.Errorf(localize.Text("启动系统服务失败，请使用「重启服务」修复: %w"), err)
		}
		return m.waitServiceReady(serviceReadyWait)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// The quick probe is a UI hint. Before reaping, give the core the same
	// patience as the watchdog: killing a slow-but-working core here and
	// respawning it is exactly the "core keeps dying" the user sees.
	if m.answersWithin(slowProbeTimeout) {
		return nil
	}

	// A previous session may have left a live process that no longer answers
	// Probe (stale socket, wrong secret, wedged controller) while still holding
	// mixed-port / AF_UNIX. Spawning without reaping fails intermittently with
	// "address already in use" or listenController remove errors on Windows.
	m.reapStaleLocked()
	m.remote.resetTransport()

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// Truncate so startupError shows this attempt, not an older failure.
	logFile, err := os.OpenFile(m.logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}

	cmd := exec.Command(exe,
		"--core",
		"--config", m.configPath,
		"--socket", m.socket,
		"--secret-file", m.secretFile,
		"--parent", fmt.Sprint(os.Getpid()),
		"--home", m.home,
		"--status", m.statusPath,
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	process.Configure(cmd)
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return err
	}
	m.cmd = cmd
	m.exit = &processExit{done: make(chan struct{})}
	exit := m.exit
	_ = logFile.Close()
	go func() {
		exit.err = cmd.Wait()
		close(exit.done)
	}()
	// Large subscriptions spend most of this window inside hub.Parse before the
	// controller socket exists; 10s was a frequent false timeout.
	if err := m.waitReady(exit, 30*time.Second); err != nil {
		_ = cmd.Process.Kill()
		<-exit.done
		m.cmd, m.exit = nil, nil
		m.remote.resetTransport()
		return err
	}
	m.remote.resetTransport()
	return nil
}

const serviceReadyWait = 30 * time.Second

// waitServiceReady polls an already-installed system service. Unlike a child
// spawn, waiting longer rarely helps: the service is either coming up quickly
// or needs a reinstall.
func (m *Manager) waitServiceReady(timeout time.Duration) error {
	// A loaded service may simply answer slowly; that is not a failed start.
	if m.answersWithin(slowProbeTimeout) {
		return nil
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if m.healthy() {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	if tail := service.LogTail(m.dir); tail != "" {
		return fmt.Errorf(localize.Text("系统服务已安装，但内核没有启动：%s"), tail)
	}
	return errors.New(localize.Text("系统服务已安装但没有响应：请在概览页重新安装服务"))
}

// reapStaleLocked stops an unowned daemon recorded in core.json and clears the
// leftover socket/status files. Caller must hold m.mu.
func (m *Manager) reapStaleLocked() {
	if m.cmd != nil && m.cmd.Process != nil {
		_ = process.Stop(m.cmd.Process)
		if m.exit != nil {
			select {
			case <-m.exit.done:
			case <-time.After(3 * time.Second):
				_ = m.cmd.Process.Kill()
				<-m.exit.done
			}
		}
		m.cmd, m.exit = nil, nil
	}
	st, err := m.Status()
	if err == nil && st.PID > 0 && st.PID != os.Getpid() && process.Alive(st.PID) {
		if p, err := os.FindProcess(st.PID); err == nil {
			_ = process.Stop(p)
			deadline := time.Now().Add(3 * time.Second)
			for process.Alive(st.PID) && time.Now().Before(deadline) {
				time.Sleep(50 * time.Millisecond)
			}
			if process.Alive(st.PID) {
				_ = p.Kill()
				time.Sleep(150 * time.Millisecond)
			}
		}
	}
	_ = os.Remove(m.statusPath)
	_ = os.Remove(m.socket)
}

func (m *Manager) waitReady(exit *processExit, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	poll := time.NewTicker(120 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-exit.done:
			return m.startupError(fmt.Sprintf(localize.Text("内核提前退出 (%v)"), exit.err))
		case <-deadline.C:
			return m.startupError(localize.Text("内核启动超时"))
		case <-poll.C:
			if m.healthy() {
				return nil
			}
		}
	}
}

func (m *Manager) startupError(reason string) error {
	tail, _ := os.ReadFile(m.logPath)
	if len(tail) > 2000 {
		tail = tail[len(tail)-2000:]
	}
	return fmt.Errorf("%s: %s", reason, strings.TrimSpace(string(tail)))
}

// Stop shuts down a daemon this app owns. A system-managed daemon is left
// running; stopping that is the system's job.
func (m *Manager) Stop() error {
	m.mu.Lock()
	cmd := m.cmd
	exit := m.exit
	m.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		if runtime.GOOS == "windows" && service.ForDataDir(m.dir) {
			if err := service.StopRunning(); err != nil {
				return err
			}
			m.remote.resetTransport()
			return nil
		}
		return errNotOwned
	}
	_ = process.Stop(cmd.Process)
	select {
	case <-exit.done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-exit.done
	}
	m.mu.Lock()
	if m.cmd == cmd {
		m.cmd, m.exit = nil, nil
	}
	m.mu.Unlock()
	m.remote.resetTransport()
	return nil
}

// ErrNotOwned says the daemon belongs to the system, not to this app.
var errNotOwned = errors.New("守护进程由系统管理，应用无法直接停止")

// NotOwned reports whether the error means "the system owns it".
func NotOwned(err error) bool { return errors.Is(err, errNotOwned) }
