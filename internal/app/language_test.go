package app

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"gopurr/internal/patch"
	"gopurr/internal/profile"
)

func TestLanguagePreferences(t *testing.T) {
	path := prefsPathFor(t.TempDir())
	for _, language := range languages {
		a := &App{prefs: defaultPrefs(), prefsPath: path}
		p := a.Prefs()
		p.Language = language
		if err := a.UpdatePrefs(p); err != nil {
			t.Fatal(err)
		}
		reloaded := &App{prefsPath: path}
		reloaded.loadPrefs()
		if reloaded.Prefs().Language != language {
			t.Fatalf("language %q was not persisted", language)
		}
		want := language
		if want == LanguageSystem {
			want = ""
		}
		if reloaded.Prefs().NativeLanguage() != want {
			t.Fatal("wrong native language override")
		}
	}
	for _, body := range []string{`{"lightMode":true}`, `{"language":"fr","lightMode":true}`} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		a := &App{prefsPath: path}
		a.loadPrefs()
		if p := a.Prefs(); p.Language != LanguageSystem || !p.LightMode {
			t.Fatalf("migration lost settings: %+v", p)
		}
		p := a.Prefs()
		p.Language = "invalid"
		if err := a.UpdatePrefs(p); err == nil || a.Prefs().Language != LanguageSystem {
			t.Fatal("invalid language published")
		}
	}
}

func TestLanguageMatching(t *testing.T) {
	for _, tc := range []struct {
		preferred []string
		want      string
	}{
		{[]string{"zh-CN"}, LanguageChinese},
		{[]string{"zh-SG"}, LanguageChinese},
		{[]string{"en-GB"}, LanguageEnglish},
		{[]string{"ja-JP"}, LanguageEnglish},
		{[]string{"zh-TW"}, LanguageEnglish},
		{[]string{"de-DE", "zh-CN"}, LanguageChinese},
		{nil, LanguageEnglish},
	} {
		got := i18n.Match(tc.preferred, []string{LanguageEnglish, LanguageChinese})
		if got == "" {
			got = LanguageEnglish
		}
		if got != tc.want {
			t.Fatalf("%v: got %q, want %q", tc.preferred, got, tc.want)
		}
	}
}

func TestLanguageSwitchesImmediatelyAndPreservesData(t *testing.T) {
	a, _ := newSettingsTestApp(t, nil)
	a.booted, a.ticking, a.page = true, true, "settings"
	preferred := []string{"zh-CN"}
	i18n.SetLocaleFunc(func() string {
		if override := a.Prefs().NativeLanguage(); override != "" {
			return override
		}
		if matched := i18n.Match(preferred, []string{LanguageEnglish, LanguageChinese}); matched != "" {
			return matched
		}
		return LanguageEnglish
	})
	t.Cleanup(func() { i18n.SetLocaleFunc(func() string { return LanguageChinese }) })
	tr := ui.NewTester(a.View, 840, 560)
	tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 88})
	tr.Frame()
	for _, step := range []struct{ label, value, title string }{
		{"English", LanguageEnglish, "Appearance"},
		{"简体中文", LanguageChinese, "外观"},
		{"跟随系统", LanguageSystem, "外观"},
	} {
		// The System choice is scoped by the language control; Theme has its own.
		if step.value == LanguageSystem {
			r, ok := tr.Find("界面语言")
			if !ok {
				t.Fatal("language selector missing")
			}
			tr.ClickAt(r.X+15, r.Y+r.H/2)
		} else if err := tr.Click(step.label); err != nil {
			t.Fatal(err)
		}
		tr.Frame()
		if a.Prefs().Language != step.value {
			t.Fatalf("click %q did not save language: %+v", step.label, a.Prefs())
		}
		if _, ok := tr.Find(step.title); !ok {
			t.Fatalf("%q not translated immediately", step.title)
		}
	}
	preferred = []string{"ja-JP"}
	tr.Frame()
	if _, ok := tr.Find("Appearance"); !ok {
		t.Fatal("unsupported system language did not fall back to English")
	}
	preferred = []string{"zh-CN"}
	tr.Frame()
	if _, ok := tr.Find("外观"); !ok {
		t.Fatal("did not follow system language change")
	}
	// Controller values are canonical, even if identical to translated words.
	a.proxySort = "名称排序"
	a.settingsReady = false
	if err := a.changePrefs(func(p *Prefs) { p.Language = LanguageEnglish }); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if a.proxySort != "名称排序" {
		t.Fatal("language changed the proxy sort value")
	}
	if translate(LanguageEnglish, "用户节点 上海") != "用户节点 上海" {
		t.Fatal("user data translated")
	}
	menu := a.trayItems(false, nil, func() {})
	if findTrayItem(t, menu, "show-window").Label != "Show Window" {
		t.Fatal("tray did not follow language")
	}
}

func TestLanguageSaveFailureKeepsSelection(t *testing.T) {
	a := &App{prefs: defaultPrefs(), prefsPath: t.TempDir()}
	a.initSettings()
	a.languageIdx = languageIndex(LanguageEnglish)
	a.reconcileSettings()
	if a.Prefs().Language != LanguageSystem || a.languageIdx != 0 || a.generalSettingsError == "" {
		t.Fatal("failed save changed language")
	}
}

func TestTranslationCatalogCoverage(t *testing.T) {
	// Every literal marked for translation must have an English fallback, and
	// printf arguments must retain their types and order in each translation.
	englishJSON, err := os.ReadFile("../localize/en.json")
	if err != nil {
		t.Fatal(err)
	}
	var englishMessages map[string]string
	if err := json.Unmarshal(englishJSON, &englishMessages); err != nil {
		t.Fatal(err)
	}
	verbs := regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z]`)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || id.Name != "tr" || len(call.Args) != 1 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			key, _ := strconv.Unquote(lit.Value)
			if _, ok := englishMessages[key]; !ok {
				t.Errorf("%s: missing English translation for %q", path, key)
			}
			return true
		})
	}
	var entries map[string]string
	if err := json.Unmarshal(englishJSON, &entries); err != nil {
		t.Fatal(err)
	}
	for source, english := range entries {
		if strings.Join(verbs.FindAllString(source, -1), ",") != strings.Join(verbs.FindAllString(english, -1), ",") {
			t.Errorf("printf placeholders differ: %q -> %q", source, english)
		}
	}
}

func TestEnglishValidationAndLocalizedSelect(t *testing.T) {
	i18n.SetLocaleFunc(func() string { return LanguageEnglish })
	t.Cleanup(func() { i18n.SetLocaleFunc(func() string { return LanguageChinese }) })
	if err := (patch.DNSSettings{}).Validate(); err == nil || err.Error() != "DNS mode must be fake-ip or redir-host" {
		t.Fatalf("DNS validation was not localized: %v", err)
	}
	if err := profile.ValidateSettings(profile.Profile{}); err == nil || err.Error() != "Enter a profile name" {
		t.Fatalf("profile validation was not localized: %v", err)
	}
	value := "订阅顺序"
	values := []string{"订阅顺序", "延迟优先", "名称排序"}
	tr := ui.NewTester(func(c *ui.Context) {
		localizedSelect(c, &value, values, translated(values)).Width(180)
	}, 400, 300)
	tr.Frame()
	if err := tr.Click("Profile Order"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if err := tr.Click("Latency"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if value != "延迟优先" {
		t.Fatalf("localized selection did not keep canonical value: %q", value)
	}
	i18n.SetLocaleFunc(func() string { return LanguageChinese })
	tr.Frame()
	if _, ok := tr.Find("延迟优先"); !ok {
		t.Fatal("selected label did not follow language")
	}
}

func TestEnglishTopNavigationFitsWindowsTitleBar(t *testing.T) {
	a, _ := newSettingsTestApp(t, nil)
	a.booted, a.ticking = true, true
	p := a.Prefs()
	p.SidebarCollapsed = true
	if err := a.UpdatePrefs(p); err != nil {
		t.Fatal(err)
	}
	i18n.SetLocaleFunc(func() string { return LanguageEnglish })
	t.Cleanup(func() { i18n.SetLocaleFunc(func() string { return LanguageChinese }) })
	tr := ui.NewTester(a.View, 840, 560)
	tr.SetPreferences(ui.Preferences{ReduceMotion: true})
	tr.SetTitleBar(ui.TitleBar{Height: 38, Right: 138})
	tr.Frame()
	var end float32
	for _, item := range navItems {
		r, ok := tr.Find(translate(LanguageEnglish, item.label))
		if !ok || r.Y > 38 || r.X < end || r.X+r.W > 840-138 {
			t.Fatalf("toolbar %s missing, overlapping or clipped: %+v", item.id, r)
		}
		end = r.X + r.W
	}
}
