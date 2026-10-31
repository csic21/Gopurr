package kernel

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopurr/internal/core"
)

// Kernel is what the UI talks to: a daemon plus its controller, with small
// caches so the每秒重绘 doesn't turn into每秒请求.
type Kernel struct {
	mgr *Manager
	r   *Remote

	watchOnce  sync.Once
	setupOnce  sync.Once
	startMu    sync.Mutex
	mu         sync.Mutex
	lastErr    string
	groups     []core.Group
	groupsAt   time.Time
	groupOrder []string
	rules      []core.Rule
	rulesAt    time.Time
	mode       string
	modeAt     time.Time
	counts     [3]int
	countsAt   time.Time
}

func New(mgr *Manager) *Kernel {
	k := &Kernel{mgr: mgr, r: mgr.Remote()}
	if raw, err := os.ReadFile(mgr.ConfigPath()); err == nil {
		k.groupOrder = configuredGroupOrder(raw)
	}
	return k
}

// WatchLogs subscribes to the daemon's traffic and log streams.
func (k *Kernel) WatchLogs() {
	k.watchOnce.Do(func() { k.r.Watch() })
}

func (k *Kernel) Running() bool {
	return k.mgr.Running()
}

// IsRoot reports whether the daemon can create a TUN device.
func (k *Kernel) IsRoot() bool { return k.mgr.IsRoot() }

func (k *Kernel) LastError() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.lastErr
}

func (k *Kernel) setErr(err error) {
	k.mu.Lock()
	k.lastErr = err.Error()
	k.mu.Unlock()
}

// Start writes the config and asks the daemon to run it, spawning the daemon
// when the app owns it.
func (k *Kernel) Start(cfg []byte) error {
	k.startMu.Lock()
	defer k.startMu.Unlock()
	path := k.mgr.ConfigPath()
	previous, readErr := os.ReadFile(path)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		k.setErr(readErr)
		return readErr
	}
	if err := writeRuntimeConfig(path, cfg); err != nil {
		k.setErr(err)
		return err
	}
	rollback := func(err error) error {
		var restoreErr error
		if errors.Is(readErr, os.ErrNotExist) {
			restoreErr = os.Remove(path)
		} else {
			restoreErr = writeRuntimeConfig(path, previous)
		}
		err = errors.Join(err, restoreErr)
		k.setErr(err)
		return err
	}
	if err := k.mgr.Ensure(); err != nil {
		return rollback(err)
	}
	k.WatchLogs()
	if err := k.r.Reload(k.mgr.ConfigPath()); err != nil {
		return rollback(err)
	}
	order := configuredGroupOrder(cfg)
	k.mu.Lock()
	k.lastErr = ""
	k.groupOrder = order
	k.mu.Unlock()
	k.invalidate()
	return nil
}

func writeRuntimeConfig(path string, cfg []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".runtime-*.yaml")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(cfg); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Stop shuts the daemon down when this app started it.
func (k *Kernel) Stop() error {
	err := k.mgr.Stop()
	if err != nil {
		k.setErr(err)
		return err
	}
	k.invalidate()
	return nil
}

func (k *Kernel) Sample() core.Sample { return k.r.Sample() }

func (k *Kernel) SetUIActive(active bool) { k.r.SetUIActive(active) }

func (k *Kernel) Connections(limit int) []core.Conn { return k.r.Connections(limit) }

func (k *Kernel) Groups() []core.Group {
	k.mu.Lock()
	if time.Since(k.groupsAt) < 2*time.Second && k.groups != nil {
		out := k.groups
		k.mu.Unlock()
		return out
	}
	k.mu.Unlock()

	groups := k.r.Groups()
	k.mu.Lock()
	if len(k.groupOrder) > 0 {
		sortGroups(groups, k.groupOrder)
	}
	k.groups, k.groupsAt = groups, time.Now()
	k.mu.Unlock()
	return groups
}

func (k *Kernel) Rules() []core.Rule {
	k.mu.Lock()
	if time.Since(k.rulesAt) < 5*time.Second && k.rules != nil {
		out := k.rules
		k.mu.Unlock()
		return out
	}
	k.mu.Unlock()

	rules := k.r.Rules()
	k.mu.Lock()
	k.rules, k.rulesAt = rules, time.Now()
	k.mu.Unlock()
	return rules
}

func (k *Kernel) Select(group, member string) error {
	err := k.r.Select(group, member)
	if err == nil {
		k.invalidate()
	}
	return err
}

func (k *Kernel) Mode() string {
	k.mu.Lock()
	if time.Since(k.modeAt) < 2*time.Second && k.mode != "" {
		out := k.mode
		k.mu.Unlock()
		return out
	}
	k.mu.Unlock()

	mode := k.r.Mode()
	k.mu.Lock()
	k.mode, k.modeAt = mode, time.Now()
	k.mu.Unlock()
	return mode
}

func (k *Kernel) SetMode(mode string) error {
	if err := k.r.SetMode(mode); err != nil {
		return err
	}
	k.mu.Lock()
	k.mode, k.modeAt = mode, time.Now()
	k.mu.Unlock()
	return nil
}

func (k *Kernel) Delay(name, testURL string) (uint16, error) { return k.r.Delay(name, testURL) }

func (k *Kernel) DelayContext(ctx context.Context, name, testURL string, timeoutMS int) (uint16, error) {
	return k.r.DelayContext(ctx, name, testURL, timeoutMS)
}

func (k *Kernel) Logs() []core.LogLine { return k.r.Logs() }

func (k *Kernel) ClearLogs() { k.r.ClearLogs() }

func (k *Kernel) Counts() (int, int, int) {
	k.mu.Lock()
	if time.Since(k.countsAt) < 5*time.Second && k.countsAt != (time.Time{}) {
		out := k.counts
		k.mu.Unlock()
		return out[0], out[1], out[2]
	}
	k.mu.Unlock()

	p, r, l := k.r.Counts()
	k.mu.Lock()
	k.counts, k.countsAt = [3]int{p, r, l}, time.Now()
	k.mu.Unlock()
	return p, r, l
}

// Shutdown is the app's exit path: stop what we own, leave system daemons be.
// ReclaimStale covers the case where this UI session never owned m.cmd (for
// example an adopted daemon) but core.json still names a process holding
// mixed-port — important after in-app update relaunch.
func (k *Kernel) Shutdown() {
	_ = k.mgr.Stop()
	k.mgr.ReclaimStale()
}

func (k *Kernel) invalidate() {
	k.mu.Lock()
	k.groupsAt = time.Time{}
	k.rulesAt = time.Time{}
	k.modeAt = time.Time{}
	k.countsAt = time.Time{}
	k.mu.Unlock()
}

func (k *Kernel) ConnectionsWithError() ([]core.Conn, error) { return k.r.ConnectionsWithError() }
func (k *Kernel) CloseConnection(id string) error            { return k.r.CloseConnection(id) }
func (k *Kernel) CloseAllConnections() error                 { return k.r.CloseAllConnections() }
