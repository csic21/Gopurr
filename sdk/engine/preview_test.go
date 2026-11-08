package engine

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopurr/internal/core"
	"gopurr/internal/profile"
)

func previewEngine(t *testing.T, raw string) (*Engine, string) {
	t.Helper()
	home := t.TempDir()
	s, err := profile.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.AddBytesChecked("Offline", []byte(raw), nil)
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{home: home, store: s, core: core.New(filepath.Join(home, "mihomo")), mode: "rule"}
	return e, p.ID
}

func TestDisconnectedGroupsNeverStartCoreAndForgetRuntimeLatency(t *testing.T) {
	e, id := previewEngine(t, directConfig)
	e.manualSelections = map[string]map[string]string{id: {"出口": "直连 B"}}
	e.delays = map[string]uint16{"直连 B": 123}
	var list GroupList
	if err := json.Unmarshal(call(t, e, "proxies.groups", nil), &list); err != nil {
		t.Fatal(err)
	}
	if !list.Preview || list.Incomplete || len(list.Groups) != 2 || list.Groups[0].Now != "直连 B" {
		t.Fatalf("wrong saved preview: %+v", list)
	}
	if e.core.Running() || e.vpn || e.runtime != nil {
		t.Fatal("browsing started a runtime")
	}
	for _, g := range list.Groups {
		for _, m := range g.Members {
			if m.Delay != nil {
				t.Fatal("preview leaked live delay")
			}
		}
	}
	// Updating a saved source must immediately replace the preview, without
	// inheriting a now-removed saved member or the prior runtime's groups.
	p, err := e.store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.home, "profiles", p.File), []byte(strings.ReplaceAll(directConfig, "直连 B", "直连 C")), 0600); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(call(t, e, "proxies.groups", nil), &list)
	if list.Groups[0].Now != "直连 A" || list.Groups[0].Members[1].Name != "直连 C" {
		t.Fatalf("stale preview: %+v", list)
	}
	if e.manualSelections[id]["出口"] != "直连 B" {
		t.Fatal("a read mutated saved choices")
	}
}

func TestDisconnectedPreviewReadsProviderCacheWithoutNetwork(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	raw := `proxies: [{name: local, type: ss, server: secret.example, password: credential}]
proxy-providers:
  saved: {type: http, url: "http://` + listener.Addr().String() + `/private?token=secret", path: providers/cache.yaml, filter: '^HK', override: {additional-prefix: 'cached '}}
  missing: {type: http, url: "http://` + listener.Addr().String() + `/missing", path: providers/missing.yaml}
  inline: {type: inline, payload: [{name: JP inline, type: trojan}]}
proxy-groups:
  - {name: Saved, type: select, proxies: [local], use: [saved, inline]}
  - {name: Missing, type: url-test, use: [missing]}
  - {name: Hidden, type: fallback, hidden: true, include-all-proxies: true, filter: 'local'}
rules: [MATCH,Saved]
`
	e, _ := previewEngine(t, raw)
	path := filepath.Join(e.home, "mihomo/providers/cache.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("proxies:\n  - {name: HK cached, type: ss, password: credential}\n  - {name: US omitted, type: ss}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	data := call(t, e, "proxies.groups", nil)
	var list GroupList
	_ = json.Unmarshal(data, &list)
	if !list.Preview || !list.Incomplete || len(list.Groups[0].Members) != 3 || list.Groups[0].Members[1].Name != "cached HK cached" || list.Groups[1].Now != "" || !list.Groups[2].Hidden {
		t.Fatalf("provider preview: %+v", list)
	}
	for _, secret := range []string{"credential", "secret.example", "token=secret", "private?"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("preview exposed %q", secret)
		}
	}
	_ = listener.(*net.TCPListener).SetDeadline(time.Now().Add(20 * time.Millisecond))
	if conn, err := listener.Accept(); err == nil {
		conn.Close()
		t.Fatal("preview contacted a provider")
	}
	outside := filepath.Join(t.TempDir(), "external.yaml")
	if err := os.WriteFile(outside, []byte("proxies: [{name: outside, type: ss}]"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(call(t, e, "proxies.groups", nil), &list)
	if len(list.Groups[0].Members) != 2 || !list.Incomplete {
		t.Fatal("followed provider cache outside the data directory")
	}
}
