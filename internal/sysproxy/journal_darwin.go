//go:build darwin

package sysproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
	"gopurr/internal/securefs"
)

type savedProxy struct {
	Enabled bool
	Server  string
	Port    int
}

type savedLease struct {
	Original    [3]savedProxy
	Bypass      []string
	Ports       map[int]bool
	Dirty       [3]bool
	Restoring   [3]bool
	BypassDirty bool
}

type leaseJournal struct {
	OwnerPID int
	Services map[string]savedLease
}

// Serialize a surviving watcher with a newly launched UI. The ownership check
// and OS restore must be one operation, so the old watcher cannot race takeover.
func lockJournal(path string) (func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return lockJournalContext(ctx, path)
}

func lockJournalContext(ctx context.Context, path string) (func(), error) {
	if path == "" {
		return func() {}, nil
	}
	// Enable starts the recovery watcher while holding this lock. Inheriting
	// its descriptor would keep the lock alive after the UI closes its copy,
	// making Restore wait for the watcher that is itself waiting for UI exit.
	fd, err := unix.Open(path+".lock", unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	for {
		err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() { _ = unix.Close(fd) }, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EINTR) {
			unix.Close(fd)
			return nil, err
		}
		select {
		case <-ctx.Done():
			unix.Close(fd)
			return nil, fmt.Errorf("等待系统代理恢复操作超时，请稍后重试: %w", ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func (l *proxyLeases) persist() error {
	if l.journal == "" {
		return nil
	}
	if len(l.services) == 0 {
		err := os.Remove(l.journal)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	saved := make(map[string]savedLease)
	for service, lease := range l.services {
		entry := savedLease{Bypass: lease.bypass, Ports: lease.ports, Dirty: lease.dirty,
			Restoring: lease.restoring, BypassDirty: lease.bypassDirty}
		for i, original := range lease.original {
			entry.Original[i] = savedProxy{original.enabled, original.server, original.port}
		}
		saved[service] = entry
	}
	raw, err := json.Marshal(leaseJournal{OwnerPID: l.owner, Services: saved})
	if err != nil {
		return err
	}
	return securefs.WriteFile(l.journal, raw, 0600)
}

func loadLeases(path string, command commandRunner) (*proxyLeases, error) {
	l := &proxyLeases{command: command, services: make(map[string]*proxyLease), journal: path}
	if path == "" {
		return l, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return nil, err
	}
	if err := securefs.PrivateFile(path); err != nil {
		return nil, err
	}
	var saved leaseJournal
	if err := json.Unmarshal(raw, &saved); err != nil {
		return nil, fmt.Errorf("系统代理恢复记录损坏: %w", err)
	}
	if saved.OwnerPID <= 1 || len(saved.Services) == 0 {
		return nil, errors.New("系统代理恢复记录缺少有效会话或网络服务")
	}
	l.owner = saved.OwnerPID
	for service, entry := range saved.Services {
		lease := &proxyLease{bypass: entry.Bypass, ports: entry.Ports, dirty: entry.Dirty,
			restoring: entry.Restoring, bypassDirty: entry.BypassDirty}
		if lease.ports == nil {
			lease.ports = make(map[int]bool)
		}
		for i, original := range entry.Original {
			if original.Enabled {
				if err := validateProxy(original.Server, original.Port); err != nil {
					return nil, fmt.Errorf("系统代理恢复记录无效: %w", err)
				}
			}
			lease.original[i] = proxySetting{original.Enabled, original.Server, original.Port}
		}
		l.services[service] = lease
	}
	return l, nil
}
