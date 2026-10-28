package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/core"
)

func TestOverviewResolvesNestedExitWithoutInventingSingleRoute(t *testing.T) {
	groups := []core.Group{
		{Name: "GLOBAL", Type: "Selector", Now: "工作"},
		{Name: "工作", Type: "Selector", Now: "自动"},
		{Name: "自动", Type: "URLTest", Now: "日本", Delays: map[string]uint16{"日本": 138}},
	}
	parent, exit, ok := resolveGroupExit(groups, groups[0])
	if !ok || exit != "日本" || parent.Delays[exit] != 138 {
		t.Fatalf("wrong resolved exit or delay source: %+v, %q, %v", parent, exit, ok)
	}
	groups[2].Now = "GLOBAL"
	if _, _, ok := resolveGroupExit(groups, groups[0]); ok {
		t.Fatal("cyclic groups reported a usable exit")
	}
	for _, kind := range []string{"LoadBalance", "Relay"} {
		groups[2].Type, groups[2].Now = kind, "日本"
		if _, _, ok := resolveGroupExit(groups, groups[0]); ok {
			t.Fatalf("%s reported a single exit", kind)
		}
	}
	groups[2].Type, groups[2].Now = "URLTest", ""
	if _, _, ok := resolveGroupExit(groups, groups[0]); ok {
		t.Fatal("unselected automatic group reported a usable exit")
	}
}

func TestOverviewQuickSwitchAndRoutingModes(t *testing.T) {
	a := subscriptionTestApp(t)
	a.booted, a.ticking, a.page = true, true, "overview"
	a.prefs.CloseOnSwitch = false // Scoped connection cleanup has separate controller tests.
	tr := ui.NewTester(a.View, 840, 560)
	tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 88})
	tr.Advance(200 * time.Millisecond)
	if err := tr.Click("切换节点"); err != nil {
		t.Fatal(err)
	}
	if !a.showOverviewNodes {
		t.Fatal("quick picker did not open")
	}
	if err := tr.Click("选择 日本 02"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if a.showOverviewNodes || !tr.HasText("日本 02") {
		t.Fatalf("selection not reflected: open=%v, notice=%q", a.showOverviewNodes, a.notice)
	}
	// A route group remembered from the rule mode must not mask GLOBAL.
	a.modeToken, a.modeIdx = "global", 1
	tr.Frame()
	if a.proxyGroup != "GLOBAL" || !tr.HasText("GLOBAL · 手动选择") {
		t.Fatal("overview does not follow GLOBAL in global mode")
	}
	a.modeToken, a.modeIdx = "direct", 2
	tr.Frame()
	if tr.HasText("切换节点") || !tr.HasText("所有流量直接访问网络") {
		t.Fatal("direct mode still offers a proxy exit")
	}
	// Automatic groups are readable, but their members are not manual actions.
	a.modeToken, a.modeIdx, a.proxyGroup, a.showOverviewNodes = "rule", 0, "自动选择", true
	tr.Frame()
	if _, ok := tr.Find("选择 日本 02"); ok {
		t.Fatal("automatic group offers a manual member selection")
	}
	tr.Key(0, ui.KeyEscape)
	tr.Frame()
	if a.showOverviewNodes {
		t.Fatal("Escape did not close the quick picker")
	}
}
