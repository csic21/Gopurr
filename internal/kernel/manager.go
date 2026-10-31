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

func (m *Manager) healthy() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	return m.remote.Probe(ctx) == nil
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
	return err == nil && st.UID == 0
}

// ServiceNeedsUpdate leaves the existing service running until the user
// explicitly authorizes reinstalling it with the new application binary.
func (m *Manager) ServiceNeedsUpdate() bool {
	if !service.Installed() {
		return false
	}
	st, err := m.Status()
	return err == nil && (st.SecurityVersion < core.SecurityVersion || st.ControllerVersion < core.ControllerVersion || (buildinfo.Version != "dev" && st.Version != buildinfo.Version))
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
	if service.Installed() {
		// An unhealthy installed service should fail fast so the UI can point
		// at reinstall instead of looking frozen for a long poll.
		return m.waitServiceReady(serviceReadyWait)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.healthy() {
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

const serviceReadyWait = 2 * time.Second

// waitServiceReady polls an already-installed system service. Unlike a child
// spawn, waiting longer rarely helps: the service is either coming up quickly
// or needs a reinstall.
func (m *Manager) waitServiceReady(timeout time.Duration) error {
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
