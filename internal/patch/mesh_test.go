package patch

import (
	"reflect"
	"strings"
	"testing"

	"github.com/metacubex/mihomo/config"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/tunnel"
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

func TestMeshRoutesWorkInDirectAndGlobalModes(t *testing.T) {
	previousHome := C.Path.HomeDir()
	C.SetHomeDir(t.TempDir())
	t.Cleanup(func() { C.SetHomeDir(previousHome) })

	raw := []byte("proxies:\n  - {name: ordinary, type: socks5, server: 127.0.0.1, port: 1080}\nrules:\n  - DOMAIN,example.com,ordinary\n  - MATCH,ordinary\n")
	for _, tc := range []struct {
		mode     string
		fallback string
	}{
		{mode: "direct", fallback: "MATCH,DIRECT"},
		{mode: "global", fallback: "MATCH,GLOBAL"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			ov := Default()
			ov.TunEnable = true
			ov.Mode = tc.mode
			ov.MeshIPs = []string{"100.70.0.1", "fd7a:115c:a1e0::2"}
			ov.MeshProxyAddress = "127.0.0.1:42123"
			ov.MeshProxyPassword = strings.Repeat("a", 64)

			out, err := Apply(raw, ov)
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := yaml.Unmarshal(out, &doc); err != nil {
				t.Fatal(err)
			}
			if doc["mode"] != "rule" {
				t.Fatalf("mode = %v, want rule", doc["mode"])
			}
			wantRules := []any{
				"IP-CIDR,100.70.0.1/32," + meshDirect + ",no-resolve",
				"IP-CIDR6,fd7a:115c:a1e0::2/128," + meshDirect + ",no-resolve",
				tc.fallback,
				"DOMAIN,example.com,ordinary",
				"MATCH,ordinary",
			}
			if !reflect.DeepEqual(doc["rules"], wantRules) {
				t.Fatalf("rules = %v, want %v", doc["rules"], wantRules)
			}
			parsed, err := config.Parse(out)
			if err != nil {
				t.Fatalf("mihomo rejected %s mesh config: %v\n%s", tc.mode, err, out)
			}
			if parsed.General.Mode != tunnel.Rule || len(parsed.Rules) != len(wantRules) {
				t.Fatalf("unexpected parsed mode=%v rules=%d", parsed.General.Mode, len(parsed.Rules))
			}

			ov.MeshIPs = nil
			disconnected, err := Apply(raw, ov)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(disconnected), meshDirect) {
				t.Fatal("stale mesh routes survived disconnect")
			}
			var after map[string]any
			if err := yaml.Unmarshal(disconnected, &after); err != nil {
				t.Fatal(err)
			}
			if after["mode"] != tc.mode {
				t.Fatalf("disconnect mode = %v, want %s", after["mode"], tc.mode)
			}
			wantDisconnectedRules := []any{"DOMAIN,example.com,ordinary", "MATCH,ordinary"}
			if !reflect.DeepEqual(after["rules"], wantDisconnectedRules) {
				t.Fatalf("disconnect rules = %v, want %v", after["rules"], wantDisconnectedRules)
			}
		})
	}
}
