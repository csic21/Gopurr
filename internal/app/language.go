package app

import (
	"fmt"
	"strings"

	ui "gopurr/internal/appui"

	"gopurr/internal/diagnostic"
	"gopurr/internal/localize"
)

const (
	LanguageSystem  = "system"
	LanguageEnglish = "en"
	LanguageChinese = "zh-Hans"
)

var languages = []string{LanguageSystem, LanguageChinese, LanguageEnglish}

var localeFunc = func() string { return LanguageChinese }

func SetLocaleFunc(fn func() string) {
	if fn != nil {
		localeFunc = fn
	}
}

func currentLocale() string {
	lang := localeFunc()
	if lang == "" || lang == LanguageSystem {
		return MatchLanguage(nil)
	}
	return lang
}

// MatchLanguage maps system language tags onto the catalogs we ship.
// Regional Chinese tags use the simplified catalog, except Traditional Chinese,
// which stays on English until that catalog exists.
func MatchLanguage(preferred []string) string {
	for _, tag := range preferred {
		base := strings.ToLower(strings.ReplaceAll(tag, "_", "-"))
		switch {
		case strings.HasPrefix(base, "zh-hant"), strings.HasPrefix(base, "zh-tw"), strings.HasPrefix(base, "zh-hk"), strings.HasPrefix(base, "zh-mo"):
			continue
		case base == "zh" || strings.HasPrefix(base, "zh-"):
			return LanguageChinese
		case strings.HasPrefix(base, "en"):
			return LanguageEnglish
		}
	}
	return LanguageEnglish
}

// Translate only application-owned messages, never names or profile content.
func tr(message string) string                  { return translate(currentLocale(), message) }
func translate(language, message string) string { return localize.Translate(language, message) }

func translated(messages []string) []string {
	out := make([]string, len(messages))
	for i, message := range messages {
		out[i] = tr(message)
	}
	return out
}

func validLanguage(value string) bool {
	return value == LanguageSystem || value == LanguageEnglish || value == LanguageChinese
}

func languageIndex(value string) int {
	for i, language := range languages {
		if value == language {
			return i
		}
	}
	return 0
}

// NativeLanguage maps the persisted preference to MyGo's system-following
// sentinel. prefs.json remains the source of truth, including after restore.
func (p Prefs) NativeLanguage() string {
	if p.Language == LanguageSystem || p.Language == "" {
		return ""
	}
	return p.Language
}

// ConfigureLanguages declares English first so unavailable system languages
// fall back to English. MyGo matches regional tags and observes system changes.
func ConfigureLanguages(p Prefs) {
	localize.SetLanguageSource(currentLocale)
	SetLocaleFunc(func() string {
		if lang := p.NativeLanguage(); lang != "" {
			return lang
		}
		return MatchLanguage(nil)
	})
}

// localizedSelect keeps controller/filter values independent of display text.
// Only labels are translated, so switching languages cannot change a target
// named like a UI message or silently reset the selected sort order.
func localizedSelect(c *ui.Context, value *string, values, labels []string) ui.Element {
	label := ""
	for i, candidate := range values {
		if *value == candidate {
			label = labels[i]
			break
		}
	}
	return ui.Select(c, &label, labels).OnChange(func() {
		for i, candidate := range labels {
			if label == candidate {
				*value = values[i]
				return
			}
		}
	})
}

func trDiagnosticDetail(detail string) string {
	var code int
	if n, _ := fmt.Sscanf(detail, "收到 HTTP %d 响应", &code); n == 1 {
		return fmt.Sprintf(tr("收到 HTTP %d 响应"), code)
	}
	if n, _ := fmt.Sscanf(detail, "测试服务器返回 HTTP %d，可尝试更换测试地址", &code); n == 1 {
		return fmt.Sprintf(tr("测试服务器返回 HTTP %d，可尝试更换测试地址"), code)
	}
	return tr(detail)
}

func diagnosticReport(steps []diagnostic.Step) string {
	var b strings.Builder
	b.WriteString(tr("Gopurr 连通诊断\n报告不包含测试地址、订阅链接、密钥、进程路径和 DNS 解析结果。\n"))
	for _, step := range steps {
		fmt.Fprintf(&b, "\n%s: %s\n%s\n", tr(step.Name), tr(string(step.State)), trDiagnosticDetail(step.Detail))
	}
	return b.String()
}
