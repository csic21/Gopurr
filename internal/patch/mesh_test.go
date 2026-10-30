package patch

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestMeshRoutesUseExactAuthenticatedIPsAndPreserveExclusions(t *testing.T) {
	ov := Default()
	ov.TunEnable = true
	ov.MeshIPs = []string{"100.70.0.1", "100.70.0.2", "100.70.0.2", "fd7a:115c:a1e0::2", "192.168.1.1", "8.8.8.8", "127.0.0.1", "invalid"}
	ov.MeshProxyAddress = "127.0.0.1:42123"
	ov.MeshProxyPassword = strings.Repeat("a", 64)
	raw := []byte("tun:\n  route-exclude-address: [192.168.1.0/24]\nrules:\n  - MATCH,DIRECT\n")
	out, err := Apply(raw, ov)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	rules := doc["rules"].([]any)
	if len(rules) != 4 || rules[0] != "IP-CIDR,100.70.0.1/32,"+meshDirect+",no-resolve" || rules[2] != "IP-CIDR6,fd7a:115c:a1e0::2/128,"+meshDirect+",no-resolve" || rules[3] != "MATCH,DIRECT" {
		t.Fatalf("rules: %v", rules)
	}
	excluded := doc["tun"].(map[string]any)["route-exclude-address"].([]any)
	if len(excluded) != 1 || excluded[0] != "192.168.1.0/24" {
		t.Fatalf("exclusions: %v", excluded)
	}
	proxies := doc["proxies"].([]any)
	if proxies[0].(map[string]any)["type"] != "ss" || proxies[0].(map[string]any)["password"] != ov.MeshProxyPassword {
		t.Fatal("mesh did not use authenticated embedded bridge")
	}
	ov.MeshIPs = nil
	out, err = Apply(raw, ov)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), meshDirect) {
		t.Fatal("stale mesh routes survived disconnect")
	}
}
