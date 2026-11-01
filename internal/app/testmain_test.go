package app

import (
	"os"
	"testing"

	"github.com/egoist/mygo/i18n"

	"gopurr/internal/localize"
)

// Existing interaction and render fixtures explicitly use Chinese, independent
// of the host's preferred languages. Language tests override this per test.
func TestMain(m *testing.M) {
	localize.SetLanguageSource(i18n.Locale)
	i18n.SetLocaleFunc(func() string { return LanguageChinese })
	// No native event loop runs under go test; async completions run inline.
	runOnMain = func(fn func()) { fn() }
	os.Exit(m.Run())
}
