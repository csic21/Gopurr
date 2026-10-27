package core

import (
	"fmt"
	"github.com/metacubex/mihomo/component/profile/cachefile"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// TestKernelLifecycle is the headless version of "does the client work":
// start a real kernel in this process, route through it, read its state, then
// shut it down.
func TestKernelLifecycle(t *testing.T) {
	home := t.TempDir()
	c := New(home)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer origin.Close()

	port := freePort(t)
	cfg := fmt.Sprintf(`mixed-port: %d
allow-lan: false
mode: rule
log-level: warning
ipv6: false
proxies: []
proxy-groups:
  - name: 节点选择
    type: select
    proxies: [DIRECT]
rules:
  - MATCH,节点选择
`, port)

	if err := c.Start([]byte(cfg)); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		c.Stop()
		// The in-process test owns mihomo's singleton cache; Windows cannot
		// remove the temporary home until this handle is closed.
		if db := cachefile.Cache().DB; db != nil {
			_ = db.Close()
		}
	}()

	// The listener comes up asynchronously once the config is applied.
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("kernel never listened on %d", port)
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !c.Running() {
		t.Fatal("kernel reports not running")
	}
	if got := c.Mode(); got != "rule" {
		t.Fatalf("mode = %q", got)
	}

	rules := c.Rules()
	// MATCH carries no payload; the adapter is the interesting part.
	if len(rules) != 1 || rules[0].Type != "Match" || rules[0].Adapter != "节点选择" {
		t.Fatalf("rules = %+v", rules)
	}

	groups := c.Groups()
	if len(groups) == 0 {
		t.Fatal("no proxy groups")
	}
	found := false
	for _, g := range groups {
		if g.Name == "节点选择" {
			found = true
			if g.Now != "DIRECT" {
				t.Fatalf("selector now = %q", g.Now)
			}
		}
	}
	if !found {
		t.Fatalf("group missing: %+v", groups)
	}

	if err := c.Select("节点选择", "DIRECT"); err != nil {
		t.Fatalf("select: %v", err)
	}
	if err := c.Select("不存在", "DIRECT"); err == nil {
		t.Fatal("expected error for unknown group")
	}

	// DIRECT has no handshake, so this measures the local round trip.
	d, err := c.Delay("DIRECT", origin.URL)
	if err != nil {
		t.Fatalf("delay: %v", err)
	}
	if d == 0 {
		t.Fatal("delay reported 0ms")
	}

	sample := c.Sample()
	if sample.Mem == 0 {
		t.Log("memory counter unavailable on this platform")
	}

	c.Stop()
	if c.Running() {
		t.Fatal("still running after stop")
	}
}
