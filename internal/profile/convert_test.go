package profile

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gopurr/internal/patch"
)

func TestParseProxyInputsSupportsAllVPSURIsAndPartialTolerance(t *testing.T) {
	vmessJSON := `{"v":"2","ps":"VMess Tokyo","add":"vmess.example.com","port":"443","id":"11111111-2222-3333-4444-555555555555","aid":"0","net":"ws","type":"none","host":"cdn.example.com","path":"/ws","tls":"tls"}`
	vmessURI := "vmess://" + base64.StdEncoding.EncodeToString([]byte(vmessJSON))
	ssUser := base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:my-secret"))
	ssrPass := base64.RawURLEncoding.EncodeToString([]byte("ssr-pass"))
	ssrRemarks := base64.RawURLEncoding.EncodeToString([]byte("SSR HK"))
	ssrURI := "ssr://" + base64.RawURLEncoding.EncodeToString([]byte("ssr.example.com:8388:origin:aes-256-cfb:plain:"+ssrPass+"/?remarks="+ssrRemarks))

	lines := []string{
		"vless://11111111-2222-3333-4444-555555555555@vless.example.com:443?security=reality&pbk=pubkey123&sid=abcd&sni=www.microsoft.com&fp=chrome&flow=xtls-rprx-vision&insecure=1#VLESS%20Reality",
		vmessURI,
		"trojan://trojan-pw@trojan.example.com:443?sni=trojan.example.com#Trojan%20SG",
		"ss://" + ssUser + "@ss.example.com:8388#SS%20JP",
		ssrURI,
		"hysteria2://hy2-pass@hy2.example.com:443?sni=hy2.example.com&insecure=1#Hy2%20US",
		"hy2://hy2-pass2@hy2b.example.com:8443#Hy2%20EU",
		"tuic://11111111-2222-3333-4444-555555555555:tuic-pw@tuic.example.com:443?congestion_control=bbr&sni=tuic.example.com#TUIC%20Node",
		"wireguard://privKeyBase64@wg.example.com:51820?publickey=pubKeyBase64&address=10.0.0.2/32,fd00::2/128&mtu=1280&reserved=1,2,3#WG%20VPS",
		"socks5://alice:bob@socks.example.com:1080#Socks%20VPS",
		"http://user1:pass1@http.example.com:8080#HTTP%20VPS",
		"vless://broken-no-host", // invalid line 12
		"not-a-valid-uri",        // invalid line 13
	}

	res := ParseProxyInputs(strings.Join(lines, "\n"))
	if res.Kind != "nodes" {
		t.Fatalf("kind = %q, want nodes", res.Kind)
	}
	if len(res.Nodes) != 11 {
		t.Fatalf("parsed %d nodes, want 11; failures=%+v", len(res.Nodes), res.Failures)
	}
	if len(res.Failures) != 2 || res.Failures[0].Line != 12 || res.Failures[1].Line != 13 {
		t.Fatalf("unexpected failures: %+v", res.Failures)
	}
	if res.Nodes[0].Type != "vless" || res.Nodes[0].Options["skip-cert-verify"] != true {
		t.Fatalf("vless reality node unexpected: %+v", res.Nodes[0])
	}
	if res.Nodes[8].Type != "wireguard" || res.Nodes[8].Options["private-key"] != "privKeyBase64" || res.Nodes[8].Options["public-key"] != "pubKeyBase64" || res.Nodes[8].Options["ip"] != "10.0.0.2" {
		t.Fatalf("wireguard node unexpected: %+v", res.Nodes[8])
	}
	if res.Nodes[9].Type != "socks5" || res.Nodes[9].Options["username"] != "alice" {
		t.Fatalf("socks5 node unexpected: %+v", res.Nodes[9])
	}
	if res.Nodes[10].Type != "http" || res.Nodes[10].Options["password"] != "pass1" {
		t.Fatalf("http node unexpected: %+v", res.Nodes[10])
	}

	// Base64-encoded multi-line URI payload
	b64Nodes := base64.StdEncoding.EncodeToString([]byte(strings.Join(lines[:4], "\n")))
	b64Res := ParseProxyInputs(b64Nodes)
	if b64Res.Kind != "nodes" || !b64Res.Decoded || len(b64Res.Nodes) != 4 {
		t.Fatalf("base64 URIs parse failed: %+v", b64Res)
	}

	// Base64-encoded Clash YAML payload -> re-detected as clash_yaml!
	clashYAML := "proxies:\n  - name: YAMLNode\n    type: trojan\n    server: y.example.com\n    port: 443\n    password: p\nproxy-groups:\n  - name: Proxy\n    type: select\n    proxies: [YAMLNode]\nrules:\n  - MATCH,Proxy\n"
	b64YAML := base64.StdEncoding.EncodeToString([]byte(clashYAML))
	yamlRes := ParseProxyInputs(b64YAML)
	if yamlRes.Kind != "clash_yaml" || !yamlRes.Decoded || len(yamlRes.Nodes) != 1 || yamlRes.Nodes[0].Name != "YAMLNode" {
		t.Fatalf("base64 clash yaml parse failed: %+v", yamlRes)
	}

	// Single proxy YAML map & YAML proxy list
	singleYAML := ParseProxyInputs("name: SingleVPS\ntype: ss\nserver: 1.2.3.4\nport: 443\ncipher: aes-128-gcm\npassword: pw\n")
	if singleYAML.Kind != "nodes" || len(singleYAML.Nodes) != 1 || singleYAML.Nodes[0].Name != "SingleVPS" {
		t.Fatalf("single yaml map failed: %+v", singleYAML)
	}
	listYAML := ParseProxyInputs("- name: ListA\n  type: trojan\n  server: a.example.com\n  port: 443\n  password: p\n")
	if listYAML.Kind != "nodes" || len(listYAML.Nodes) != 1 || listYAML.Nodes[0].Name != "ListA" {
		t.Fatalf("yaml list failed: %+v", listYAML)
	}
}

func TestParseProxyInputsHTTPDisambiguationAndModeOverride(t *testing.T) {
	// Plain HTTP URL in auto mode -> subscription URL (ambiguousHttp: true)
	sub := ParseProxyInputs("https://sub.example.com/clash?token=abc")
	if sub.Kind != "url" || !sub.AmbiguousHTTP || sub.URL != "https://sub.example.com/clash?token=abc" {
		t.Fatalf("auto subscription url failed: %+v", sub)
	}
	// HTTP URL with userinfo -> HTTP proxy node
	httpProxy := ParseProxyInputs("http://u:p@1.2.3.4:8080#MyHTTP")
	if httpProxy.Kind != "nodes" || len(httpProxy.Nodes) != 1 || httpProxy.Nodes[0].Port != 8080 {
		t.Fatalf("http userinfo proxy failed: %+v", httpProxy)
	}
	// Mode override: force plain http://host:port as node
	forcedNode := ParseProxyInputsWithMode("http://1.2.3.4:8080#NoAuthHTTP", "nodes")
	if forcedNode.Kind != "nodes" || len(forcedNode.Nodes) != 1 || forcedNode.Nodes[0].Name != "NoAuthHTTP" {
		t.Fatalf("forced node mode failed: %+v", forcedNode)
	}
	// Mode override: force URL
	forcedURL := ParseProxyInputsWithMode("http://1.2.3.4:8080/sub", "url")
	if forcedURL.Kind != "url" {
		t.Fatalf("forced url mode failed: %+v", forcedURL)
	}
}

func TestCreateLocalUpdateContentAndAddWithOptions(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// 1. CreateLocal with initial VPS node -> valid pure local profile usable by BuildAggregate
	local, err := store.CreateLocal(context.Background(), "My VPS", []patch.ProxyNode{
		{Name: "Tokyo VPS", Type: "trojan", Server: "tokyo.example.com", Port: 443, Options: map[string]any{"type": "trojan", "password": "secret"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !local.IsPureLocal() || local.Kind != KindFile || local.Path != "" {
		t.Fatalf("unexpected pure local profile: %+v", local)
	}
	if store.ActiveID() != local.ID {
		t.Fatal("first created local profile should become active")
	}
	// Verify aggregate.go reads pure local profile cleanly
	aggYAML, stats, err := store.BuildAggregate(AggregateSpec{Sources: []AggregateSource{{ID: local.ID}}})
	if err != nil || stats.Output != 1 || !strings.Contains(string(aggYAML), "Tokyo VPS") {
		t.Fatalf("BuildAggregate on pure local failed: %v %+v", err, stats)
	}

	// 2. AddCheckedWithOptionsContext uses UserAgent and UseProxy on INITIAL download!
	var gotUA string
	var viaProxy bool
	subServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("proxies:\n  - {name: S1, type: ss, server: 1.2.3.4, port: 443, cipher: aes-128-gcm, password: p}\nproxy-groups:\n  - {name: Proxy, type: select, proxies: [S1]}\nrules:\n  - MATCH,Proxy\n"))
	}))
	defer subServer.Close()

	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		viaProxy = true
		resp, err := http.Get(r.URL.String())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		req, _ := http.NewRequest(r.Method, r.URL.String(), nil)
		req.Header = r.Header.Clone()
		resp2, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp2.Body.Close()
		w.WriteHeader(resp2.StatusCode)
		buf := make([]byte, 4096)
		for {
			n, rerr := resp2.Body.Read(buf)
			if n > 0 {
				_, _ = w.Write(buf[:n])
			}
			if rerr != nil {
				break
			}
		}
	}))
	defer proxyServer.Close()

	proxyURL, _ := url.Parse(proxyServer.URL)
	store.SetProxyURL(func() *url.URL { return proxyURL })
	hours := 12
	sub, err := store.AddCheckedWithOptionsContext(context.Background(), subServer.URL+"/sub", "CustomSub", AddOptions{
		UserAgent:   "GopurrTestUA/1.0",
		UseProxy:    true,
		UpdateHours: &hours,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if gotUA != "GopurrTestUA/1.0" || !viaProxy {
		t.Fatalf("initial fetch did not apply UA/proxy: ua=%q viaProxy=%v", gotUA, viaProxy)
	}
	if sub.UserAgent != "GopurrTestUA/1.0" || !sub.UseProxy || sub.UpdateHours == nil || *sub.UpdateHours != 12 {
		t.Fatalf("imported metadata not persisted: %+v", sub)
	}

	// 3. InspectProfile reports accurate counts
	inspect := store.InspectProfile(sub)
	if inspect.NodeCount != 1 || inspect.LocalNodeCount != 0 {
		t.Fatalf("unexpected inspect: %+v", inspect)
	}
}
