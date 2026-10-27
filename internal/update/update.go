// Package update coordinates signed application updates without changing the
// running proxy. mygo verifies and stages the archive before reporting success.
package update

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

type Phase string

const (
	Idle        Phase = "idle"
	Checking    Phase = "checking"
	Current     Phase = "current"
	Available   Phase = "available"
	Downloading Phase = "downloading"
	Ready       Phase = "ready"
	Failed      Phase = "failed"
)

type State struct {
	Phase                 Phase
	Enabled, Visible      bool
	Version, Notes, Error string
	Downloaded, Total     int64
	LastCheck             time.Time
}

type Release struct {
	Version, Notes string
	Install        func(context.Context, func(int64, int64)) error
}

type Backend interface {
	Enabled() bool
	Check(context.Context) (*Release, error)
}

type Native struct{}

func (Native) Enabled() bool { return mygo.Updater.Enabled() }
func (Native) Check(ctx context.Context) (*Release, error) {
	up, err := mygo.Updater.Check(ctx)
	if err != nil || up == nil {
		return nil, err
	}
	return &Release{Version: up.Version, Notes: up.Notes, Install: up.Install}, nil
}

type Controller struct {
	mu          sync.Mutex
	backend     Backend
	state       State
	release     *Release
	cancel      context.CancelFunc
	closed      bool
	changed     func()
	lastAttempt time.Time
}

func New(backend Backend, changed func()) *Controller {
	return &Controller{backend: backend, changed: changed, state: State{Phase: Idle, Enabled: backend.Enabled()}}
}

func (c *Controller) Snapshot() State { c.mu.Lock(); defer c.mu.Unlock(); return c.state }

func (c *Controller) notify() {
	c.mu.Lock()
	fn, closed := c.changed, c.closed
	c.mu.Unlock()
	if fn != nil && !closed {
		fn()
	}
}

func (c *Controller) Show() {
	c.mu.Lock()
	c.state.Visible = true
	c.mu.Unlock()
	c.notify()
}

func (c *Controller) Hide() {
	c.mu.Lock()
	c.state.Visible = false
	c.mu.Unlock()
	c.notify()
}

// Check never installs an update. Concurrent manual/background checks share one
// request; a manual check makes the current operation visible.
func (c *Controller) Check(manual bool) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	if manual {
		c.state.Visible = true
	}
	if c.state.Phase == Checking || c.state.Phase == Downloading || c.state.Phase == Ready {
		c.mu.Unlock()
		c.notify()
		return
	}
	if !c.state.Enabled {
		c.state.Phase, c.state.Error = Failed, "请安装正式发布包，并将应用放在可写的安装目录中。"
		c.mu.Unlock()
		c.notify()
		return
	}
	c.state.Phase, c.state.Error = Checking, ""
	c.lastAttempt = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	c.cancel = cancel
	c.mu.Unlock()
	c.notify()
	go func() {
		defer cancel()
		release, err := c.backend.Check(ctx)
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return
		}
		c.cancel = nil
		switch {
		case err != nil:
			c.state.Phase = Failed
			if !errors.Is(err, context.Canceled) {
				c.state.Error = err.Error()
			}
		case release == nil:
			c.state.Phase, c.state.Version, c.state.Notes = Current, "", ""
			c.release = nil
			c.state.LastCheck = time.Now()
		default:
			c.state.Phase, c.state.Version, c.state.Notes = Available, release.Version, release.Notes
			c.state.LastCheck, c.state.Visible, c.release = time.Now(), true, release
		}
		c.mu.Unlock()
		c.notify()
	}()
}

// Install is only called by the explicit download/install button. Cancellation
// and signature failures keep the release available for a later retry.
func (c *Controller) Install() {
	c.mu.Lock()
	if c.closed || c.state.Phase != Available || c.release == nil || c.release.Install == nil {
		c.mu.Unlock()
		return
	}
	release := c.release
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	c.cancel = cancel
	c.state.Phase, c.state.Error, c.state.Downloaded, c.state.Total = Downloading, "", 0, 0
	c.mu.Unlock()
	c.notify()
	go func() {
		defer cancel()
		err := release.Install(ctx, func(downloaded, total int64) {
			c.mu.Lock()
			c.state.Downloaded, c.state.Total = downloaded, total
			c.mu.Unlock()
			c.notify()
		})
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return
		}
		c.cancel = nil
		if err != nil {
			c.state.Phase = Available
			if !errors.Is(err, context.Canceled) {
				c.state.Error = err.Error()
			}
		} else {
			c.state.Phase = Ready
		}
		c.mu.Unlock()
		c.notify()
	}()
}

func (c *Controller) Cancel() {
	c.mu.Lock()
	cancel := c.cancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (c *Controller) Close() {
	c.mu.Lock()
	c.closed = true
	cancel := c.cancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Run checks shortly after startup, then daily. Failed checks retry after an
// hour. Closing the window does not stop checks; quitting the app does.
func (c *Controller) Run(ctx context.Context, automatic func() bool) {
	first := time.NewTimer(10 * time.Second)
	defer first.Stop()
	select {
	case <-ctx.Done():
		return
	case <-first.C:
	}
	if automatic() {
		c.Check(false)
	}
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !automatic() {
				continue
			}
			c.mu.Lock()
			state, last := c.state, c.lastAttempt
			c.mu.Unlock()
			if !state.Enabled || state.Phase == Available || state.Phase == Ready {
				continue
			}
			interval := 24 * time.Hour
			if state.Phase == Failed {
				interval = time.Hour
			}
			if time.Since(last) >= interval {
				c.Check(false)
			}
		}
	}
}
