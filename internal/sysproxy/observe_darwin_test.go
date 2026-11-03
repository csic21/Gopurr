//go:build darwin

package sysproxy

import (
	"github.com/ebitengine/purego"
	"os"
	"reflect"
	"testing"
)

func TestNativeProxyObserverPreservesDisabledProtocolsAndAutomaticSettings(t *testing.T) {
	a, err := loadProxyObserver()
	if err != nil {
		t.Fatal(err)
	}
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		t.Fatal(err)
	}
	var dataCreate func(uintptr, *byte, int64) uintptr
	var plistCreate func(uintptr, uintptr, uint64, uintptr, uintptr) uintptr
	purego.RegisterLibFunc(&dataCreate, cf, "CFDataCreate")
	purego.RegisterLibFunc(&plistCreate, cf, "CFPropertyListCreateWithData")
	raw := []byte(`<?xml version="1.0"?><plist version="1.0"><dict>
 <key>HTTPEnable</key><integer>1</integer><key>HTTPProxy</key><string>127.0.0.1</string><key>HTTPPort</key><integer>7890</integer>
 <key>HTTPSEnable</key><integer>0</integer><key>HTTPSProxy</key><string>disabled.example</string><key>HTTPSPort</key><integer>8443</integer>
 <key>SOCKSEnable</key><true/><key>SOCKSProxy</key><string>localhost</string><key>SOCKSPort</key><integer>1080</integer>
 <key>ProxyAutoConfigEnable</key><true/><key>ProxyAutoConfigURLString</key><string>https://example.invalid/proxy.pac</string>
 <key>ProxyAutoDiscoveryEnable</key><integer>1</integer><key>ExceptionsList</key><array><string>*.local</string><string>公司.example</string></array>
 </dict></plist>`)
	data := dataCreate(0, &raw[0], int64(len(raw)))
	defer a.release(data)
	dict := plistCreate(0, data, 0, 0, 0)
	if dict == 0 {
		t.Fatal("invalid fixture plist")
	}
	defer a.release(dict)
	got := a.decodeProxies(State{Supported: true, Service: "Wi-Fi"}, dict)
	want := State{Supported: true, Service: "Wi-Fi", Enabled: true, Server: "127.0.0.1", Port: 7890, ProtocolsKnown: true,
		Protocols:  [3]Endpoint{{true, "127.0.0.1", 7890}, {false, "disabled.example", 8443}, {true, "localhost", 1080}},
		PACEnabled: true, PAC: "https://example.invalid/proxy.pac", AutoDiscovery: true, Bypass: "*.local\n公司.example"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("native observation lost saved protocol, PAC, discovery or bypass fields")
	}
	if got := a.decodeProxies(State{Supported: true}, 0); got.Enabled || !got.ProtocolsKnown || got.PACEnabled {
		t.Fatal("unconfigured protocols were reported enabled")
	}
}

func TestNativeProxyObserverMatchesReadOnlySystemCommands(t *testing.T) {
	if os.Getenv("GOPURR_TEST_PROXY_OBSERVE") != "1" {
		t.Skip("opt-in read-only host observation")
	}
	a, err := loadProxyObserver()
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.current()
	if err != nil {
		t.Fatal(err)
	}
	want, err := currentUsing(run)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal("native and command observations differ (settings omitted from output)")
	}
}

func BenchmarkNativeProxyObservation(b *testing.B) {
	if os.Getenv("GOPURR_TEST_PROXY_OBSERVE") != "1" {
		b.Skip("opt-in read-only host observation")
	}
	a, err := loadProxyObserver()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := a.current(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCommandProxyObservation(b *testing.B) {
	if os.Getenv("GOPURR_TEST_PROXY_OBSERVE") != "1" {
		b.Skip("opt-in read-only host observation")
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := currentUsing(run); err != nil {
			b.Fatal(err)
		}
	}
}
