package patch

import (
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestProfileLayersOrderAndManagedNetwork(t *testing.T) {
	source := []byte("dns: {enable: true, nameserver: [9.9.9.9]}\nrules: [\"MATCH,DIRECT\"]\n")
	global := Default()
	global.MergeYAML = "dns: {nameserver: [8.8.8.8]}\nprofile: {store-selected: true}\n"
	global.DNS = &DNSSettings{Enable: true, EnhancedMode: "fake-ip", Nameserver: []string{"1.1.1.1"}, DefaultNameserver: []string{"1.1.1.1"}}
	global.ExtraRules = []string{"DOMAIN,global.example,DIRECT"}
	local := ProfileOverrides{MergeYAML: "dns: {nameserver: [223.5.5.5]}\nmixed-port: 9999\nmode: direct\ntun: {enable: true}\n", ExtraRules: []string{"DOMAIN,local.example,REJECT"}}
	composed, err := ApplyLayers(source, global, local)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(composed, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["mixed-port"] != 7890 || doc["mode"] != "rule" || doc["tun"].(map[string]any)["enable"] != false {
		t.Fatal("subscription changed app network settings")
	}
	if !reflect.DeepEqual(doc["rules"], []any{"DOMAIN,local.example,REJECT", "DOMAIN,global.example,DIRECT", "MATCH,DIRECT"}) {
		t.Fatalf("rule precedence: %v", doc["rules"])
	}
	if !reflect.DeepEqual(doc["dns"].(map[string]any)["nameserver"], []any{"223.5.5.5"}) {
		t.Fatal("subscription YAML did not override global DNS")
	}
	dns := *global.DNS
	dns.Nameserver = []string{"119.29.29.29"}
	local.DNS = &dns
	composed, err = ApplyLayers(source, global, local)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(composed), "119.29.29.29") || strings.Contains(string(composed), "223.5.5.5") {
		t.Fatal("subscription DNS form did not override YAML")
	}
}

func TestProfileLayerRejectsUnknownTargetsAndControllers(t *testing.T) {
	for _, local := range []ProfileOverrides{
		{ExtraRules: []string{"DOMAIN,example.com,missing-group"}},
		{MergeYAML: "external-controller-pipe: test"},
		{MergeYAML: "secret: test"},
		{MergeYAML: "rules: [DOMAIN,missing]"},
	} {
		if _, err := ApplyLayers([]byte("rules: [\"MATCH,DIRECT\"]"), Default(), local); err == nil {
			t.Fatalf("invalid layer accepted: %+v", local)
		}
	}
	global := Default()
	global.ExtraRules = []string{"DOMAIN,example.com,Work"}
	raw := []byte("proxy-groups: [{name: Work, type: select, proxies: [DIRECT]}]\nrules: [\"MATCH,DIRECT\"]")
	if _, err := ApplyLayers(raw, global, ProfileOverrides{MergeYAML: "proxy-groups: []"}); err == nil || !strings.Contains(err.Error(), "Work") {
		t.Fatal("global rule target removed by subscription was not checked")
	}
}
