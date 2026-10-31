package app

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/shortcuts"
)

type shortcutBackend struct {
	keys    map[string]func()
	blocked string
}

func (b *shortcutBackend) Register(key string, fn func()) error {
	if key == b.blocked || b.keys[key] != nil {
		return errors.New("already in use")
	}
	b.keys[key] = fn
	return nil
}
func (b *shortcutBackend) Unregister(key string) { delete(b.keys, key) }

func TestShortcutSettingsConflictAndDiskFailureKeepBindings(t *testing.T) {
	a := &App{prefs: defaultPrefs(), prefsPath: prefsPathFor(t.TempDir())}
	b := &shortcutBackend{keys: map[string]func(){}}
	a.ConfigureShortcuts(b, func() {})
	a.shortcutDraft[0] = "Ctrl+Alt+P"
	a.saveShortcutSettings()
	old := a.Prefs()
	if len(b.keys) != 1 || old.Shortcuts[0] != "Ctrl+Alt+P" {
		t.Fatal("settings not registered")
	}
	b.blocked = "Ctrl+Alt+X"
	a.shortcutDraft[0] = b.blocked
	a.saveShortcutSettings()
	if a.shortcutError == "" || a.Prefs() != old || b.keys["Ctrl+Alt+P"] == nil {
		t.Fatal("conflict replaced old settings")
	}
	a.prefsPath = t.TempDir() // rename over a directory fails
	a.shortcutDraft[0] = "Ctrl+Alt+N"
	a.saveShortcutSettings()
	if a.shortcutError == "" || a.Prefs() != old || len(b.keys) != 1 || b.keys["Ctrl+Alt+P"] == nil {
		t.Fatal("disk failure lost bindings")
	}
	a.CloseShortcuts()
}

func TestShortcutStartupRetryAndPersistence(t *testing.T) {
	path := prefsPathFor(t.TempDir())
	a := &App{prefs: defaultPrefs(), prefsPath: path}
	p := a.Prefs()
	p.Shortcuts[0] = "Ctrl+Alt+P"
	if err := a.UpdatePrefs(p); err != nil {
		t.Fatal(err)
	}
	b := &shortcutBackend{keys: map[string]func(){}, blocked: "Ctrl+Alt+P"}
	shown := 0
	a.ConfigureShortcuts(b, func() { shown++ })
	if a.shortcutError == "" {
		t.Fatal("startup conflict hidden")
	}
	b.blocked = ""
	a.openShortcutSettings()
	a.saveShortcutSettings()
	if a.shortcutError != "" || a.showShortcuts {
		t.Fatal("could not retry")
	}
	b.keys["Ctrl+Alt+P"]()
	if shown != 1 {
		t.Fatal("window action missing")
	}
	a.shutdownPending.Store(true)
	b.keys["Ctrl+Alt+P"]()
	if shown != 1 {
		t.Fatal("shortcut fired while quitting")
	}
	reloaded := &App{prefsPath: path}
	reloaded.loadPrefs()
	if reloaded.Prefs().Shortcuts != p.Shortcuts {
		t.Fatal("bindings not persistent")
	}
	a.CloseShortcuts()
}

func TestExternalSubscriptionStagesWithoutDownloadingOrOverwritingDraft(t *testing.T) {
	a := subscriptionTestApp(t)
	a.booted, a.ticking = true, true
	before := a.store.ActiveID()
	raw := "gopurr://install-config?" + url.Values{"url": {"https://subscription.example.com/sub?token=private"}, "name": {"新订阅"}}.Encode()
	a.OpenSubscriptionURL(raw)
	if a.page != "subs" || !a.showAddSub || a.subName != "新订阅" || a.store.ActiveID() != before {
		t.Fatal("import did not stage confirmation")
	}
	busy, _ := a.subState()
	if busy {
		t.Fatal("external link downloaded without confirmation")
	}
	previous := a.subURL
	a.OpenSubscriptionURL("clash://install-config?url=https://other.example.com")
	if a.subURL != previous || a.notice == "" {
		t.Fatal("external link overwrote draft")
	}
	a.OpenSubscriptionURL("clash://install-config?url=file:///secret")
	if a.subURL != previous {
		t.Fatal("invalid link modified editor")
	}
}

func TestShortcutEditorCancelAndMinimumWindow(t *testing.T) {
	a, _ := newSettingsTestApp(t, nil)
	a.booted, a.ticking, a.page = true, true, "settings"
	a.openShortcutSettings()
	tr := ui.NewTester(a.View, 840, 560)
	tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 80})
	tr.Frame()
	for _, label := range []string{"保存快捷键", "取消", "清空全部", "切换到直连模式"} {
		r, ok := tr.Find(label)
		if !ok || r.Y+r.H > 560 || r.X+r.W > 840 {
			t.Fatalf("clipped %s: %+v", label, r)
		}
	}
	// Long native conflict details scroll inside the panel, leaving the
	// recovery actions accessible at the minimum supported size.
	a.shortcutError = strings.Repeat("快捷键被其他应用占用，请选择不同组合。", 25)
	tr.Frame()
	for _, label := range []string{"保存快捷键", "取消", "清空全部"} {
		r, ok := tr.Find(label)
		if !ok || r.Y+r.H > 560 {
			t.Fatalf("long error hid %s: %+v", label, r)
		}
	}
	a.shortcutDraft[0] = "Ctrl+Alt+P"
	if err := tr.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if a.showShortcuts || a.Prefs().Shortcuts != (shortcuts.Config{}) {
		t.Fatal("cancel saved draft")
	}
	a.openShortcutSettings()
	if a.shortcutDraft != a.Prefs().Shortcuts {
		t.Fatal("reopening retained canceled changes")
	}
}

func TestShortcutRoutingAndMissingService(t *testing.T) {
	a := subscriptionTestApp(t)
	a.booted, a.ticking = true, true
	shown := 0
	open := func() { shown++ }
	for action, mode := range map[shortcuts.Action]string{shortcuts.RuleMode: "rule", shortcuts.GlobalMode: "global", shortcuts.DirectMode: "direct"} {
		a.RunShortcut(action, open)
		if a.ov.Mode != mode || a.modeToken != mode {
			t.Fatalf("mode %s not applied", mode)
		}
	}
	a.RunShortcut(shortcuts.ToggleTUN, open)
	if shown != 1 || a.page != "overview" || a.notice == "" || a.tunOn || a.confirmInstall {
		t.Fatal("missing service did not open Overview or installed without confirmation")
	}
}

func TestRestoreKeepsMachineShortcuts(t *testing.T) {
	a := subscriptionTestApp(t)
	p := a.Prefs()
	p.Shortcuts[0] = "Ctrl+Alt+P"
	if err := a.UpdatePrefs(p); err != nil {
		t.Fatal(err)
	}
	s, err := a.backupSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	p.Shortcuts[0] = "Ctrl+Alt+N"
	if err := a.UpdatePrefs(p); err != nil {
		t.Fatal(err)
	}
	got, _, _, err := a.prepareRestore(s)
	if err != nil || got.Shortcuts != p.Shortcuts {
		t.Fatalf("restore changed local keys: %v", err)
	}
	// Old preference files acquire disabled bindings without losing other data.
	path := filepath.Join(t.TempDir(), "prefs.json")
	if err := os.WriteFile(path, []byte(`{"silentStart":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	old := &App{prefsPath: path}
	old.loadPrefs()
	if !old.Prefs().SilentStart || old.Prefs().Shortcuts != (shortcuts.Config{}) {
		t.Fatal("old preferences lost")
	}
}
