package mesh

import "gopurr/internal/localize"

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"gopurr/internal/securefs"
)

type diskState struct {
	Managed     bool
	Visits      []Visit
	AutoConnect bool
}

type Controller struct {
	mu      sync.Mutex
	op      sync.Mutex
	backend Backend
	path    string
	wake    func()
	s       Snapshot
	closed  bool
	stop    chan struct{}
	cancel  context.CancelFunc
	serial  uint64
}

func New(backend Backend, path string, wake func()) *Controller {
	c := &Controller{backend: backend, path: path, wake: wake, stop: make(chan struct{})}
	if path != "" {
		if err := securefs.PrivateFile(path); err == nil {
			// An existing unreadable/corrupt safety journal is not evidence of
			// an unmanaged client. Conservatively protect it on startup.
			c.s.Managed = true
			if st, err := os.Stat(path); err == nil && st.Size() <= 1<<20 {
				var d diskState
				b, _ := os.ReadFile(path)
				if json.Unmarshal(b, &d) == nil {
					c.s.Managed, c.s.Visits, c.s.AutoConnect = d.Managed, d.Visits, d.AutoConnect
				} else {
					c.s.Error = localize.Text("本地组网记录损坏，启动后将重新关闭入站访问")
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			c.s.Managed = true
			c.s.Error = localize.Text("无法读取本地组网安全状态，启动后将尝试关闭入站访问")
		}
	}
	if len(c.s.Visits) > 100 {
		c.s.Visits = c.s.Visits[:100]
	}
	for i := range c.s.Visits {
		c.s.Visits[i].Active = false
	}
	return c
}

func (c *Controller) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.s
	s.Visits = append([]Visit(nil), s.Visits...)
	s.Status.Peers = append([]Device(nil), s.Status.Peers...)
	s.Status.Health = append([]string(nil), s.Status.Health...)
	s.Status.RouteIPs = slices.Clone(s.Status.RouteIPs)
	return s
}

func (c *Controller) notify() {
	if c.wake != nil {
		c.wake()
	}
}

// Start is lazy for an untouched installation; managed clients resume polling
// even while the window is hidden. Startup always closes previously open access.
func (c *Controller) Start() {
	c.mu.Lock()
	if c.s.Started || c.closed {
		c.mu.Unlock()
		return
	}
	c.s.Started = true
	managed, auto := c.s.Managed, c.s.AutoConnect
	c.mu.Unlock()
	if managed && auto {
		c.Do("connect")
	} else if managed {
		c.Do("block")
	} else {
		c.Refresh()
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		lastRefresh := time.Now()
		for {
			select {
			case <-c.stop:
				return
			case <-ticker.C:
				s := c.Snapshot().Status
				waitingForLogin := s.State == "Starting" || s.State == "NeedsLogin"
				if waitingForLogin || time.Since(lastRefresh) >= 5*time.Second {
					c.Refresh()
					lastRefresh = time.Now()
				}
			}
		}
	}()
}

func (c *Controller) SetAutoConnect(enabled bool) {
	c.mu.Lock()
	previous := c.s.AutoConnect
	c.s.AutoConnect = enabled
	if err := c.saveLocked(); err != nil {
		c.s.AutoConnect = previous
		c.s.Error = localize.Text("自动连接设置保存失败，请重试")
	}
	c.mu.Unlock()
	c.notify()
}

func (c *Controller) Probe(ctx context.Context, id, protocol string, port uint16) ProbeResult {
	if b, ok := c.backend.(interface {
		Probe(context.Context, string, string, uint16) ProbeResult
	}); ok {
		return b.Probe(ctx, id, protocol, port)
	}
	return ProbeResult{DeviceID: id, Port: port, State: "unknown", Message: localize.Text("当前组网内核不支持连接检测"), Checked: time.Now()}
}

func (c *Controller) Refresh() { c.work("refresh", false) }
func (c *Controller) Do(action string) {
	if action != "connect" && action != "allow" && action != "block" && action != "disconnect" {
		return
	}
	if action == "block" || action == "disconnect" {
		if b, ok := c.backend.(interface{ BlockNow() }); ok {
			b.BlockNow()
		}
	}
	c.work(action, action == "disconnect" || action == "block")
}

func (c *Controller) work(action string, interrupt bool) {
	c.mu.Lock()
	if c.closed || (c.s.Busy && !interrupt) {
		c.mu.Unlock()
		return
	}
	if c.cancel != nil {
		c.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	c.cancel = cancel
	c.serial++
	id := c.serial
	c.s.Busy, c.s.Action = true, action
	c.mu.Unlock()
	c.notify()
	go func() {
		defer cancel()
		c.op.Lock()
		defer c.op.Unlock()
		if ctx.Err() != nil {
			return
		}
		err := c.perform(ctx, action)
		c.mu.Lock()
		if c.serial == id {
			c.s.Busy, c.s.Action = false, ""
			if err != nil {
				c.s.Error = err.Error()
			}
		}
		c.mu.Unlock()
		c.notify()
	}()
}

func (c *Controller) SetServices(raw string, allow bool) {
	action := "services:"
	if allow {
		action = "open:"
	}
	c.work(action+raw, true)
}

func (c *Controller) SetDeviceBlocked(id string, blocked bool) {
	action := "trust:"
	if blocked {
		action = "deny:"
	}
	if blocked {
		if b, ok := c.backend.(interface{ BlockDeviceNow(string) }); ok {
			b.BlockDeviceNow(id)
		}
	}
	c.work(action+id, blocked)
}

type policyBackend interface {
	SetServices(context.Context, string) error
	SetDeviceBlocked(context.Context, string, bool) error
}

func (c *Controller) saveLocked() error {
	if c.path == "" {
		return nil
	}
	// Atomic replacements inherit their directory's Windows DACL. Tighten it
	// before creating the first journal, including when used without App.New.
	// Preserve the fail-closed behavior when the data directory is unavailable.
	dir := filepath.Dir(c.path)
	if _, err := os.Stat(dir); err != nil {
		return err
	}
	if err := securefs.PrivateDir(dir); err != nil {
		return err
	}
	b, err := json.Marshal(diskState{Managed: c.s.Managed, Visits: c.s.Visits, AutoConnect: c.s.AutoConnect})
	if err != nil {
		return err
	}
	return securefs.WriteFile(c.path, b, 0600)
}

func (c *Controller) perform(ctx context.Context, action string) error {
	if action != "refresh" {
		c.mu.Lock()
		c.s.Managed = true
		err := c.saveLocked()
		c.mu.Unlock()
		if err != nil && action != "block" && action != "disconnect" {
			return errors.New(localize.Text("无法保存组网安全状态，未执行操作"))
		}
	}
	var actionErr error
	var auth string
	switch action {
	case "connect":
		auth, actionErr = c.backend.Connect(ctx)
	case "allow":
		actionErr = c.backend.SetIncoming(ctx, true)
	case "block":
		actionErr = c.backend.SetIncoming(ctx, false)
	case "disconnect":
		actionErr = c.backend.Disconnect(ctx)
	default:
		if p, ok := c.backend.(policyBackend); ok {
			kind, value, _ := strings.Cut(action, ":")
			switch kind {
			case "services", "open":
				actionErr = p.SetServices(ctx, value)
				if actionErr == nil && kind == "open" {
					actionErr = c.backend.SetIncoming(ctx, true)
				}
			case "deny", "trust":
				actionErr = p.SetDeviceBlocked(ctx, value, kind == "deny")
			}
		}
	}
	s, err := c.backend.Read(ctx)
	if ctx.Err() != nil {
		if actionErr != nil {
			return actionErr
		}
		return ctx.Err()
	}
	c.mu.Lock()
	if err != nil {
		// Retain last-known device rows but never claim stale protection is live.
		if !s.Installed || s.State != "" {
			c.s.Status = s
		}
		c.s.Status.Installed = s.Installed
		c.s.Status.ShieldsKnown = false
		c.mu.Unlock()
		return errors.Join(actionErr, err)
	}
	if auth != "" && ValidAuthURL(auth) && !s.Running {
		s.AuthURL = auth
	}
	c.s.Status, c.s.Updated = s, time.Now()
	c.s.Error = ""
	c.mu.Unlock()
	var observation Observation
	if s.Running {
		observation, err = c.backend.Accesses(ctx)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.s.MonitorError = err.Error()
		return actionErr
	}
	c.s.MonitorError = ""
	c.s.MonitorScope = observation.Scope
	visits := observation.Visits
	previous := c.s.Visits
	for _, old := range previous {
		found := false
		for _, v := range visits {
			if old.Key == v.Key && old.FirstSeen == v.FirstSeen {
				found = true
				break
			}
		}
		if !found {
			old.Active = false
			visits = append(visits, old)
		}
	}
	if len(visits) > 100 {
		visits = visits[:100]
	}
	c.s.Visits = visits
	if c.s.Managed {
		if err := c.saveLocked(); err != nil {
			c.s.MonitorError = localize.Text("访问记录无法保存，当前仅显示内存中的记录")
		}
	}
	return actionErr
}

// Shutdown closes the owned node as well as protecting active local services.
func (c *Controller) Shutdown() error {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
	}
	managed := c.s.Managed
	c.mu.Unlock()
	c.op.Lock()
	defer c.op.Unlock()
	if managed {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err := c.backend.SetIncoming(ctx, false)
		if err != nil {
			err = c.backend.Disconnect(ctx)
		}
		if owned, ok := c.backend.(*Embedded); ok && err == nil {
			err = owned.Disconnect(ctx)
		}
		cancel()
		if err != nil {
			return err
		}
	}
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		close(c.stop)
	}
	c.mu.Unlock()
	return nil
}

func (c *Controller) ClearHistory() {
	if b, ok := c.backend.(interface{ ClearHistory() }); ok {
		b.ClearHistory()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	kept := c.s.Visits[:0]
	for _, v := range c.s.Visits {
		if v.Active {
			kept = append(kept, v)
		}
	}
	c.s.Visits = kept
	if err := c.saveLocked(); err != nil {
		c.s.Error = localize.Text("历史记录清理未能保存")
	}
}
