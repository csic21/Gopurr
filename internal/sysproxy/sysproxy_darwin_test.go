//go:build darwin

package sysproxy

import (
	"strings"
	"testing"
)

func TestEnableScriptCoversAllThreeProxies(t *testing.T) {
	s := enableScript("Wi-Fi", "127.0.0.1", 7890)
	for _, want := range []string{
		"networksetup -setwebproxy 'Wi-Fi' '127.0.0.1' 7890",
		"networksetup -setsecurewebproxy 'Wi-Fi' '127.0.0.1' 7890",
		"networksetup -setsocksfirewallproxy 'Wi-Fi' '127.0.0.1' 7890",
		"networksetup -setproxybypassdomains 'Wi-Fi'",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("script missing %q:\n%s", want, s)
		}
	}
	if strings.Count(s, ";") != 3 {
		t.Fatalf("expected four statements:\n%s", s)
	}
}

func TestDisableScript(t *testing.T) {
	s := disableScript("Wi-Fi")
	for _, want := range []string{
		"-setwebproxystate 'Wi-Fi' off",
		"-setsecurewebproxystate 'Wi-Fi' off",
		"-setsocksfirewallproxystate 'Wi-Fi' off",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("script missing %q:\n%s", want, s)
		}
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"Wi-Fi":         "'Wi-Fi'",
		"a'b":           `'a'\''b'`,
		"USB 10/100":    "'USB 10/100'",
		"$(rm -rf /)":   "'$(rm -rf /)'",
		"`whoami`":      "'`whoami`'",
		"Thunderbolt 4": "'Thunderbolt 4'",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Fatalf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestServiceForDeviceRejectsUnknown(t *testing.T) {
	// A device that cannot exist must not resolve to a service.
	if _, err := serviceForDevice("zz9"); err == nil {
		t.Fatal("expected error for unknown device")
	}
}
