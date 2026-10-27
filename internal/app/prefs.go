package app

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Prefs are the settings that belong to the window, not to the kernel. They
// live beside the profiles, but in their own file: a kernel config and a UI
// preference are different things and should not share a schema.
type Prefs struct {
	CheckAppUpdates bool `json:"checkAppUpdates"`
	// Appearance controls both the native window and its content.
	Appearance string `json:"appearance"`
	// DockVisibility controls the macOS Dock independently of window release.
	DockVisibility string `json:"dockVisibility"`
	// LightMode releases the interface after the window is closed, the way
	// Magpie's 轻量模式 does: the kernel and the system proxy keep running.
	LightMode bool `json:"lightMode"`
	// LightDelay is how long the closed window stays in memory before it is
	// destroyed, in seconds.
	LightDelay int `json:"lightDelaySeconds"`
	// AutoUpdateHours refreshes subscription profiles every N hours; 0 is off.
	AutoUpdateHours int `json:"autoUpdateHours"`
	// LastAutoUpdate is when the last automatic refresh finished.
	LastAutoUpdate time.Time `json:"lastAutoUpdate"`
	// RestoreSystemProxy enables the system proxy after the kernel is ready.
	RestoreSystemProxy bool `json:"restoreSystemProxy"`
	// Test settings apply to manual node latency tests.
	TestURL         string `json:"testURL"`
	TestTimeoutMS   int    `json:"testTimeoutMS"`
	TestConcurrency int    `json:"testConcurrency"`
}

func defaultPrefs() Prefs {
	return Prefs{
		CheckAppUpdates: true,
		Appearance:      AppearanceSystem,
		DockVisibility:  DockShown,
		LightDelay:      60,
		TestURL:         "https://www.gstatic.com/generate_204",
		TestTimeoutMS:   6000,
		TestConcurrency: 8,
	}
}

func (a *App) loadPrefs() {
	a.prefs = defaultPrefs()
	if a.prefsPath == "" {
		return
	}
	b, err := os.ReadFile(a.prefsPath)
	if err != nil {
		return
	}
	// Decode over defaults: adding a preference must not erase settings from
	// an older file, or give a newly added field its unusable zero value.
	p := defaultPrefs()
	if json.Unmarshal(b, &p) != nil {
		return
	}
	if !validAppearance(p.Appearance) {
		p.Appearance = AppearanceSystem
	}
	if !validDockVisibility(p.DockVisibility) {
		p.DockVisibility = DockShown
	}
	if p.LightDelay <= 0 {
		p.LightDelay = defaultPrefs().LightDelay
	}
	if p.AutoUpdateHours < 0 {
		p.AutoUpdateHours = 0
	}
	p.TestURL = strings.TrimSpace(p.TestURL)
	if validateTestURL(p.TestURL) != nil {
		p.TestURL = defaultPrefs().TestURL
	}
	if p.TestTimeoutMS < 1000 || p.TestTimeoutMS > 30000 {
		p.TestTimeoutMS = defaultPrefs().TestTimeoutMS
	}
	if p.TestConcurrency < 1 || p.TestConcurrency > 32 {
		p.TestConcurrency = defaultPrefs().TestConcurrency
	}
	a.prefs = p
}

// Prefs returns a copy, safe to read from any goroutine.
func (a *App) Prefs() Prefs {
	a.pmu.Lock()
	defer a.pmu.Unlock()
	return a.prefs
}

// UpdatePrefs persists before publishing. A failed write leaves the current
// preferences and lifecycle hook untouched. Existing callers may ignore the
// returned error; settings controls display it to the user.
func (a *App) UpdatePrefs(p Prefs) error {
	return a.changePrefs(func(cur *Prefs) { *cur = p })
}

// changePrefs folds a set of edits into the latest value under one lock. In
// particular, UI edits must preserve the auto-update loop's latest timestamp.
func (a *App) changePrefs(edit func(*Prefs)) error {
	a.pmu.Lock()
	p := a.prefs
	edit(&p)
	p = normalizePrefs(p)
	if err := validatePrefs(p); err != nil {
		a.pmu.Unlock()
		return err
	}
	if err := writeSettingsFile(a.prefsPath, p); err != nil {
		a.pmu.Unlock()
		return fmt.Errorf("无法保存偏好: %w", err)
	}
	a.prefs = p
	hook := a.prefsHook
	a.pmu.Unlock()

	if hook != nil {
		hook(p)
	}
	return nil
}

// Zero-valued new fields remain compatible with callers constructing Prefs
// literals. The settings form validates explicit inputs before this step.
func normalizePrefs(p Prefs) Prefs {
	d := defaultPrefs()
	if p.Appearance == "" {
		p.Appearance = d.Appearance
	}
	if p.DockVisibility == "" {
		p.DockVisibility = d.DockVisibility
	}
	if p.LightDelay <= 0 {
		p.LightDelay = d.LightDelay
	}
	p.TestURL = strings.TrimSpace(p.TestURL)
	if p.TestURL == "" {
		p.TestURL = d.TestURL
	}
	if p.TestTimeoutMS == 0 {
		p.TestTimeoutMS = d.TestTimeoutMS
	}
	if p.TestConcurrency == 0 {
		p.TestConcurrency = d.TestConcurrency
	}
	return p
}

func validatePrefs(p Prefs) error {
	if !validAppearance(p.Appearance) {
		return fmt.Errorf("请选择跟随系统、浅色或深色主题")
	}
	if !validDockVisibility(p.DockVisibility) {
		return fmt.Errorf("请选择隐藏、跟随窗口或显示程序坞图标")
	}
	if p.AutoUpdateHours < 0 {
		return fmt.Errorf("订阅更新间隔不能为负数")
	}
	return validateTestSettings(p.TestURL, p.TestTimeoutMS, p.TestConcurrency)
}

func validateTestSettings(testURL string, timeoutMS, concurrency int) error {
	if err := validateTestURL(testURL); err != nil {
		return err
	}
	if timeoutMS < 1000 || timeoutMS > 30000 {
		return fmt.Errorf("测试超时须在 1000–30000 毫秒之间")
	}
	if concurrency < 1 || concurrency > 32 {
		return fmt.Errorf("测试并发数须在 1–32 之间")
	}
	return nil
}

func validateTestURL(raw string) error {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.ContainsAny(raw, " \t\r\n") {
		return fmt.Errorf("请输入完整的 HTTP 或 HTTPS 测试地址，不含账号或片段")
	}
	return nil
}

// Replace the file atomically so a failed or interrupted save cannot truncate
// the user's previous settings. Both preferences and network overrides use it.
func writeSettingsFile(path string, value any) error {
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".settings-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// SetPrefsHook lets the window side apply appearance, Dock visibility and the
// release timer after a preference is saved.
func (a *App) SetPrefsHook(fn func(Prefs)) {
	a.pmu.Lock()
	a.prefsHook = fn
	p := a.prefs
	a.pmu.Unlock()
	if fn != nil {
		fn(p)
	}
}

func prefsPathFor(dir string) string { return filepath.Join(dir, "prefs.json") }

// Auto-update choices, shared by the settings page and the loop.
var autoUpdateHours = []int{0, 6, 12, 24}

func autoUpdateIndex(hours int) int {
	for i, h := range autoUpdateHours {
		if h == hours {
			return i
		}
	}
	return 0
}
