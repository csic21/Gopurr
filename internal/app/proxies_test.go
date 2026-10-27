package app

import (
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"gopurr/internal/core"
)

func TestProxyModesAndUniqueNodes(t *testing.T) {
	groups := []core.Group{
		{Name: "GLOBAL", Members: []string{"选择", "自动", "HK", "JP", "DIRECT", "REJECT"}},
		{Name: "选择", Members: []string{"自动", "HK", "JP", "DIRECT"}},
		{Name: "自动", Members: []string{"HK", "JP", "HK"}},
		{Name: "隐藏", Hidden: true, Members: []string{"HK"}},
	}
	if got := uniqueNodes(groups); !reflect.DeepEqual(got, []string{"HK", "JP"}) {
		t.Fatalf("duplicate/group count: %v", got)
	}
	if got := visibleGroups(groups, "rule"); len(got) != 2 || got[0].Name != "选择" {
		t.Fatalf("rule groups = %v", got)
	}
	if got := visibleGroups(groups, "global"); len(got) != 1 || got[0].Name != "GLOBAL" {
		t.Fatalf("global groups = %v", got)
	}
}

func TestProxyAvailabilitySort(t *testing.T) {
	a := &App{delays: map[string]uint16{"slow": 500, "fast": 40, "down": 0}, proxyState: proxyState{proxySort: "延迟优先"}}
	g := core.Group{Members: []string{"unknown", "down", "slow", "fast", "fast"}}
	if got := a.sortedMembers(g); !reflect.DeepEqual(got, []string{"fast", "slow", "unknown", "down"}) {
		t.Fatalf("sort = %v", got)
	}
	a.proxyOnlyLive = true
	if got := a.sortedMembers(g); !reflect.DeepEqual(got, []string{"fast", "slow"}) {
		t.Fatalf("availability filter = %v", got)
	}
	if g.Members[0] != "unknown" || len(g.Members) != 5 {
		t.Fatal("filter changed source group")
	}
}

func TestLargeProxyListBuildsVisibleRows(t *testing.T) {
	t.Setenv("GOPURR_HOME", t.TempDir())
	a := New()
	g := core.Group{Name: "选择", Type: "Selector", Now: "节点 0000"}
	for i := 0; i < 2000; i++ {
		g.Members = append(g.Members, fmt.Sprintf("节点 %04d", i))
	}
	built := 0
	tr := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme(c.Theme().Dark))
		built = 0
		ui.Column(c).Fill().Children(func() {
			ui.List(c, &a.nodeList, len(g.Members)).Fill().ItemKey(func(i int) any { return g.Members[i] }).Rows(func(row ui.ListRow) {
				built++
				a.nodeRow(row.Context, g, g.Members[row.Index])
			})
		})
	}, 650, 440)
	tr.Frame()
	if built < 1 || built > 50 {
		t.Fatalf("built %d rows for a 2000-node list", built)
	}
	if !tr.HasText("节点 0000") || tr.HasText("节点 1999") {
		t.Fatal("list is not limited to visible rows")
	}
}

func TestAppStartsKernelByDefault(t *testing.T) {
	t.Setenv("GOPURR_HOME", t.TempDir())
	t.Setenv("GOPURR_AUTOSTART", "")
	t.Setenv("GOPURR_TUN", "")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	a := New()
	a.ov.MixedPort = ln.Addr().(*net.TCPAddr).Port
	// An occupied port makes the automatic start attempt observable without
	// launching a daemon from the test executable or modifying system proxy.
	tr := ui.NewTester(a.View, 1060, 720)
	tr.Frame()
	if !a.booted || !strings.Contains(a.notice, "已被占用") {
		t.Fatalf("no automatic start attempt: %q", a.notice)
	}
}

func TestSubscriptionSourceHidesCredentials(t *testing.T) {
	source := subscriptionSource("https://user:password@example.com/token/in/path?token=secret#private")
	if strings.Contains(source, "password") || strings.Contains(source, "secret") || strings.Contains(source, "/path") {
		t.Fatalf("credentials exposed: %s", source)
	}
	if !strings.Contains(source, "example.com") {
		t.Fatalf("missing source host: %s", source)
	}
}
