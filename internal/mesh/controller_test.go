package mesh

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeBackend struct {
	mu                        sync.Mutex
	status                    Status
	err, socketErr, switchErr error
	calls                     []string
}

func (b *fakeBackend) Read(context.Context) (Status, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.status, b.err
}
func (b *fakeBackend) Connect(context.Context) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, "connect")
	b.status.Running = true
	b.status.ShieldsUp = true
	return "", nil
}
func (b *fakeBackend) SetIncoming(_ context.Context, allow bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, map[bool]string{true: "allow", false: "block"}[allow])
	if b.switchErr == nil {
		b.status.ShieldsUp = !allow
	}
	return b.switchErr
}
func (b *fakeBackend) Disconnect(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, "disconnect")
	if b.switchErr != nil {
		return b.switchErr
	}
	b.status.Running = false
	return nil
}
func (b *fakeBackend) Accesses(context.Context) (Observation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return Observation{}, b.socketErr
}
func waitIdle(t *testing.T, c *Controller) Snapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s := c.Snapshot()
		if !s.Busy {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("controller did not finish")
	return Snapshot{}
}

func TestManagedStartupClosesAccessAndPersistsPrivately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mesh.json")
	b := &fakeBackend{status: Status{Installed: true, Official: true, Running: true, ShieldsKnown: true, ShieldsUp: true}}
	c := New(b, path, nil)
	c.Do("allow")
	if s := waitIdle(t, c); s.Status.ShieldsUp || !s.Managed {
		t.Fatalf("allow failed: %+v", s)
	}
	assertPrivateMeshFile(t, path)
	// Simulate an unclean exit: the next instance always starts protected.
	next := New(b, path, nil)
	next.Start()
	s := waitIdle(t, next)
	if !s.Status.ShieldsUp {
		t.Fatal("previous session's open access survived restart")
	}
	if err := next.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if err := c.Shutdown(); err != nil {
		t.Fatal(err)
	}
}

func TestReadFailureNeverClaimsProtectionAndMonitorFailureIsVisible(t *testing.T) {
	b := &fakeBackend{status: Status{Installed: true, Official: true, Running: true, ShieldsKnown: true, ShieldsUp: true}}
	c := New(b, "", nil)
	c.Refresh()
	waitIdle(t, c)
	b.mu.Lock()
	b.err = errors.New("service unavailable")
	b.mu.Unlock()
	c.Refresh()
	s := waitIdle(t, c)
	if s.Status.ShieldsKnown || s.Error == "" {
		t.Fatal("stale protection presented as current")
	}
	b.mu.Lock()
	b.err = nil
	b.socketErr = errors.New("cannot monitor")
	b.mu.Unlock()
	c.Refresh()
	s = waitIdle(t, c)
	if s.MonitorError == "" {
		t.Fatal("monitor failure presented as zero visits")
	}
	if err := c.Shutdown(); err != nil {
		t.Fatal(err)
	}
}

func TestFailedShutdownProtectionCanBeRetried(t *testing.T) {
	b := &fakeBackend{status: Status{Installed: true, Official: true, Running: true, ShieldsKnown: true}, switchErr: errors.New("permission denied")}
	c := New(b, "", nil)
	c.Do("block")
	waitIdle(t, c)
	if err := c.Shutdown(); err == nil {
		t.Fatal("quit despite inability to protect")
	}
	b.mu.Lock()
	b.switchErr = nil
	b.mu.Unlock()
	if err := c.Shutdown(); err != nil {
		t.Fatal(err)
	}
}

func TestEmergencyDisconnectCancelsPendingConnect(t *testing.T) {
	b := &cancelBackend{entered: make(chan struct{})}
	c := New(b, "", nil)
	c.Do("connect")
	select {
	case <-b.entered:
	case <-time.After(time.Second):
		t.Fatal("connect did not start")
	}
	c.Do("disconnect")
	s := waitIdle(t, c)
	if s.Status.Running || !b.disconnected {
		t.Fatal("pending connect defeated emergency disconnect")
	}
	_ = c.Shutdown()
}

type cancelBackend struct {
	fakeBackend
	entered      chan struct{}
	disconnected bool
}

func (b *cancelBackend) Connect(ctx context.Context) (string, error) {
	close(b.entered)
	<-ctx.Done()
	return "", ctx.Err()
}
func (b *cancelBackend) Disconnect(context.Context) error { b.disconnected = true; return nil }

func TestUnwritableStateDoesNotOpenAccess(t *testing.T) {
	b := &fakeBackend{}
	c := New(b, filepath.Join(t.TempDir(), "missing", "mesh.json"), nil)
	c.Do("allow")
	s := waitIdle(t, c)
	if s.Error == "" || len(b.calls) != 0 {
		t.Fatal("opened access without persistence")
	}
	// Closing access must remain possible even when local storage is broken.
	c.Do("block")
	waitIdle(t, c)
	if len(b.calls) != 1 || b.calls[0] != "block" {
		t.Fatal("storage failure disabled emergency protection")
	}
	_ = c.Shutdown()
}

func TestCorruptJournalStillProtectsOnStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mesh.json")
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	b := &fakeBackend{status: Status{Installed: true, Official: true, Running: true, ShieldsKnown: true}}
	c := New(b, path, nil)
	if !c.Snapshot().Managed {
		t.Fatal("corrupt safety state treated as unmanaged")
	}
	c.Start()
	s := waitIdle(t, c)
	if !s.Status.ShieldsUp {
		t.Fatal("corrupt journal left access open")
	}
	_ = c.Shutdown()
}

func TestInitialPartialStatusStillShowsInstalledClient(t *testing.T) {
	b := &fakeBackend{status: Status{Installed: true, Running: true, State: "Running"}, err: errors.New("cannot read protection")}
	c := New(b, "", nil)
	c.Refresh()
	s := waitIdle(t, c)
	if !s.Status.Installed || s.Status.ShieldsKnown || s.Error == "" {
		t.Fatalf("partial status lost: %+v", s)
	}
	_ = c.Shutdown()
}

func TestOptInStartupReconnectNeverRestoresOpenAccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mesh.json")
	b := &fakeBackend{status: Status{Installed: true, Official: true, ShieldsKnown: true, ShieldsUp: true}}
	c := New(b, path, nil)
	c.Do("connect")
	waitIdle(t, c)
	c.SetAutoConnect(true)
	b.mu.Lock()
	b.status.ShieldsUp = false
	b.calls = nil
	b.mu.Unlock()
	next := New(b, path, nil)
	if !next.Snapshot().AutoConnect {
		t.Fatal("auto-connect preference lost")
	}
	next.Start()
	s := waitIdle(t, next)
	b.mu.Lock()
	calls := append([]string(nil), b.calls...)
	b.mu.Unlock()
	if !s.Status.ShieldsUp || len(calls) != 1 || calls[0] != "connect" {
		t.Fatalf("unsafe reconnect: %+v %v", s, calls)
	}
	next.SetAutoConnect(false)
	b.mu.Lock()
	b.calls = nil
	b.mu.Unlock()
	manual := New(b, path, nil)
	manual.Start()
	waitIdle(t, manual)
	b.mu.Lock()
	calls = append([]string(nil), b.calls...)
	b.mu.Unlock()
	if len(calls) != 1 || calls[0] != "block" {
		t.Fatal("reconnected with preference disabled")
	}
	manual.Shutdown()
	next.Shutdown()
	c.Shutdown()
}

func TestAutoConnectPreferenceRollsBackOnStorageFailure(t *testing.T) {
	c := New(&fakeBackend{}, filepath.Join(t.TempDir(), "missing", "mesh.json"), nil)
	c.SetAutoConnect(true)
	if s := c.Snapshot(); s.AutoConnect || s.Error == "" {
		t.Fatal("enabled unsaved startup connection")
	}
	c.Shutdown()
}
