//go:build darwin

package sysproxy

import (
	"testing"
)

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
