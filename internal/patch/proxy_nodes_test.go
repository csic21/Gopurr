package patch

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProxyEditsAddNodesAndPreserveIdentity(t *testing.T) {
	port := 443
	edits := &ProxyEdits{Nodes: []NodeEdit{
		{LocalID: "a", Name: proxyString("Local A"), Server: proxyString("a.example.com"), Port: &port, Options: map[string]any{"type": "trojan", "password": "local-secret"}, Aliases: []string{"Previous A"}},
		{LocalID: "b", Name: proxyString("Local B"), Server: proxyString("b.example.com"), Port: &port, Options: map[string]any{"type": "socks5", "dialer-proxy": "Previous A"}},
	}, Groups: []GroupEdit{{Source: "Choice", Members: &[]string{"Local A", "Local B", "Two"}}}}
	raw, err := PreviewProxyEdits([]byte(proxyEditFixture), CloneProxyEdits(edits))
	if err != nil {
		t.Fatal(err)
	}
	nodes, groups, err := ReadProxyEditor(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 4 || nodes[2].Name != "Local A" || nodes[3].Options["dialer-proxy"] != "Local A" || groups[0].Members[0] != "Local A" {
		t.Fatal("added nodes, order or chain references lost")
	}
	if edits.Target("Previous A") != "Local A" {
		t.Fatal("local rename identity lost")
	}
	for _, mutate := range []func(*ProxyEdits){
		func(e *ProxyEdits) { e.Nodes[1].LocalID = "a" },
		func(e *ProxyEdits) { e.Nodes[0].LocalID = "" },
		func(e *ProxyEdits) { e.Nodes[0].Name = proxyString("One") },
		func(e *ProxyEdits) { e.Nodes[0].Options["server"] = "hidden.example.com" },
		func(e *ProxyEdits) { e.Nodes[0].Options["type"] = nil },
		func(e *ProxyEdits) { e.Nodes[0].Port = nil },
		func(e *ProxyEdits) { e.Nodes[0].Aliases = []string{"Two"} },
	} {
		bad := CloneProxyEdits(edits)
		mutate(bad)
		if _, err := PreviewProxyEdits([]byte(proxyEditFixture), bad); err == nil {
			t.Fatal("accepted invalid local node")
		}
	}
}

func TestProxyOptionsPersistNestedDeletionAcrossJSON(t *testing.T) {
	base := map[string]any{"type": "vless", "tls": true, "ws-opts": map[string]any{"path": "/old", "headers": map[string]any{"Host": "old.example.com"}, "max-early-data": 2048}}
	next := MergeProxyOptions(base, map[string]any{"tls": nil, "ws-opts": map[string]any{"path": "/local"}})
	diff := DiffProxyOptions(base, next)
	encoded, _ := json.Marshal(diff)
	if strings.Contains(string(encoded), "max-early-data") || strings.Contains(string(encoded), "Host") {
		t.Fatal("unmodified nested options captured")
	}
	var persisted map[string]any
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		t.Fatal(err)
	}
	refreshed := MergeProxyOptions(base, map[string]any{"ws-opts": map[string]any{"headers": map[string]any{"Host": "fresh.example.com"}}})
	got := MergeProxyOptions(refreshed, persisted)
	transport := got["ws-opts"].(map[string]any)
	if _, ok := got["tls"]; ok {
		t.Fatal("deleted option was restored")
	}
	if transport["path"] != "/local" || transport["headers"].(map[string]any)["Host"] != "fresh.example.com" {
		t.Fatal("nested refresh or edit lost")
	}
	if base["ws-opts"].(map[string]any)["path"] != "/old" {
		t.Fatal("source map mutated")
	}
}
