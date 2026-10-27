// Package core embeds the mihomo kernel into the app process.
//
// There is no sidecar binary and no localhost REST controller: the kernel is
// started with hub.Parse, read through tunnel/statistic, and stopped with
// executor.Shutdown. That is the whole reason this app can be a single
// ~20MB binary with no port to expose.
package core

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/hub"
	"github.com/metacubex/mihomo/hub/executor"
	"github.com/metacubex/mihomo/log"
	"github.com/metacubex/mihomo/tunnel"
	"github.com/metacubex/mihomo/tunnel/statistic"
)

// Sample is one second of kernel accounting.
type Sample struct {
	UpNow     int64
	DownNow   int64
	UpTotal   int64
	DownTotal int64
	Conns     int
	Mem       uint64
}

// Conn is one live connection, flattened for the UI.
type Conn struct {
	ID              string
	Process         string
	ProcessPath     string
	SourceIP        string
	SourcePort      string
	DestinationIP   string
	DestinationPort string
	RulePayload     string
	Chains          []string
	Host            string
	Network         string
	Rule            string
	Chain           string
	Up              int64
	Down            int64
	Start           time.Time
}

// Group is one proxy group with its current selection.
type Group struct {
	Name        string
	Type        string
	Now         string
	Members     []string
	Hidden      bool
	Delays      map[string]uint16
	MemberTypes map[string]string
}

// selectable is what a Selector / URLTest group has in common.
type selectable interface {
	Now() string
	Set(string) error
}

type withMembers interface {
	Proxies() []C.Proxy
}

// Rule is one entry of the routing table.
type Rule struct {
	Type    string
	Payload string
	Adapter string
}

// LogLine is one kernel log record.
type LogLine struct {
	At    time.Time
	Level string
	Text  string
}

// Core owns the kernel lifecycle.
type Core struct {
	mu       sync.RWMutex
	running  bool
	lastErr  string
	started  time.Time
	logs     []LogLine
	logStart sync.Once
}

// New prepares the kernel home. mihomo resolves geoip.dat, geosite.dat,
// cache.db and provider files relative to it, so an empty home breaks every
// GEOIP rule in a real subscription.
func New(home string) *Core {
	if home != "" {
		if err := os.MkdirAll(home, 0o755); err == nil {
			C.SetHomeDir(home)
		}
	}
	return &Core{}
}

// WatchLogs keeps the last few hundred kernel log lines for the UI. It is
// safe to call more than once.
func (c *Core) WatchLogs() {
	c.logStart.Do(func() {
		sub := log.Subscribe()
		go func() {
			for ev := range sub {
				c.mu.Lock()
				c.logs = append(c.logs, LogLine{
					At:    time.Now(),
					Level: strings.ToLower(ev.LogLevel.String()),
					Text:  ev.Payload,
				})
				if len(c.logs) > 400 {
					c.logs = c.logs[len(c.logs)-400:]
				}
				c.mu.Unlock()
			}
		}()
	})
}

// Logs returns the buffered log lines, oldest first.
func (c *Core) Logs() []LogLine {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]LogLine, len(c.logs))
	copy(out, c.logs)
	return out
}

// ClearLogs empties the buffer.
func (c *Core) ClearLogs() {
	c.mu.Lock()
	c.logs = nil
	c.mu.Unlock()
}

// Rules lists the active routing table.
func (c *Core) Rules() []Rule {
	rules := tunnel.Rules()
	out := make([]Rule, 0, len(rules))
	for _, r := range rules {
		if r == nil {
			continue
		}
		out = append(out, Rule{
			Type:    r.RuleType().String(),
			Payload: r.Payload(),
			Adapter: r.Adapter(),
		})
	}
	return out
}

// Delay runs one latency test through a named proxy.
func (c *Core) Delay(name, testURL string) (uint16, error) {
	p, ok := tunnel.Proxies()[name]
	if !ok {
		return 0, fmt.Errorf("没有这个代理: %s", name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	return p.URLTest(ctx, testURL, nil)
}

// Start parses a config and brings every listener up.
func (c *Core) Start(raw []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("内核启动崩溃: %v", r)
		}
	}()

	if err := hub.Parse(raw); err != nil {
		c.setErr(err)
		return err
	}

	c.mu.Lock()
	c.running = true
	c.lastErr = ""
	c.started = time.Now()
	c.mu.Unlock()
	statistic.DefaultManager.ResetStatistic()
	return nil
}

// Stop tears the kernel down; the app keeps running.
func (c *Core) Stop() {
	defer func() { _ = recover() }()
	executor.Shutdown()
	c.mu.Lock()
	c.running = false
	c.mu.Unlock()
}

func (c *Core) Running() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}

func (c *Core) LastError() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastErr
}

func (c *Core) setErr(err error) {
	c.mu.Lock()
	c.lastErr = err.Error()
	c.mu.Unlock()
}

// Sample reads the kernel counters. Blips are per-second, filled by mihomo's
// own one-second ticker, so callers should poll about that often.
func (c *Core) Sample() Sample {
	up, down := statistic.DefaultManager.Now()
	totalUp, totalDown := statistic.DefaultManager.Total()

	conns := 0
	statistic.DefaultManager.Range(func(t statistic.Tracker) bool {
		if info := t.Info(); info != nil && !info.Start.IsZero() {
			conns++
		}
		return true
	})

	return Sample{
		UpNow:     up,
		DownNow:   down,
		UpTotal:   totalUp,
		DownTotal: totalDown,
		Conns:     conns,
		Mem:       statistic.DefaultManager.Memory(),
	}
}

// Connections returns the most recent connections, newest first.
func (c *Core) Connections(limit int) []Conn {
	out := make([]Conn, 0, 32)
	statistic.DefaultManager.Range(func(t statistic.Tracker) bool {
		info := t.Info()
		if info == nil {
			return true
		}
		host := ""
		network := ""
		if info.Metadata != nil {
			host = info.Metadata.Host
			if host == "" && info.Metadata.DstIP.IsValid() {
				host = info.Metadata.DstIP.String()
			}
			network = info.Metadata.NetWork.String()
		}
		out = append(out, Conn{
			Host:    host,
			Network: network,
			Rule:    info.Rule,
			Chain:   info.Chain.String(),
			Up:      info.UploadTotal.Load(),
			Down:    info.DownloadTotal.Load(),
			Start:   info.Start,
		})
		return true
	})

	sort.Slice(out, func(i, j int) bool { return out[i].Start.After(out[j].Start) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Groups lists selectable proxy groups in stable order.
func (c *Core) Groups() []Group {
	proxies := tunnel.Proxies()
	names := make([]string, 0, len(proxies))
	for name := range proxies {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]Group, 0, 8)
	for _, name := range names {
		p := proxies[name]
		if p == nil {
			continue
		}
		sel, ok := p.Adapter().(selectable)
		if !ok {
			continue
		}
		g := Group{Name: name, Type: p.Type().String(), Now: sel.Now()}
		if m, ok := p.Adapter().(withMembers); ok {
			for _, member := range m.Proxies() {
				if member != nil {
					g.Members = append(g.Members, member.Name())
				}
			}
		}
		out = append(out, g)
	}
	return out
}

// Select switches a proxy group to one of its members.
func (c *Core) Select(group, member string) error {
	p, ok := tunnel.Proxies()[group]
	if !ok {
		return fmt.Errorf("没有这个分组: %s", group)
	}
	sel, ok := p.Adapter().(selectable)
	if !ok {
		return fmt.Errorf("%s 不可选择", group)
	}
	return sel.Set(member)
}

// Mode returns the current routing mode as a UI token.
func (c *Core) Mode() string {
	switch tunnel.Mode() {
	case tunnel.Global:
		return "global"
	case tunnel.Direct:
		return "direct"
	default:
		return "rule"
	}
}

// SetMode switches between rule / global / direct.
func (c *Core) SetMode(token string) {
	switch token {
	case "global":
		tunnel.SetMode(tunnel.Global)
	case "direct":
		tunnel.SetMode(tunnel.Direct)
	default:
		tunnel.SetMode(tunnel.Rule)
	}
}

// Proxies returns every outbound name the kernel knows, for diagnostics.
func (c *Core) Counts() (proxies int, rules int, listeners int) {
	return len(tunnel.Proxies()), len(tunnel.Rules()), len(tunnel.Listeners())
}
