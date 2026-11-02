package sysproxy

import (
	"testing"
	"unsafe"
)

func TestWindowsOptionNativeLayoutAndReadOnlySnapshot(t *testing.T) {
	// The supported Windows release target is amd64. These offsets are the
	// WinINet ABI, not Go-only serialization details.
	if unsafe.Sizeof(uintptr(0)) == 8 && (unsafe.Sizeof(internetOption{}) != 16 || unsafe.Offsetof(internetOptionList{}.Options) != 24) {
		t.Fatal("WinINet option layout does not match the Windows ABI")
	}
	state, err := Current()
	if err != nil || !state.Supported || !state.WindowsOptions {
		t.Fatalf("could not read Windows proxy options: %v", err)
	}
}

func TestParseWindowsProxy(t *testing.T) {
	for _, tc := range []struct {
		raw, host string
		port      int
	}{
		{"127.0.0.1:7890", "127.0.0.1", 7890},
		{"http=proxy.example:8080;https=secure.example:444;socks=other:1080", "proxy.example", 8080},
		{"[::1]:7890", "::1", 7890},
		{"https=secure.example:444", "", 0},
		{"", "", 0},
	} {
		host, port := parseWindowsProxy(tc.raw)
		if host != tc.host || port != tc.port {
			t.Fatalf("parse %q = %s:%d", tc.raw, host, port)
		}
	}
}

func TestWindowsProxyRejectsInvalidAddressBeforeWriting(t *testing.T) {
	for _, tc := range []struct {
		server string
		port   int
	}{
		{"", 7890}, {"127.0.0.1;http=evil", 7890}, {"127.0.0.1", 0}, {"127.0.0.1", 65536},
	} {
		if Apply(tc.server, tc.port) == nil {
			t.Fatal("invalid proxy accepted")
		}
	}
}
