package patch

import "testing"

func TestWindowsTunDefaultsCaptureBothDNSProtocolsAndPreserveExplicitChoices(t *testing.T) {
	tun := map[string]any{}
	defaultTunOptions(tun, "windows")
	if tun["strict-route"] != true || tun["device"] != "Gopurr" || len(tun["dns-hijack"].([]any)) != 2 {
		t.Fatalf("incomplete Windows defaults: %#v", tun)
	}
	tun = map[string]any{"strict-route": false, "device": "Custom", "dns-hijack": []any{"1.1.1.1:53"}}
	defaultTunOptions(tun, "windows")
	if tun["strict-route"] != false || tun["device"] != "Custom" || len(tun["dns-hijack"].([]any)) != 1 {
		t.Fatal("explicit advanced choices overwritten")
	}
}
