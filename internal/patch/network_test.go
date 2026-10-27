package patch

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/metacubex/mihomo/config"
	C "github.com/metacubex/mihomo/constant"
	_ "github.com/metacubex/mihomo/hub/executor" // supplies config's temporaryUpdateGeneral linkname
	"go.yaml.in/yaml/v3"
)

const networkProfile = `dns:
  enable: true
  enhanced-mode: redir-host
  nameserver: [8.8.8.8]
  nameserver-policy:
    '+.example.com': 9.9.9.9
  fake-ip-range: 198.19.0.1/16
sniffer:
  enable: true
  override-destination: true
  sniff:
    HTTP:
      ports: [80]
      override-destination: true
    TLS:
      ports: [443]
rules: ["MATCH,DIRECT"]
`

func TestNetworkFeaturesFollowAndOverride(t *testing.T) {
	ov := Default()
	ov.MergeYAML = "dns:\n  nameserver: [9.9.9.9]\nsniffer:\n  enable: false\n"
	raw, err := Apply([]byte(networkProfile), ov)
	if err != nil {
		t.Fatal(err)
	}
	d, s, err := FeatureSettings(raw)
	if err != nil || !d.Enable || d.EnhancedMode != "redir-host" || !reflect.DeepEqual(d.Nameserver, []string{"9.9.9.9"}) || s.Enable {
		t.Fatalf("follow mode failed: DNS=%+v sniffer=%+v error=%v", d, s, err)
	}
	d = DefaultDNSSettings()
	d.Nameserver = []string{"https://1.1.1.1/dns-query"}
	s = DefaultSnifferSettings()
	s.HTTPPorts, s.QUICPorts = nil, nil
	s.SkipSrcAddress = []string{"10.0.0.8", "fd00::1"}
	ov.DNS, ov.Sniffer = &d, &s
	raw, err = Apply([]byte(networkProfile), ov)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	dns, sniff := doc["dns"].(map[string]any), doc["sniffer"].(map[string]any)
	if dns["fake-ip-range"] != "198.19.0.1/16" || dns["nameserver-policy"] == nil {
		t.Fatalf("unmanaged DNS fields lost: %v", dns)
	}
	protocols := sniff["sniff"].(map[string]any)
	if !reflect.DeepEqual(sniff["skip-src-address"], []any{"10.0.0.8/32", "fd00::1/128"}) {
		t.Fatalf("bare IP ranges not normalized: %v", sniff["skip-src-address"])
	}
	if len(protocols) != 1 || protocols["TLS"].(map[string]any)["override-destination"] != false {
		t.Fatalf("protocol clearing/global destination switch ignored: %v", protocols)
	}
	if values, ok := dns["fallback"].([]any); !ok || len(values) != 0 {
		t.Fatalf("empty list must clear inherited values: %v", dns["fallback"])
	}
	// Both the legacy overrides file and new fields round-trip without turning
	// absent settings into explicit false values.
	body, err := json.Marshal(ov)
	if err != nil {
		t.Fatal(err)
	}
	var restored Overrides
	if err := json.Unmarshal(body, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored, ov) {
		t.Fatalf("round trip: %+v", restored)
	}
	ov.DNS, ov.Sniffer = nil, nil
	raw, err = Apply([]byte(networkProfile), ov)
	if err != nil {
		t.Fatal(err)
	}
	d, s, err = FeatureSettings(raw)
	if err != nil || d.EnhancedMode != "redir-host" || s.Enable {
		t.Fatalf("follow reset failed: %s, %v", raw, err)
	}
}

func TestNetworkFeaturesAcceptedByKernel(t *testing.T) {
	previousHome := C.Path.HomeDir()
	C.SetHomeDir(t.TempDir())
	t.Cleanup(func() { C.SetHomeDir(previousHome) })
	ov := Default()
	d, s := DefaultDNSSettings(), DefaultSnifferSettings()
	d.Nameserver = []string{"1.1.1.1", "https://dns.example.com/dns-query#DIRECT"}
	d.DefaultNameserver = []string{"system", "tls://1.1.1.1:853"}
	d.FakeIPFilter = []string{"*.lan", "+.local"}
	s.SkipDomain, s.ForceDomain = []string{"+.push.apple.com"}, []string{"+.example.com"}
	s.SkipSrcAddress, s.SkipDstAddress = []string{"192.168.0.0/16", "10.0.0.8", "fd00::1"}, []string{"91.105.192.0/23"}
	ov.DNS, ov.Sniffer = &d, &s
	raw, err := Apply([]byte("rules: [\"MATCH,DIRECT\"]\n"), ov)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := config.Parse(raw)
	if err != nil {
		t.Fatalf("kernel rejected generated config: %v\n%s", err, raw)
	}
	if !parsed.DNS.Enable || !parsed.Sniffer.Enable {
		t.Fatal("kernel did not enable both features")
	}
}

func TestNetworkFeaturesValidation(t *testing.T) {
	for _, raw := range []string{"0", "65536", "-1", "80,", "90-80", "80--90", "443;80", "abc", "+80"} {
		if _, err := ParseSniffPorts(raw); err == nil {
			t.Errorf("accepted ports %q", raw)
		}
	}
	if got, err := ParseSniffPorts(" 80, 443,8080-8880 "); err != nil || !reflect.DeepEqual(got, []string{"80", "443", "8080-8880"}) {
		t.Fatalf("valid port set: %v, %v", got, err)
	}
	for _, value := range []string{"1.1.1.1", "2001:4860:4860::8888", "system", "tls://1.1.1.1:853", "https://1.1.1.1/dns-query"} {
		if err := validateDNSServer(value, true); err != nil {
			t.Errorf("valid bootstrap %q rejected: %v", value, err)
		}
	}
	for _, value := range []string{"https://", "https://dns.example.com/dns-query", "1.1.1.1:70000", "bogus://1.1.1.1", "bad address"} {
		if err := validateDNSServer(value, true); err == nil {
			t.Errorf("accepted invalid bootstrap %q", value)
		}
	}
	ov := Default()
	d := DefaultDNSSettings()
	d.Enable, ov.TunEnable = false, true
	ov.DNS = &d
	if _, err := Apply([]byte(networkProfile), ov); err == nil || !strings.Contains(err.Error(), "TUN") {
		t.Fatalf("DNS/TUN conflict accepted: %v", err)
	}
	s := DefaultSnifferSettings()
	s.SkipDstAddress = []string{"192.168.0.0/99"}
	if err := s.Validate(); err == nil {
		t.Fatal("accepted invalid skipped CIDR")
	}
	s.SkipDstAddress = nil
	s.HTTPPorts, s.TLSPorts, s.QUICPorts = nil, nil, nil
	if err := s.Validate(); err == nil {
		t.Fatal("enabled sniffer without protocols accepted")
	}
}

func TestNetworkFeatureDefaultsMatchKernelAndLegacyPorts(t *testing.T) {
	d, s, err := FeatureSettings([]byte("rules: [\"MATCH,DIRECT\"]\n"))
	defaults := config.DefaultRawConfig()
	if err != nil || d.Enable || s.Enable || d.EnhancedMode != defaults.DNS.EnhancedMode.String() ||
		!reflect.DeepEqual(d.Nameserver, defaults.DNS.NameServer) || s.OverrideDestination != defaults.Sniffer.OverrideDest {
		t.Fatalf("display differs from kernel defaults: %+v, %+v, %v", d, s, err)
	}
	_, s, err = FeatureSettings([]byte("sniffer: {enable: true, sniff: {TLS: {}}}\n"))
	if err != nil || len(s.HTTPPorts) != 0 || !reflect.DeepEqual(s.TLSPorts, []string{"1-65535"}) {
		t.Fatalf("all-port protocol changed: %+v %v", s, err)
	}
	_, s, err = FeatureSettings([]byte("sniffer: {enable: true, sniffing: [tls], port-whitelist: [443, 8443]}\n"))
	if err != nil || !reflect.DeepEqual(s.TLSPorts, []string{"443", "8443"}) || len(s.HTTPPorts) > 0 {
		t.Fatalf("legacy ports lost: %+v %v", s, err)
	}
}
