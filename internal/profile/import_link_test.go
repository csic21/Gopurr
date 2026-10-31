package profile

import (
	"net/url"
	"strings"
	"testing"
)

func TestImportLinkPreservesEncodedCredentials(t *testing.T) {
	target := "https://example.com/subscribe?token=a%2Bb%252Fc&flag=meta"
	for _, scheme := range []string{"gopurr", "clash"} {
		raw := scheme + "://install-config?" + url.Values{"url": {target}, "name": {"工作订阅"}}.Encode()
		got, err := ParseImportLink(raw)
		if err != nil || got.URL != target || got.Name != "工作订阅" {
			t.Fatalf("got %+v, %v", got, err)
		}
	}
}

func TestImportLinkRejectsUnsupportedOrAmbiguousRequests(t *testing.T) {
	for _, raw := range []string{
		"https://install-config?url=https://example.com", "gopurr://run?url=https://example.com",
		"gopurr://user@install-config?url=https://example.com", "clash://install-config/path?url=https://example.com",
		"clash://install-config?url=file:///tmp/config.yaml", "clash://install-config?url=javascript:alert(1)",
		"clash://install-config?url=https://user:password@example.com", "clash://install-config?url=https://",
		"clash://install-config?url=https://example.com&url=https://other.com",
		"clash://install-config?url=https://example.com&token=secret", "clash://install-config?url=https://example.com&silent=true",
		"clash://install-config?url=https://example.com%23fragment", "clash://install-config?url=%zz",
		"clash://install-config?url=https://example.com&name=%0Aevil",
		"clash://install-config?url=https://example.com&name=" + strings.Repeat("a", 129),
		"clash://install-config?url=https://example.com\n", "clash://install-config?url=https://example.com#silent",
	} {
		if _, err := ParseImportLink(raw); err == nil {
			t.Errorf("accepted %q", raw)
		} else if strings.Contains(err.Error(), "secret") {
			t.Fatal("credential in error")
		}
	}
}
