package engine

import (
	"testing"

	"github.com/metacubex/mihomo/config"
	"go.yaml.in/yaml/v3"
)

func TestVPNConfigurationPreservesDNSButOwnsTunnel(t *testing.T) {
	raw := []byte(`mode: rule
dns:
  enable: false
  listen: 0.0.0.0:53
  nameserver: [https://dns.example/dns-query]
  fake-ip-filter: ["+.internal"]
tun:
  auto-route: true
  auto-detect-interface: true
  file-descriptor: 123456
`)
	managed, err := vpnConfiguration(raw, 37)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(managed, &doc); err != nil {
		t.Fatal(err)
	}
	dns := doc["dns"].(map[string]any)
	if dns["enable"] != true || dns["listen"] != nil {
		t.Fatalf("VPN must resolve DNS internally without opening a DNS listener: %#v", dns)
	}
	if dns["nameserver"].([]any)[0] != "https://dns.example/dns-query" || dns["fake-ip-filter"].([]any)[0] != "+.internal" {
		t.Fatalf("source DNS options were discarded: %#v", dns)
	}
	tun := doc["tun"].(map[string]any)
	if tun["file-descriptor"] != 37 || tun["auto-route"] != false || tun["auto-detect-interface"] != false {
		t.Fatalf("source config took ownership of Android's system-managed tunnel: %#v", tun)
	}
	if doc["mode"] != "rule" {
		t.Fatal("VPN configuration discarded source routing mode")
	}
	parsed, err := config.Parse(managed)
	if err != nil {
		t.Fatal(err)
	}
	addresses := parsed.General.Tun.Inet4Address
	if len(addresses) != 1 || addresses[0].String() != "198.18.0.1/30" {
		t.Fatalf("core's parsed TUN address differs from Android's system interface: %v", addresses)
	}
}
