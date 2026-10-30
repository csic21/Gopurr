package patch

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestMeshSessionLeavesOrdinaryNetworkSettingsUnchanged(t *testing.T) {
	ov := Default()
	ov.TunEnable = true
	ov.AllowLAN = true
	raw := []byte(`dns:
  enable: true
  nameserver: [https://dns.example/dns-query]
tun:
  route-exclude-address: [192.168.0.0/16]
proxies:
  - {name: ordinary, type: socks5, server: 127.0.0.1, port: 1080}
rules: ["MATCH,ordinary"]
`)
	baseline, err := Apply(raw, ov)
	if err != nil {
		t.Fatal(err)
	}
	ov.MeshIPs = []string{"100.70.0.2", "fd7a:115c:a1e0::2"}
	ov.MeshProxyAddress, ov.MeshProxyPassword = "127.0.0.1:42123", strings.Repeat("a", 64)
	connected, err := Apply(raw, ov)
	if err != nil {
		t.Fatal(err)
	}
	var before, during map[string]any
	if err := yaml.Unmarshal(baseline, &before); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(connected, &during); err != nil {
		t.Fatal(err)
	}
	for key, value := range before {
		if key != "rules" && key != "proxies" && !reflect.DeepEqual(value, during[key]) {
			t.Errorf("mesh changed ordinary setting %s", key)
		}
	}
	proxies := during["proxies"].([]any)
	rules := during["rules"].([]any)
	if !reflect.DeepEqual(proxies[:len(proxies)-1], before["proxies"]) ||
		!reflect.DeepEqual(rules[2:], before["rules"]) {
		t.Fatal("mesh changed ordinary proxies or their rule order")
	}
	ov.MeshIPs, ov.MeshProxyAddress, ov.MeshProxyPassword = nil, "", ""
	disconnected, err := Apply(raw, ov)
	if err != nil || !bytes.Equal(baseline, disconnected) {
		t.Fatalf("explicit disconnect did not restore ordinary configuration: %v", err)
	}
}
