package patch

import (
	"bytes"
	"encoding/json"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

const sub = `mixed-port: 7890
allow-lan: true
mode: global
proxies: []
proxy-groups: []
rules:
  - MATCH,DIRECT
`

func TestApplyKeepsEverythingElse(t *testing.T) {
	out, err := Apply([]byte(sub), Default())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["mode"] != "rule" {
		t.Fatalf("mode not overridden: %v", doc["mode"])
	}
	if doc["allow-lan"] != false {
		t.Fatalf("allow-lan not overridden: %v", doc["allow-lan"])
	}
	rules, _ := doc["rules"].([]any)
	if len(rules) != 1 || rules[0] != "MATCH,DIRECT" {
		t.Fatalf("rules lost: %v", doc["rules"])
	}
	if doc["unified-delay"] != true {
		t.Fatalf("unified-delay: %v", doc["unified-delay"])
	}
}

func TestApplyTun(t *testing.T) {
	ov := Default()
	ov.TunEnable = true
	out, err := Apply([]byte(sub), ov)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	tun, ok := doc["tun"].(map[string]any)
	if !ok {
		t.Fatalf("no tun section: %v", doc["tun"])
	}
	if tun["enable"] != true {
		t.Fatalf("tun not enabled: %v", tun)
	}
	if tun["stack"] != "mixed" || tun["auto-route"] != true {
		t.Fatalf("tun keys: %v", tun)
	}
	dns, ok := doc["dns"].(map[string]any)
	if !ok || dns["enhanced-mode"] != "fake-ip" {
		t.Fatalf("dns: %v", doc["dns"])
	}
}

func TestApplyBadYAML(t *testing.T) {
	if _, err := Apply([]byte("a: [unclosed"), Default()); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMixedPortReplacesSubscriptionListeners(t *testing.T) {
	raw := []byte("port: 7890\nsocks-port: 7891\nproxies: []\nrules: [MATCH,DIRECT]\n")
	out, err := Apply(raw, Default())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["mixed-port"] != 7890 {
		t.Fatalf("mixed listener = %v", doc["mixed-port"])
	}
	for _, key := range []string{"port", "socks-port"} {
		if value, ok := doc[key]; ok && value != 0 {
			t.Errorf("subscription listener %s is still enabled: %v", key, value)
		}
	}

	// Without an application-owned mixed port, keep the original listeners.
	ov := Default()
	ov.MixedPort = 0
	out, err = Apply(raw, ov)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["port"] != 7890 || doc["socks-port"] != 7891 {
		t.Fatalf("original listeners changed: port=%v socks-port=%v", doc["port"], doc["socks-port"])
	}
}

func TestMixedPort(t *testing.T) {
	if got := MixedPort([]byte(sub)); got != 7890 {
		t.Fatalf("MixedPort = %d", got)
	}
	if got := MixedPort([]byte("mode: rule\n")); got != 0 {
		t.Fatalf("MixedPort without key = %d", got)
	}
}

func TestApplyEmptyDoc(t *testing.T) {
	out, err := Apply(nil, Default())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "mixed-port: 7890") {
		t.Fatalf("empty config: %s", out)
	}
}

func appliedDoc(t *testing.T, raw []byte, ov Overrides) map[string]any {
	t.Helper()
	out, err := Apply(raw, ov)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestApplyMergePrecedenceAndRuleOrder(t *testing.T) {
	raw := []byte(`port: 7890
socks-port: 7891
mixed-port: 7892
mode: global
proxies:
  - {name: OldNode, type: direct}
proxy-groups:
  - {name: OldGroup, type: select, proxies: [OldNode]}
rules:
  - DOMAIN,subscription.example,OldGroup
  - MATCH,DIRECT
dns:
  nameserver: [1.1.1.1, 223.5.5.5]
  fallback: [9.9.9.9]
  fallback-filter:
    geoip: true
    ipcidr: [240.0.0.0/4]
profile:
  store-selected: true
  store-fake-ip: true
external-controller: 127.0.0.1:9090
secret: subscription-secret
metadata:
  note: subscription note
  untouched: retained
`)
	original := bytes.Clone(raw)
	ov := Default()
	ov.MixedPort = 7899
	ov.TunEnable = true
	ov.MergeYAML = `mixed-port: 9000
allow-lan: true
ipv6: true
unified-delay: false
mode: direct
tun:
  enable: false
  stack: system
  auto-route: false
  auto-detect-interface: false
  mtu: 1400
  dns-hijack: [udp://any:53]
dns:
  nameserver: [8.8.8.8]
  fallback-filter:
    geoip: false
profile:
  store-selected: false
metadata:
  note: ""
proxies:
  - {name: Node, type: direct}
proxy-groups:
  - {name: Route, type: select, proxies: [Node]}
rules:
  - DOMAIN,merged.example,Route
  - MATCH,DIRECT
`
	ov.ExtraRules = []string{
		" DOMAIN-SUFFIX,first.example,Route ",
		"DOMAIN,second.example,Node",
		"IP-CIDR,10.0.0.0/8,DIRECT,no-resolve",
	}
	doc := appliedDoc(t, raw, ov)
	if !bytes.Equal(raw, original) || ov.ExtraRules[0] != " DOMAIN-SUFFIX,first.example,Route " {
		t.Fatal("Apply mutated the subscription or overrides")
	}
	for key, want := range map[string]any{
		"mixed-port": 7899, "mode": "rule", "allow-lan": false,
		"ipv6": false, "unified-delay": true,
		"external-controller": "127.0.0.1:9090", "secret": "subscription-secret",
	} {
		if doc[key] != want {
			t.Errorf("%s = %v, want %v", key, doc[key], want)
		}
	}
	for _, key := range []string{"port", "socks-port"} {
		if _, ok := doc[key]; ok {
			t.Errorf("conflicting listener %s was retained", key)
		}
	}
	wantRules := []any{
		"DOMAIN-SUFFIX,first.example,Route", "DOMAIN,second.example,Node",
		"IP-CIDR,10.0.0.0/8,DIRECT,no-resolve", "DOMAIN,merged.example,Route", "MATCH,DIRECT",
	}
	if !reflect.DeepEqual(doc["rules"], wantRules) {
		t.Errorf("rules = %v, want %v", doc["rules"], wantRules)
	}
	for key, wantName := range map[string]string{"proxies": "Node", "proxy-groups": "Route"} {
		list := doc[key].([]any)
		if len(list) != 1 || list[0].(map[string]any)["name"] != wantName {
			t.Errorf("%s was concatenated instead of replaced: %v", key, list)
		}
	}
	dns := doc["dns"].(map[string]any)
	if !reflect.DeepEqual(dns["nameserver"], []any{"8.8.8.8"}) || !reflect.DeepEqual(dns["fallback"], []any{"9.9.9.9"}) {
		t.Errorf("DNS list replacement or preservation failed: %v", dns)
	}
	filter := dns["fallback-filter"].(map[string]any)
	if filter["geoip"] != false || !reflect.DeepEqual(filter["ipcidr"], []any{"240.0.0.0/4"}) {
		t.Errorf("nested map was not merged: %v", filter)
	}
	if dns["enable"] != true || dns["enhanced-mode"] != "fake-ip" {
		t.Errorf("TUN DNS defaults missing: %v", dns)
	}
	tun := doc["tun"].(map[string]any)
	for key, want := range map[string]any{
		"enable": true, "stack": "mixed", "auto-route": true,
		"auto-detect-interface": true, "mtu": 1400,
	} {
		if tun[key] != want {
			t.Errorf("tun.%s = %v, want %v", key, tun[key], want)
		}
	}
	if !reflect.DeepEqual(tun["dns-hijack"], []any{"udp://any:53"}) {
		t.Errorf("custom TUN fields lost: %v", tun)
	}
	if !reflect.DeepEqual(doc["profile"], map[string]any{"store-selected": false, "store-fake-ip": true}) {
		t.Errorf("profile lost untouched fields: %v", doc["profile"])
	}
	if !reflect.DeepEqual(doc["metadata"], map[string]any{"note": "", "untouched": "retained"}) {
		t.Errorf("empty string was not applied: %v", doc["metadata"])
	}
}

func TestApplyClearsAdditionsAndRebuildsFromSubscription(t *testing.T) {
	ov := Default()
	ov.MergeYAML = "metadata: {note: custom}\nrules: []\n"
	ov.ExtraRules = []string{"DOMAIN,example.com,DIRECT"}
	first := appliedDoc(t, []byte(sub), ov)
	if len(first["rules"].([]any)) != 1 || first["metadata"] == nil {
		t.Fatalf("additions not applied: %v", first)
	}
	ov.MergeYAML = ""
	for _, extra := range [][]string{nil, {}, {"", " \t "}} {
		ov.ExtraRules = extra
		got := appliedDoc(t, []byte(sub), ov)
		want := appliedDoc(t, []byte(sub), Default())
		if !reflect.DeepEqual(got, want) {
			t.Errorf("clearing additions retained stale data: %v", got)
		}
	}
}

func TestApplyRejectsNonMapAndMultipleYAMLDocuments(t *testing.T) {
	for _, raw := range []string{
		"[]", "- item", "plain text", "42", "null", `""`,
		"dns: [unclosed", "dns: {}\ndns: {}", "dns: {}\n---\nrules: []",
		"dns: {}\n---\na: [unclosed",
	} {
		t.Run(raw, func(t *testing.T) {
			if out, err := Apply([]byte(raw), Default()); err == nil || out != nil {
				t.Errorf("invalid subscription accepted: output=%s err=%v", out, err)
			}
			ov := Default()
			ov.MergeYAML = raw
			if out, err := Apply([]byte(sub), ov); err == nil || out != nil || !strings.Contains(err.Error(), "扩展 YAML") {
				t.Errorf("invalid merge accepted or missing context: output=%s err=%v", out, err)
			}
		})
	}
	for _, empty := range []string{"", " \n\t", "# editor notes only\n"} {
		ov := Default()
		ov.MergeYAML = empty
		if _, err := Apply([]byte(sub), ov); err != nil {
			t.Errorf("empty merge should clear additions: %v", err)
		}
	}
}

func TestApplyProtectsApplicationController(t *testing.T) {
	for _, field := range []string{
		"external-controller", "external-controller-unix", "external-controller-tls",
		"external-controller-pipe", "external-controller-cors", "external-controller-routing-mark", "secret",
	} {
		t.Run(field, func(t *testing.T) {
			ov := Default()
			ov.MergeYAML = field + ": null\n"
			if out, err := Apply([]byte(sub), ov); err == nil || out != nil || !strings.Contains(err.Error(), field) {
				t.Errorf("managed controller field accepted: output=%s err=%v", out, err)
			}
		})
	}
	ov := Default()
	ov.MergeYAML = "template: &controller\n  secret: ignored\n<<: *controller\n"
	if _, err := Apply([]byte(sub), ov); err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("YAML merge alias bypassed controller protection: %v", err)
	}
	ov.MergeYAML = "metadata: {secret: ordinary nested value}\n"
	if _, err := Apply([]byte(sub), ov); err != nil {
		t.Fatalf("unrelated nested secret was rejected: %v", err)
	}
}

func TestApplyRejectsBasicSectionTypeErrors(t *testing.T) {
	for _, merge := range []string{
		"rules: not-a-list", "rules: [42]", "rules: ['']", "rules: [{type: MATCH}]",
		"proxy-groups: {}", "proxies: {}", "proxy-groups: [DIRECT]", "proxies: [Node]",
		"proxies: [{name: Node}]", "proxies: [{name: 42, type: direct}]",
		"proxy-groups: [{name: Route, type: select, proxies: DIRECT}]",
		"proxy-groups: [{name: Route, type: select, proxies: [42]}]",
		"proxy-groups: [{name: Route, type: select, use: provider}]",
	} {
		t.Run(merge, func(t *testing.T) {
			ov := Default()
			ov.MergeYAML = merge
			if out, err := Apply([]byte(sub), ov); err == nil || out != nil {
				t.Errorf("invalid section accepted: output=%s err=%v", out, err)
			}
		})
	}
}

const ruleContext = `proxies:
  - {name: Node, type: direct}
proxy-groups:
  - {name: Route, type: select, proxies: [Node]}
rule-providers:
  custom:
    type: http
    behavior: domain
    url: https://example.invalid/rules.yaml
sub-rules:
  named:
    - MATCH,DIRECT
rules:
  - MATCH,DIRECT
`

func TestApplySupportsMihomoRuleFormsOffline(t *testing.T) {
	rules := []string{
		"DOMAIN,example.com,Route", "DOMAIN-SUFFIX,example.com,Node",
		"DOMAIN-KEYWORD,example,DIRECT", `DOMAIN-REGEX,^(?=example)[a-z]{1,8}\.com$,DIRECT`,
		"DOMAIN-WILDCARD,*.example.com,DIRECT", "GEOSITE,offline-data-not-installed,DIRECT",
		"GEOIP,CN,DIRECT,no-resolve", "SRC-GEOIP,CN,DIRECT",
		"IP-ASN,13335,DIRECT,src,no-resolve", "SRC-IP-ASN,13335,DIRECT",
		"IP-CIDR,10.0.0.0/8,DIRECT,no-resolve", "IP-CIDR6,2001:db8::/32,DIRECT",
		"SRC-IP-CIDR,192.0.2.0/24,DIRECT", "IP-SUFFIX,192.0.2.0/24,DIRECT,src",
		"SRC-IP-SUFFIX,2001:db8::/32,DIRECT", "SRC-PORT,1024-65535,DIRECT",
		"DST-PORT,80/443/8000-8010,DIRECT", "IN-PORT,7890,DIRECT", "DSCP,0-63,DIRECT",
		"PROCESS-NAME,curl,DIRECT", "PROCESS-PATH,/usr/bin/curl,DIRECT",
		"PROCESS-NAME-REGEX,^(curl|wget){1,2}$,DIRECT", "PROCESS-PATH-REGEX,^/usr/(bin|local/bin)/(curl|wget)$,DIRECT",
		"PROCESS-NAME-WILDCARD,*curl*,DIRECT", "PROCESS-PATH-WILDCARD,/Applications/*,DIRECT",
		"NETWORK,udp,DIRECT", "IN-TYPE,SOCKS/HTTP,DIRECT", "IN-USER,admin,DIRECT",
		"IN-NAME,mixed-in,DIRECT", "REMATCH-NAME,retry,DIRECT", "RULE-SET,custom,DIRECT,no-resolve,src",
		"SUB-RULE,(NETWORK,TCP),named", "AND,((DOMAIN,example.com),(NETWORK,TCP)),DIRECT",
		"OR,((DOMAIN,example.com),(GEOIP,CN,no-resolve)),DIRECT", "NOT,((DOMAIN,example.com)),DIRECT",
		"AND,((RULE-SET,custom),(OR,((NETWORK,UDP),(GEOSITE,offline-data-not-installed)))),DIRECT",
	}
	for _, target := range []string{"DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE", "PASS-RULE", "GLOBAL"} {
		rules = append(rules, "MATCH,"+target)
	}
	if runtime.GOOS == "linux" || runtime.GOOS == "android" {
		rules = append(rules, "UID,1000/1001-2000,DIRECT")
	}
	ov := Default()
	ov.ExtraRules = rules
	doc := appliedDoc(t, []byte(ruleContext), ov)
	got := doc["rules"].([]any)
	if len(got) != len(rules)+1 {
		t.Fatalf("rules length = %d, want %d", len(got), len(rules)+1)
	}
	for i, want := range rules {
		if got[i] != want {
			t.Errorf("rule %d changed: %q, want %q", i, got[i], want)
		}
	}
}

func TestApplyRejectsInvalidExtraRulesBeforeSaving(t *testing.T) {
	cases := []struct{ rule, contains string }{
		{"DOMAIN,example.com", "格式"}, {"MATCH", "格式"}, {"MATCH,DIRECT,extra", "格式"},
		{"DOMAIN,,DIRECT", "匹配内容"}, {"DOMAIN,example.com,", "格式"},
		{"UNKNOWN,example.com,DIRECT", "unsupported rule type"},
		{"DOMAIN,example.com,MissingPolicy", "MissingPolicy"},
		{"DOMAIN,example.com,DIRECT\nMATCH,DIRECT", "一行"},
		{"IP-CIDR,invalid,DIRECT", "IP-CIDR"}, {"IP-CIDR6,2001:db8::/129,DIRECT", "IP-CIDR6"},
		{"IP-SUFFIX,invalid,DIRECT", "IP-SUFFIX"}, {"NETWORK,icmp,DIRECT", "NETWORK"},
		{"IP-ASN,not-a-number,DIRECT", "ASN"}, {"SRC-IP-ASN,4294967296,DIRECT", "ASN"},
		{"DST-PORT,invalid,DIRECT", "DST-PORT"}, {"DST-PORT,65536,DIRECT", "DST-PORT"},
		{"SRC-PORT,80/65536,DIRECT", "SRC-PORT"}, {"IN-PORT,-1,DIRECT", "IN-PORT"},
		{"DSCP,64,DIRECT", "DSCP"}, {"DSCP,256,DIRECT", "DSCP"},
		{"DOMAIN-REGEX,[,DIRECT", "DOMAIN-REGEX"}, {"PROCESS-PATH-REGEX,(,DIRECT", "PROCESS-PATH-REGEX"},
		{"PROCESS-NAME-REGEX,*,DIRECT", "PROCESS-NAME-REGEX"},
		{"IN-TYPE,unknown,DIRECT", "IN-TYPE"},
		{"IP-CIDR,10.0.0.0/8,DIRECT,no-resovle", "no-resovle"},
		{"DOMAIN,example.com,DIRECT,", "未知规则选项"},
		{"AND,(),DIRECT", "不能为空"}, {"AND,(DOMAIN,example.com),DIRECT", "每个条件"},
		{"AND,((DST-PORT,abc)),DIRECT", "DST-PORT"},
		{"OR,((UNKNOWN,example.com)),DIRECT", "unsupported rule type"},
		{"NOT,((DOMAIN,a),(DOMAIN,b)),DIRECT", "not rule must contain one rule"},
		{"AND,((DOMAIN,example.com),DIRECT", "每个条件"},
		{"RULE-SET,undefined,DIRECT", "undefined"},
		{"OR,((RULE-SET,undefined),(DOMAIN,example.com)),DIRECT", "undefined"},
		{"SUB-RULE,(NETWORK,TCP),undefined", "undefined"},
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "android" {
		cases = append(cases, struct{ rule, contains string }{"UID,1000,DIRECT", "not support this platform"})
	}
	for _, tc := range cases {
		t.Run(tc.rule, func(t *testing.T) {
			ov := Default()
			ov.ExtraRules = []string{"DOMAIN,valid.example,DIRECT", tc.rule}
			out, err := Apply([]byte(ruleContext), ov)
			if err == nil || out != nil || !strings.Contains(err.Error(), "第 2 条额外规则") || !strings.Contains(err.Error(), tc.contains) {
				t.Errorf("output=%s err=%v, want rule index and %q", out, err, tc.contains)
			}
		})
	}
}

func TestApplyValidatesReplacementRulesAndFinalTargets(t *testing.T) {
	for _, rule := range []string{"DST-PORT,invalid,DIRECT", "DOMAIN,example.com,MissingPolicy"} {
		body, err := yaml.Marshal(map[string]any{"rules": []string{rule}})
		if err != nil {
			t.Fatal(err)
		}
		ov := Default()
		ov.MergeYAML = string(body)
		if _, err := Apply([]byte(sub), ov); err == nil || !strings.Contains(err.Error(), "扩展 YAML 中第 1 条") {
			t.Errorf("invalid replacement rules accepted: %v", err)
		}
	}
	ov := Default()
	ov.MergeYAML = "proxies: []\nproxy-groups: []\n"
	ov.ExtraRules = []string{"DOMAIN,example.com,Node"}
	if _, err := Apply([]byte(ruleContext), ov); err == nil || !strings.Contains(err.Error(), "Node") {
		t.Errorf("rule target was checked before merge: %v", err)
	}
	ov.MergeYAML = "rules: null\n"
	ov.ExtraRules = []string{"MATCH,DIRECT"}
	doc := appliedDoc(t, []byte(sub), ov)
	if !reflect.DeepEqual(doc["rules"], []any{"MATCH,DIRECT"}) {
		t.Errorf("extra rules could not replace a null rule list: %v", doc["rules"])
	}
}

func TestOverridesJSONRoundTripAndClear(t *testing.T) {
	var ov Overrides
	if err := json.Unmarshal([]byte(`{"extraRules":["MATCH,DIRECT"],"mergeYaml":"dns: {enable: true}"}`), &ov); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ov.ExtraRules, []string{"MATCH,DIRECT"}) || ov.MergeYAML != "dns: {enable: true}" {
		t.Fatalf("JSON fields not decoded: %+v", ov)
	}
	if err := json.Unmarshal([]byte(`{"extraRules":[],"mergeYaml":""}`), &ov); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(ov)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "extraRules") || strings.Contains(string(out), "mergeYaml") {
		t.Fatalf("cleared fields were not omitted: %s", out)
	}
}
