package patch

import (
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func proxyString(v string) *string       { return &v }
func proxyInt(v int) *int                { return &v }
func proxyStrings(v ...string) *[]string { return &v }

const proxyEditFixture = `proxies:
  - name: One
    type: ss
    server: old.example.com
    port: 443
    password: fresh-secret
    cipher: aes-128-gcm
    plugin-opts: {host: provider.example.com}
  - name: Two
    type: trojan
    server: two.example.com
    port: 8443
    password: token
proxy-groups:
  - name: Choice
    type: select
    proxies: [One, Two, DIRECT]
    icon: https://example.com/icon.png
  - name: Outer
    type: select
    proxies: [Choice, DIRECT]
rules:
  - DOMAIN-SUFFIX,example.com,Choice
  - AND,((DOMAIN,one.example.com),(NETWORK,TCP)),One
  - MATCH,Outer
sub-rules:
  work:
    - DOMAIN,work.example.com,Choice
`

func proxyDocument(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestProxyEditsAreSurgicalAndRenameReferences(t *testing.T) {
	edits := &ProxyEdits{Nodes: []NodeEdit{{Source: "One", Name: proxyString("Renamed"), Server: proxyString("local.example.com"), Port: proxyInt(9443)}}, Groups: []GroupEdit{{Source: "Choice", Name: proxyString("Work")}}}
	raw, err := PreviewProxyEdits([]byte(proxyEditFixture), edits)
	if err != nil {
		t.Fatal(err)
	}
	doc := proxyDocument(t, raw)
	node := proxyEntries(doc, "proxies")[0].(map[string]any)
	if node["password"] != "fresh-secret" || node["cipher"] != "aes-128-gcm" || node["plugin-opts"] == nil {
		t.Fatal("unknown protocol fields lost")
	}
	if node["name"] != "Renamed" || node["server"] != "local.example.com" || node["port"] != 9443 {
		t.Fatalf("node edit not applied: %v", node)
	}
	groups := proxyEntries(doc, "proxy-groups")
	if groups[0].(map[string]any)["icon"] == nil || !reflect.DeepEqual(proxyMembers(groups[0].(map[string]any)), []string{"Renamed", "Two", "DIRECT"}) {
		t.Fatal("group unknown fields or members lost")
	}
	if proxyMembers(groups[1].(map[string]any))[0] != "Work" {
		t.Fatal("nested group reference not renamed")
	}
	if !strings.Contains(string(raw), "DOMAIN-SUFFIX,example.com,Work") || !strings.Contains(string(raw), "NETWORK,TCP)),Renamed") || !strings.Contains(string(raw), "DOMAIN,work.example.com,Work") {
		t.Fatal("rule policy not renamed")
	}
	// A later subscription update retains changed credentials and new members.
	updated := strings.ReplaceAll(proxyEditFixture, "fresh-secret", "new-subscription-secret")
	updated = strings.Replace(updated, "[One, Two, DIRECT]", "[One, Two, REJECT, DIRECT]", 1)
	raw, err = PreviewProxyEdits([]byte(updated), edits)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "new-subscription-secret") || !strings.Contains(string(raw), "REJECT") {
		t.Fatal("editor froze refreshed subscription fields")
	}
}

func TestProxyEditsSupportGroupAddOrderAndDeletionChecks(t *testing.T) {
	add := GroupEdit{Name: proxyString("Automatic"), Type: proxyString("url-test"), Members: proxyStrings("Two", "One")}
	raw, err := PreviewProxyEdits([]byte(proxyEditFixture), &ProxyEdits{Groups: []GroupEdit{add}})
	if err != nil {
		t.Fatal(err)
	}
	doc := proxyDocument(t, raw)
	groups := proxyEntries(doc, "proxy-groups")
	last := groups[len(groups)-1].(map[string]any)
	if last["url"] == nil || last["interval"] != 300 || !reflect.DeepEqual(proxyMembers(last), []string{"Two", "One"}) {
		t.Fatal("new automatic group invalid or reordered")
	}
	for _, edits := range []*ProxyEdits{
		{Groups: []GroupEdit{{Source: "Choice", Delete: true}}},
		{Groups: []GroupEdit{{Source: "Outer", Members: proxyStrings("Outer")}}},
		{Groups: []GroupEdit{{Source: "Choice", Members: proxyStrings("Outer")}}},
		{Groups: []GroupEdit{{Source: "Choice", Members: proxyStrings("Missing")}}},
		{Groups: []GroupEdit{{Source: "Choice", Members: proxyStrings("One", "One")}}},
		{Groups: []GroupEdit{{Source: "Choice", Members: proxyStrings()}}},
		{Groups: []GroupEdit{{Source: "Choice", Name: proxyString("One")}}},
		{Nodes: []NodeEdit{{Source: "One", Name: proxyString("DIRECT")}}},
		{Nodes: []NodeEdit{{Source: "Missing", Server: proxyString("local.example.com")}}},
		{Nodes: []NodeEdit{{Source: "One", Port: proxyInt(65536)}}},
	} {
		if _, err := PreviewProxyEdits([]byte(proxyEditFixture), edits); err == nil {
			t.Fatalf("invalid edits accepted: %+v", edits)
		}
	}
	// Remove an unused group without rewriting any unrelated source fields.
	source := strings.Replace(proxyEditFixture, "MATCH,Outer", "MATCH,DIRECT", 1)
	raw, err = PreviewProxyEdits([]byte(source), &ProxyEdits{Groups: []GroupEdit{{Source: "Outer", Delete: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "name: Outer") {
		t.Fatal("unused group not deleted")
	}
}

func TestProxyEditsSwapNamesAndPreserveProviderGroups(t *testing.T) {
	edits := &ProxyEdits{Nodes: []NodeEdit{{Source: "One", Name: proxyString("Two")}, {Source: "Two", Name: proxyString("One")}}}
	raw, err := PreviewProxyEdits([]byte(proxyEditFixture), edits)
	if err != nil {
		t.Fatal(err)
	}
	doc := proxyDocument(t, raw)
	if !reflect.DeepEqual(proxyMembers(proxyEntries(doc, "proxy-groups")[0].(map[string]any)), []string{"Two", "One", "DIRECT"}) {
		t.Fatal("simultaneous rename did not swap references")
	}
	source := `proxy-providers: {remote: {type: http, url: https://example.com}}
proxy-groups: [{name: Remote, type: select, use: [remote]}]
rules: ["MATCH,Remote"]`
	if _, err := PreviewProxyEdits([]byte(source), &ProxyEdits{Groups: []GroupEdit{{Source: "Remote", Name: proxyString("Provider")}}}); err != nil {
		t.Fatal(err)
	}
}

func TestProxyEditsCloneAndAppPolicyRewrite(t *testing.T) {
	edits := &ProxyEdits{Nodes: []NodeEdit{{Source: "One", Name: proxyString("Local")}}, Groups: []GroupEdit{{Source: "Choice", Name: proxyString("Work"), Members: proxyStrings("Local", "Two")}}}
	clone := CloneProxyEdits(edits)
	(*clone.Groups[0].Members)[0] = "DIRECT"
	*clone.Nodes[0].Name = "Different"
	if *edits.Nodes[0].Name != "Local" || (*edits.Groups[0].Members)[0] != "Local" {
		t.Fatal("clone aliases preferences")
	}
	want := []string{"DOMAIN,x,Work", "IP-CIDR,10.0.0.0/8,Local,no-resolve", "AND,((DOMAIN,x),(NETWORK,TCP)),Work", "SUB-RULE,(NETWORK,TCP),Choice"}
	got := RewriteProxyRuleTargets([]string{"DOMAIN,x,Choice", "IP-CIDR,10.0.0.0/8,One,no-resolve", "AND,((DOMAIN,x),(NETWORK,TCP)),Choice", "SUB-RULE,(NETWORK,TCP),Choice"}, edits)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("policies: %v", got)
	}
	var empty *ProxyEdits
	if empty.Target("DIRECT") != "DIRECT" {
		t.Fatal("nil receiver changed target")
	}
}

func TestProxyEditAliasesPreservePoliciesAndRejectCollisions(t *testing.T) {
	edits := &ProxyEdits{Groups: []GroupEdit{{Source: "Choice", Name: proxyString("Work v2"), Aliases: []string{"Work"}}}}
	if edits.Target("Work") != "Work v2" || edits.Target("Choice") != "Work v2" {
		t.Fatal("prior local policy lost")
	}
	for _, alias := range []string{"One", "Outer", "DIRECT"} {
		bad := CloneProxyEdits(edits)
		bad.Groups[0].Aliases = []string{alias}
		if _, err := PreviewProxyEdits([]byte(proxyEditFixture), bad); err == nil {
			t.Errorf("ambiguous alias %s accepted", alias)
		}
	}
	bad := CloneProxyEdits(edits)
	bad.Nodes = []NodeEdit{{Source: "One", Name: proxyString("Another"), Aliases: []string{"Work"}}}
	if _, err := PreviewProxyEdits([]byte(proxyEditFixture), bad); err == nil {
		t.Fatal("duplicate aliases accepted")
	}
	bad = CloneProxyEdits(edits)
	bad.Groups[0].Aliases = make([]string, 33)
	if _, err := PreviewProxyEdits([]byte(proxyEditFixture), bad); err == nil {
		t.Fatal("unbounded history accepted")
	}
	source := strings.Replace(proxyEditFixture, "proxies: [Choice, DIRECT]", "proxies: [Work, DIRECT]", 1)
	edits.Groups = append(edits.Groups, GroupEdit{Source: "Outer", Members: proxyStrings("Work", "DIRECT")})
	if raw, err := PreviewProxyEdits([]byte(source), edits); err != nil || !strings.Contains(string(raw), "Work v2") {
		t.Fatalf("explicit historical members failed: %s %v", raw, err)
	}
}

func TestProxyEditsRenameDialerPoliciesAndProtectReferencedGroups(t *testing.T) {
	source := strings.Replace(proxyEditFixture, "    password: token", "    password: token\n    dialer-proxy: One", 1)
	source += "proxy-providers:\n  remote:\n    type: http\n    url: https://example.com\n    override: {dialer-proxy: Choice}\nntp: {enable: true, dialer-proxy: Choice}\n"
	edits := &ProxyEdits{Nodes: []NodeEdit{{Source: "One", Name: proxyString("Local One")}}, Groups: []GroupEdit{{Source: "Choice", Name: proxyString("Work")}}}
	raw, err := PreviewProxyEdits([]byte(source), edits)
	if err != nil {
		t.Fatal(err)
	}
	doc := proxyDocument(t, raw)
	node := proxyEntries(doc, "proxies")[1].(map[string]any)
	if node["dialer-proxy"] != "Local One" {
		t.Fatal("outbound chain retained old node identity")
	}
	providers := doc["proxy-providers"].(map[string]any)
	if providers["remote"].(map[string]any)["override"].(map[string]any)["dialer-proxy"] != "Work" || doc["ntp"].(map[string]any)["dialer-proxy"] != "Work" {
		t.Fatal("nested dialer policies retained old group identity")
	}
	unused := strings.Replace(proxyEditFixture, "MATCH,Outer", "MATCH,DIRECT", 1)
	unused = strings.Replace(unused, "    password: token", "    password: token\n    dialer-proxy: Outer", 1)
	if _, err := PreviewProxyEdits([]byte(unused), &ProxyEdits{Groups: []GroupEdit{{Source: "Outer", Delete: true}}}); err == nil {
		t.Fatal("deleted a group still used by outbound dialer")
	}
}

func TestProxyEditsRenameListenerAndTunnelPolicies(t *testing.T) {
	source := strings.Replace(proxyEditFixture, "plugin-opts: {host: provider.example.com}", "plugin-opts: {host: provider.example.com, proxy: One}", 1)
	source += "listeners:\n  - {name: local, type: mixed, port: 7891, proxy: One}\ntunnels:\n  - {network: [tcp], address: '127.0.0.1:9000', target: 'example.com:443', proxy: Choice}\n  - tcp,127.0.0.1:9001,example.com:443,Choice\n"
	edits := &ProxyEdits{Nodes: []NodeEdit{{Source: "One", Name: proxyString("Local One")}}, Groups: []GroupEdit{{Source: "Choice", Name: proxyString("Work v2"), Aliases: []string{"Work"}}}}
	raw, err := PreviewProxyEdits([]byte(source), edits)
	if err != nil {
		t.Fatal(err)
	}
	doc := proxyDocument(t, raw)
	if proxyEntries(doc, "listeners")[0].(map[string]any)["proxy"] != "Local One" {
		t.Fatal("listener retained old node identity")
	}
	tunnels := proxyEntries(doc, "tunnels")
	if tunnels[0].(map[string]any)["proxy"] != "Work v2" || !strings.HasSuffix(tunnels[1].(string), ",Work v2") {
		t.Fatal("tunnel retained old policy")
	}
	if proxyEntries(doc, "proxies")[0].(map[string]any)["plugin-opts"].(map[string]any)["proxy"] != "One" {
		t.Fatal("rewrote an unrelated protocol dictionary")
	}
	for _, section := range []string{
		"listeners: [{name: local, type: mixed, port: 7891, proxy: Outer}]\n",
		"tunnels: [{network: [tcp], address: '127.0.0.1:9000', target: 'example.com:443', proxy: Outer}]\n",
		"tunnels: ['tcp,127.0.0.1:9000,example.com:443,Outer']\n",
		"listeners: [{name: local, type: mixed, port: 7891, proxy: Old Outer}]\n",
	} {
		unused := strings.Replace(proxyEditFixture, "MATCH,Outer", "MATCH,DIRECT", 1) + section
		if _, err := PreviewProxyEdits([]byte(unused), &ProxyEdits{Groups: []GroupEdit{{Source: "Outer", Delete: true, Aliases: []string{"Old Outer"}}}}); err == nil {
			t.Fatalf("deleted referenced group for %s", section)
		}
	}
}

func TestProxyEditorBaseKeepsIntermediateTemplateIdentities(t *testing.T) {
	ov := Default()
	ov.MixedPort, ov.Mode, ov.TunEnable = 1234, "global", true
	ov.ExtraRules = []string{"DOMAIN,extra.example.com,DIRECT"}
	ov.Library = []OverrideTemplate{
		{ID: "global", Name: "Global", YAML: "tcp-concurrent: true\nprofile: {store-selected: false}\n"},
		{ID: "local", Name: "Local", YAML: "rules: [\"MATCH,Work\"]\nprofile: {store-selected: true}\n"},
	}
	ov.TemplateIDs = []string{"global"}
	ov.MergeYAML = "unified-delay: false\n"
	local := ProfileOverrides{TemplateIDs: []string{"local"}, MergeYAML: "profile: {store-fake-ip: true}\n", ExtraRules: []string{"DOMAIN,local.example.com,DIRECT"}, ProxyEdits: &ProxyEdits{Groups: []GroupEdit{{Source: "Choice", Name: proxyString("Work")}}}}
	source := "mixed-port: 7890\nmode: rule\ndns: {enable: false}\nexternal-controller: '127.0.0.1:9999'\n" + proxyEditFixture
	raw, err := ProxyEditorBase([]byte(source), ov, local)
	if err != nil {
		t.Fatal(err)
	}
	doc := proxyDocument(t, raw)
	if doc["mixed-port"] != 7890 || doc["mode"] != "rule" || doc["tun"] != nil || doc["external-controller"] != nil {
		t.Fatal("intermediate stage applied managed runtime values or retained controller")
	}
	profile := doc["profile"].(map[string]any)
	if profile["store-selected"] != true || profile["store-fake-ip"] != true || doc["tcp-concurrent"] != true || doc["unified-delay"] != false {
		t.Fatal("template/inline layer order differs from runtime composition")
	}
	groups := proxyEntries(doc, "proxy-groups")
	if groups[0].(map[string]any)["name"] != "Choice" || !reflect.DeepEqual(proxyEntries(doc, "rules"), []any{"MATCH,Work"}) {
		t.Fatal("base applied edits or app/extra rules")
	}
	local.MergeYAML = "external-controller: '127.0.0.1:9999'"
	if _, err := ProxyEditorBase([]byte(source), ov, local); err == nil {
		t.Fatal("base bypassed controller restriction")
	}
	local.MergeYAML = "proxies: broken"
	if _, err := ProxyEditorBase([]byte(source), ov, local); err == nil {
		t.Fatal("base skipped structural validation")
	}
}

func TestSubscriptionRefreshPreservesLocalVPSNodesAndDeduplicatesAppendMembers(t *testing.T) {
	subV1 := `proxies:
  - name: SubHK
    type: ss
    server: hk.provider.example
    port: 443
    cipher: aes-128-gcm
    password: secret-v1
  - name: SubJP
    type: trojan
    server: jp.provider.example
    port: 443
    password: pass-v1
proxy-groups:
  - name: Proxy
    type: select
    proxies: [SubHK, SubJP, DIRECT]
  - name: Auto
    type: url-test
    proxies: [SubHK, SubJP]
    url: https://www.gstatic.com/generate_204
    interval: 300
rules:
  - DOMAIN-SUFFIX,google.com,Proxy
  - MATCH,DIRECT
`
	// User adds a personal VPS node and appends it to Proxy & Auto via AppendMembers,
	// plus also appends a node name "SubSG" that upstream does not yet have in Proxy.
	edits := &ProxyEdits{
		Nodes: []NodeEdit{
			{
				Source:  "",
				LocalID: "vps-tokyo-1",
				Name:    proxyString("MyVPS Tokyo"),
				Server:  proxyString("203.0.113.10"),
				Port:    proxyInt(443),
				Options: map[string]any{
					"type":       "vless",
					"uuid":       "11111111-2222-3333-4444-555555555555",
					"tls":        true,
					"servername": "vps.example.com",
				},
			},
		},
		Groups: []GroupEdit{
			{Source: "Proxy", AppendMembers: []string{"MyVPS Tokyo"}},
			{Source: "Auto", AppendMembers: []string{"MyVPS Tokyo", "SubHK"}},
		},
	}

	// Verify v1: MyVPS Tokyo is added, and SubHK in Auto's AppendMembers deduplicates against inherited SubHK!
	rawV1, err := ApplyLayers([]byte(subV1), Default(), ProfileOverrides{ProxyEdits: edits})
	if err != nil {
		t.Fatalf("v1 apply failed: %v", err)
	}
	docV1 := proxyDocument(t, rawV1)
	groupsV1 := proxyEntries(docV1, "proxy-groups")
	if !reflect.DeepEqual(proxyMembers(groupsV1[0].(map[string]any)), []string{"SubHK", "SubJP", "DIRECT", "MyVPS Tokyo"}) {
		t.Fatalf("v1 Proxy members unexpected: %v", proxyMembers(groupsV1[0].(map[string]any)))
	}
	if !reflect.DeepEqual(proxyMembers(groupsV1[1].(map[string]any)), []string{"SubHK", "SubJP", "MyVPS Tokyo"}) {
		t.Fatalf("v1 Auto members did not auto-deduplicate SubHK: %v", proxyMembers(groupsV1[1].(map[string]any)))
	}

	// Now upstream subscription refreshes: SubJP is removed, SubSG and SubUS are added,
	// credentials rotate, AND subscription rules are updated with new domains.
	subV2 := `proxies:
  - name: SubHK
    type: ss
    server: hk.provider.example
    port: 443
    cipher: aes-128-gcm
    password: secret-v2
  - name: SubSG
    type: trojan
    server: sg.provider.example
    port: 443
    password: pass-v2
  - name: SubUS
    type: trojan
    server: us.provider.example
    port: 443
    password: pass-v2
proxy-groups:
  - name: Proxy
    type: select
    proxies: [SubHK, SubSG, SubUS, DIRECT]
  - name: Auto
    type: url-test
    proxies: [SubHK, SubSG, SubUS]
    url: https://www.gstatic.com/generate_204
    interval: 300
rules:
  - DOMAIN-SUFFIX,google.com,Proxy
  - DOMAIN-SUFFIX,openai.com,Proxy
  - MATCH,DIRECT
`
	rawV2, err := ApplyLayers([]byte(subV2), Default(), ProfileOverrides{ProxyEdits: edits})
	if err != nil {
		t.Fatalf("v2 refresh apply failed: %v", err)
	}
	docV2 := proxyDocument(t, rawV2)
	// Confirm MyVPS Tokyo survived intact alongside refreshed upstream nodes.
	var nodeNames []string
	var foundVPS map[string]any
	for _, entry := range proxyEntries(docV2, "proxies") {
		m := entry.(map[string]any)
		name := m["name"].(string)
		nodeNames = append(nodeNames, name)
		if name == "MyVPS Tokyo" {
			foundVPS = m
		}
	}
	if !reflect.DeepEqual(nodeNames, []string{"SubHK", "SubSG", "SubUS", "MyVPS Tokyo"}) {
		t.Fatalf("v2 nodes unexpected: %v", nodeNames)
	}
	if foundVPS == nil || foundVPS["server"] != "203.0.113.10" || foundVPS["uuid"] != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("VPS node fields altered after refresh: %v", foundVPS)
	}
	groupsV2 := proxyEntries(docV2, "proxy-groups")
	if !reflect.DeepEqual(proxyMembers(groupsV2[0].(map[string]any)), []string{"SubHK", "SubSG", "SubUS", "DIRECT", "MyVPS Tokyo"}) {
		t.Fatalf("v2 Proxy members unexpected: %v", proxyMembers(groupsV2[0].(map[string]any)))
	}
	if !reflect.DeepEqual(proxyMembers(groupsV2[1].(map[string]any)), []string{"SubHK", "SubSG", "SubUS", "MyVPS Tokyo"}) {
		t.Fatalf("v2 Auto members unexpected: %v", proxyMembers(groupsV2[1].(map[string]any)))
	}
	if !strings.Contains(string(rawV2), "DOMAIN-SUFFIX,openai.com,Proxy") || !strings.Contains(string(rawV2), "secret-v2") {
		t.Fatalf("updated subscription rules or credentials lost: %s", rawV2)
	}

	// Explicit duplicate INSIDE AppendMembers must still be rejected.
	dupAppend := CloneProxyEdits(edits)
	dupAppend.Groups[0].AppendMembers = []string{"MyVPS Tokyo", "MyVPS Tokyo"}
	if _, err := PreviewProxyEdits([]byte(subV2), dupAppend); err == nil {
		t.Fatal("duplicate inside AppendMembers was accepted")
	}
	// Explicit duplicate INSIDE Members must still be rejected.
	dupExplicit := &ProxyEdits{Groups: []GroupEdit{{Source: "Proxy", Members: proxyStrings("SubHK", "SubHK")}}}
	if _, err := PreviewProxyEdits([]byte(subV2), dupExplicit); err == nil {
		t.Fatal("duplicate inside explicit Members was accepted")
	}
}

func TestDeleteLocalAndBaseNodeCleansGroupsAndProtectsReferences(t *testing.T) {
	// 1. Overlay local node deletion via PruneDeletedLocalNodeRefs + ValidateDeletedNodeRefs
	prev := &ProxyEdits{
		Nodes: []NodeEdit{
			{Source: "", LocalID: "vps-1", Name: proxyString("MyVPS"), Server: proxyString("1.2.3.4"), Port: proxyInt(443), Options: map[string]any{"type": "trojan", "password": "pw"}},
			{Source: "", LocalID: "vps-2", Name: proxyString("RelayVPS"), Server: proxyString("5.6.7.8"), Port: proxyInt(443), Options: map[string]any{"type": "trojan", "password": "pw", "dialer-proxy": "MyVPS"}},
		},
		Groups: []GroupEdit{
			{Source: "Choice", AppendMembers: []string{"MyVPS"}},
			{Name: proxyString("OnlyVPS"), Type: proxyString("select"), Members: proxyStrings("MyVPS")},
		},
	}
	// User removes MyVPS from Nodes, leaving stale refs in Groups.
	next := CloneProxyEdits(prev)
	next.Nodes = next.Nodes[1:] // keep RelayVPS, drop MyVPS
	pruned, deletedNames := PruneDeletedLocalNodeRefs(prev, next)
	if !reflect.DeepEqual(deletedNames, []string{"MyVPS"}) {
		t.Fatalf("deletedNames = %v, want [MyVPS]", deletedNames)
	}
	// Choice's no-op AppendMembers GroupEdit was dropped; OnlyVPS has empty Members -> rejected!
	if _, err := PreviewProxyEdits([]byte(proxyEditFixture), pruned); err == nil {
		t.Fatal("deleting sole member of OnlyVPS should fail with empty group error")
	}
	// Give OnlyVPS another member, but RelayVPS still has dialer-proxy: MyVPS -> ValidateDeletedNodeRefs catches it!
	*pruned.Groups[0].Members = []string{"RelayVPS"}
	compiled, err := PreviewProxyEdits([]byte(proxyEditFixture), pruned)
	if err != nil {
		t.Fatalf("unexpected preview error: %v", err)
	}
	if err := ValidateDeletedNodeRefs(compiled, deletedNames); err == nil {
		t.Fatal("dangling dialer-proxy to deleted local node was not caught")
	}
	// Remove dialer-proxy from RelayVPS -> succeeds cleanly.
	delete(pruned.Nodes[0].Options, "dialer-proxy")
	compiled, err = PreviewProxyEdits([]byte(proxyEditFixture), pruned)
	if err != nil {
		t.Fatalf("unexpected error after fixing dialer: %v", err)
	}
	if err := ValidateDeletedNodeRefs(compiled, deletedNames); err != nil {
		t.Fatalf("unexpected deleted ref error after cleanup: %v", err)
	}

	// 2. Pure local YAML base node deletion via NodeEdit{Source: "...", Delete: true}
	localYAML := `proxies:
  - name: NodeA
    type: ss
    server: a.example.com
    port: 443
    cipher: aes-128-gcm
    password: p
  - name: NodeB
    type: ss
    server: b.example.com
    port: 443
    cipher: aes-128-gcm
    password: p
proxy-groups:
  - name: Gopurr Select
    type: select
    proxies: [Gopurr Auto, NodeA, NodeB]
  - name: Gopurr Auto
    type: url-test
    proxies: [NodeA, NodeB]
    url: https://www.gstatic.com/generate_204
    interval: 300
rules:
  - MATCH,Gopurr Select
`
	out, err := PreviewProxyEdits([]byte(localYAML), &ProxyEdits{
		Nodes: []NodeEdit{{Source: "NodeA", Delete: true}},
	})
	if err != nil {
		t.Fatalf("base node delete failed: %v", err)
	}
	doc := proxyDocument(t, out)
	if len(proxyEntries(doc, "proxies")) != 1 {
		t.Fatalf("NodeA not removed: %v", doc["proxies"])
	}
	for _, g := range proxyEntries(doc, "proxy-groups") {
		if strings.Contains(strings.Join(proxyMembers(g.(map[string]any)), ","), "NodeA") {
			t.Fatalf("group retained deleted NodeA: %v", g)
		}
	}
	// Deleting BOTH NodeA and NodeB empties Gopurr Auto -> must fail!
	if _, err := PreviewProxyEdits([]byte(localYAML), &ProxyEdits{
		Nodes: []NodeEdit{{Source: "NodeA", Delete: true}, {Source: "NodeB", Delete: true}},
	}); err == nil {
		t.Fatal("emptying Gopurr Auto by deleting all nodes was accepted")
	}
	// Deleting a node referenced by dialer-proxy or rules -> must fail!
	withRule := strings.Replace(localYAML, "MATCH,Gopurr Select", "DOMAIN,x.com,NodeA\n  - MATCH,Gopurr Select", 1)
	if _, err := PreviewProxyEdits([]byte(withRule), &ProxyEdits{
		Nodes: []NodeEdit{{Source: "NodeA", Delete: true}},
	}); err == nil {
		t.Fatal("deleting node referenced by rule was accepted")
	}
	withDialer := strings.Replace(localYAML, "server: b.example.com", "server: b.example.com\n    dialer-proxy: NodeA", 1)
	if _, err := PreviewProxyEdits([]byte(withDialer), &ProxyEdits{
		Nodes: []NodeEdit{{Source: "NodeA", Delete: true}},
	}); err == nil {
		t.Fatal("deleting node referenced by dialer-proxy was accepted")
	}
}
