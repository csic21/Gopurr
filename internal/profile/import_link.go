package profile

import (
	"errors"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopurr/internal/localize"
)

type ImportLink struct{ URL, Name string }

// ParseImportLink decodes the subscription URL exactly once. No error includes
// the incoming URL, which commonly contains subscription credentials.
func ParseImportLink(raw string) (ImportLink, error) {
	invalid := func() (ImportLink, error) {
		return ImportLink{}, errors.New(localize.Text("导入链接无效，请使用包含 HTTP 或 HTTPS 订阅地址的 install-config 链接"))
	}
	if len(raw) > 32768 || !utf8.ValidString(raw) || strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return invalid()
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "gopurr" && u.Scheme != "clash") || u.Host != "install-config" || (u.Path != "" && u.Path != "/") || u.User != nil || u.Fragment != "" {
		return invalid()
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q["url"]) != 1 || len(q["name"]) > 1 {
		return invalid()
	}
	for key := range q {
		if key != "url" && key != "name" {
			return invalid()
		}
	}
	if strings.IndexFunc(q.Get("name"), unicode.IsControl) >= 0 {
		return invalid()
	}
	link := ImportLink{URL: q.Get("url"), Name: strings.TrimSpace(q.Get("name"))}
	target, err := url.Parse(link.URL)
	if err != nil || target == nil || target.Fragment != "" || !utf8.ValidString(link.URL+link.Name) || len(link.URL) > 16384 || strings.IndexFunc(link.URL+link.Name, unicode.IsControl) >= 0 || utf8.RuneCountInString(link.Name) > 128 {
		return invalid()
	}
	if err := ValidateSettings(Profile{Name: "import", URL: link.URL}); err != nil {
		return invalid()
	}
	return link, nil
}
