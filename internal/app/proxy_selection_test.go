package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestProxySelectionIncludesRadioWhitespaceAndLatency(t *testing.T) {
	for _, size := range [][2]int{{1060, 720}, {840, 560}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			dir, err := os.MkdirTemp("", "gp-pick-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			t.Setenv("GOPURR_HOME", dir)
			a := New()
			secret, _ := os.ReadFile(filepath.Join(dir, "core.secret"))
			startFakeCore(t, dir, string(secret))
			a.booted, a.ticking, a.page, a.proxyGroup = true, true, "proxies", "节点选择"
			a.prefs.CloseOnSwitch = false
			a.proxyQuery = "02" // selection must also work in filtered lists
			tr := ui.NewTester(a.View, size[0], size[1])
			tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 88})
			tr.Advance(200 * time.Millisecond)
			current := func() string {
				for _, group := range a.k.Groups() {
					if group.Name == "节点选择" {
						return group.Now
					}
				}
				return ""
			}
			for _, region := range []string{"radio", "name", "whitespace", "latency"} {
				if !a.selectProxy("节点选择", "香港 01") {
					t.Fatal(a.notice)
				}
				tr.Frame()
				r, ok := tr.Find("选择 日本 02")
				if !ok {
					t.Fatal("selection target missing")
				}
				x, y := r.X+20, r.Y+r.H/2
				switch region {
				case "name":
					x = r.X + 60
				case "whitespace":
					x, y = r.X+r.W/2, r.Y+3
				case "latency":
					x = r.X + r.W - 20
				}
				tr.Move(x, y)
				if tr.Cursor() != ui.CursorPointer {
					t.Fatalf("%s does not show a selectable cursor", region)
				}
				tr.Press(x, y)
				tr.Release(x, y)
				tr.Frame()
				if current() != "日本 02" {
					t.Fatalf("clicking %s did not select the node: %s", region, current())
				}
			}
			// The node retains focus after pointer selection. Both activation
			// keys must select without starting a latency test.
			for _, key := range []ui.Key{ui.KeySpace, ui.KeyEnter} {
				a.selectProxy("节点选择", "香港 01")
				tr.Frame()
				tr.Key(0, key)
				if current() != "日本 02" {
					t.Fatalf("keyboard selection failed for %v", key)
				}
			}
			a.selectProxy("节点选择", "香港 01")
			tr.Frame()
			a.bmu.Lock()
			beforeTest := a.testRun
			a.bmu.Unlock()
			if err := tr.Click("测试 日本 02"); err != nil {
				t.Fatal(err)
			}
			if current() != "香港 01" {
				t.Fatal("latency button also changed the selected node")
			}
			a.bmu.Lock()
			afterTest := a.testRun
			a.bmu.Unlock()
			if afterTest != beforeTest+1 {
				t.Fatal("latency button did not start its own test")
			}
			a.cancelDelayTest()
		})
	}
}
