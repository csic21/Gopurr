package kernel

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
	remote *Remote
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
	return err == nil && (st.SecurityVersion < core.SecurityVersion || (buildinfo.Version != "dev" && st.Version != buildinfo.Version))
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
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if m.healthy() {
				return nil
			}
			time.Sleep(150 * time.Millisecond)
		}
		return errors.New("系统服务已安装但没有响应：请重试，或在概览页重新安装")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.healthy() {
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logFile, err := os.OpenFile(m.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
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
	_ = logFile.Close()
	go func() { _ = cmd.Wait() }()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if m.healthy() {
			return nil
		}
		time.Sleep(120 * time.Millisecond)
	}
	tail, _ := os.ReadFile(m.logPath)
	if len(tail) > 800 {
		tail = tail[len(tail)-800:]
	}
	return fmt.Errorf("守护进程没有起来: %s", strings.TrimSpace(string(tail)))
}

// Stop shuts down a daemon this app owns. A system-managed daemon is left
// running; stopping that is the system's job.
func (m *Manager) Stop() error {
	m.mu.Lock()
	cmd := m.cmd
	m.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return errNotOwned
	}
	_ = process.Stop(cmd.Process)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !m.healthy() {
			m.mu.Lock()
			m.cmd = nil
			m.mu.Unlock()
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	m.mu.Lock()
	m.cmd = nil
	m.mu.Unlock()
	return nil
}

// ErrNotOwned says the daemon belongs to the system, not to this app.
var errNotOwned = errors.New("守护进程由系统管理，应用无法直接停止")

// NotOwned reports whether the error means "the system owns it".
func NotOwned(err error) bool { return errors.Is(err, errNotOwned) }
