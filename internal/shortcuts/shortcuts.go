// Package shortcuts manages the application's system-wide key bindings.
package shortcuts

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"gopurr/internal/localize"
)

type Action int

const (
	ShowWindow Action = iota
	ToggleProxy
	ToggleTUN
	RuleMode
	GlobalMode
	DirectMode
	Count
)

// Config is a value so preference snapshots and editor drafts never alias.
// Empty bindings are disabled; a fresh install claims no system shortcuts.
type Config [Count]string

// Normalize accepts a deliberately small, portable subset of native
// accelerators. Resolving aliases here also catches equivalent duplicates.
func Normalize(raw, goos string) (string, error) {
	if strings.ContainsAny(raw, "\r\n\t") {
		return "", errors.New(localize.Text("快捷键不能包含换行或制表符"))
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) > 80 {
		return "", errors.New(localize.Text("快捷键不能超过 80 个字符"))
	}
	parts := strings.Split(raw, "+")
	mods := map[string]bool{}
	for _, p := range parts[:len(parts)-1] {
		var m string
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "cmdorctrl", "commandorcontrol":
			m = "Ctrl"
			if goos == "darwin" {
				m = "Super"
			}
		case "cmd", "command", "super", "meta", "win":
			m = "Super"
		case "ctrl", "control":
			m = "Ctrl"
		case "alt", "option":
			m = "Alt"
		case "shift":
			m = "Shift"
		default:
			return "", errors.New(localize.Text("请使用 Ctrl、Alt、Shift 或 Cmd 组合"))
		}
		if mods[m] {
			return "", errors.New(localize.Text("修饰键不能重复"))
		}
		mods[m] = true
	}
	if !mods["Super"] && !mods["Ctrl"] && !mods["Alt"] {
		return "", errors.New(localize.Text("至少包含 Ctrl、Alt 或 Cmd 中的一个修饰键"))
	}
	key := strings.ToUpper(strings.TrimSpace(parts[len(parts)-1]))
	valid := len(key) == 1 && (key[0] >= 'A' && key[0] <= 'Z' || key[0] >= '0' && key[0] <= '9')
	if key == "SPACE" {
		key, valid = "Space", true
	}
	if strings.HasPrefix(key, "F") {
		if n, err := strconv.Atoi(key[1:]); err == nil && n >= 1 && n <= 12 && key == "F"+strconv.Itoa(n) {
			valid = true
		}
	}
	if !valid {
		return "", errors.New(localize.Text("按键须为字母、数字、F1–F12 或 Space"))
	}
	var out []string
	for _, m := range []string{"Super", "Ctrl", "Alt", "Shift"} {
		if mods[m] {
			out = append(out, m)
		}
	}
	return strings.Join(append(out, key), "+"), nil
}

func (c Config) Normalized() (Config, error) {
	var next Config
	seen := map[string]bool{}
	for action, value := range c {
		key, err := Normalize(value, runtime.GOOS)
		if err != nil {
			return c, fmt.Errorf("%q: %w", value, err)
		}
		if key != "" && seen[key] {
			return c, fmt.Errorf("%s: %s", key, localize.Text("同一组合不能用于多个动作"))
		}
		seen[key], next[action] = true, key
	}
	return next, nil
}

type Backend interface {
	Register(string, func()) error
	Unregister(string)
}

// Manager is used on the UI thread, like the native callbacks it dispatches.
type Manager struct {
	backend  Backend
	dispatch func(Action)
	active   map[string]Action
}

func New(backend Backend, dispatch func(Action)) *Manager {
	return &Manager{backend: backend, dispatch: dispatch, active: map[string]Action{}}
}

// Prepare registers only new keys, retaining every old registration until
// Commit. A conflict or failed disk write therefore cannot lose old bindings.
// Shared keys stay registered even when two actions exchange accelerators.
func (m *Manager) Prepare(c Config) (*Change, error) {
	next, err := c.Normalized()
	if err != nil {
		return nil, err
	}
	x := &Change{manager: m, next: map[string]Action{}}
	for action, key := range next {
		if key == "" {
			continue
		}
		x.next[key] = Action(action)
		if _, exists := m.active[key]; exists {
			continue
		}
		if err := m.backend.Register(key, func() {
			if action, exists := m.active[key]; exists {
				m.dispatch(action)
			}
		}); err != nil {
			x.Rollback()
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		x.added = append(x.added, key)
	}
	return x, nil
}

type Change struct {
	manager *Manager
	next    map[string]Action
	added   []string
	done    bool
}

func (x *Change) Rollback() {
	if x == nil || x.done {
		return
	}
	for _, key := range x.added {
		x.manager.backend.Unregister(key)
	}
	x.done = true
}

func (x *Change) Commit() {
	if x == nil || x.done {
		return
	}
	m := x.manager
	old := m.active
	m.active = x.next
	for key := range old {
		if _, exists := x.next[key]; !exists {
			m.backend.Unregister(key)
		}
	}
	x.done = true
}

func (m *Manager) Close() {
	for key := range m.active {
		m.backend.Unregister(key)
	}
	m.active = map[string]Action{}
}
