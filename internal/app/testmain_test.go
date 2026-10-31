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
	os.Exit(m.Run())
}
