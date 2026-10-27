package app

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestStoppedKernelActions(t *testing.T) {
	// Force a safe, deterministic startup error without spawning a daemon.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	for _, page := range []string{"subs", "proxies"} {
		t.Run(page, func(t *testing.T) {
			t.Setenv("GOPURR_HOME", t.TempDir())
			a := New()
			a.page = page
			a.ov.MixedPort = port
			// Exercise the regular page layout without OS integration or the
			// background loops started by the top-level View.
			tr := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(theme(c.Theme().Dark))
				a.main(c)
			}, 1060, 720)
			tr.Frame()
			if _, ok := tr.Find("启动内核"); !ok {
				t.Fatal("stopped kernel has no start action")
			}
			if err := tr.Click("启动内核"); err != nil {
				t.Fatal(err)
			}
			tr.Frame()
			if !strings.Contains(a.notice, fmt.Sprintf("端口 %d 已被占用", port)) {
				t.Fatalf("missing startup error: %q", a.notice)
			}
			if _, ok := tr.Find(a.notice); !ok {
				t.Fatal("startup error is hidden on the current page")
			}
		})
	}
}

// TestInteraction drives the UI the way a person does: click a sidebar item,
// click a button, watch the state change. It is the part the render test cannot
// cover.
func TestInteraction(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPURR_HOME", dir)

	a := New()
	secret, err := os.ReadFile(filepath.Join(dir, "core.secret"))
	if err != nil {
		t.Fatalf("no secret from the kernel manager: %v", err)
	}
	startFakeCore(t, dir, string(secret))
	a.k.WatchLogs()
	time.Sleep(250 * time.Millisecond)

	tr := ui.NewTester(a.View, 1060, 720)
	tr.Frame()

	if a.page != "overview" {
		t.Fatalf("start page = %q", a.page)
	}

	if err := tr.Click("规则"); err != nil {
		t.Fatalf("click 规则: %v", err)
	}
	tr.Frame()
	if a.page != "rules" {
		t.Fatalf("after clicking 规则, page = %q", a.page)
	}

	// The page change runs a transition; a frame in the middle of it must still
	// draw rather than panic or blank out.
	tr.Advance(60 * time.Millisecond)
	tr.Frame()
	if img := tr.Image(); img == nil {
		t.Fatal("no image during the page transition")
	}

	// Rules filtering: the adapter dropdown filters without touching the query.
	a.ruleAdapter = "DIRECT"
	tr.Frame()
	shown := filterRules(a.k.Rules(), a.ruleQuery, a.ruleAdapter)
	if len(shown) == 0 || len(shown) == len(a.k.Rules()) {
		t.Fatalf("adapter filter did not narrow the list: %d of %d", len(shown), len(a.k.Rules()))
	}
	for _, row := range shown {
		if row.rule.Adapter != "DIRECT" {
			t.Fatalf("filter let through %q", row.rule.Adapter)
		}
	}

	if err := tr.Click("日志"); err != nil {
		t.Fatalf("click 日志: %v", err)
	}
	tr.Frame()
	if a.page != "logs" {
		t.Fatalf("after clicking 日志, page = %q", a.page)
	}

	if err := tr.Click("暂停"); err != nil {
		t.Fatalf("click 暂停: %v", err)
	}
	tr.Frame()
	if !a.logPaused || a.logFrozen == nil {
		t.Fatal("pause did not freeze the buffer")
	}

	if err := tr.Click("继续"); err != nil {
		t.Fatalf("click 继续: %v", err)
	}
	tr.Frame()
	if a.logPaused || a.logFrozen != nil {
		t.Fatal("resume did not unfreeze the buffer")
	}

	if err := tr.Click("警告"); err != nil {
		t.Fatalf("click 警告: %v", err)
	}
	tr.Frame()
	if a.logLevelIdx != 2 {
		t.Fatalf("level filter = %d, want 2", a.logLevelIdx)
	}

	// Every visible line must satisfy the chosen level.
	for _, line := range filterLogs(a.k.Logs(), a.logLevelIdx) {
		if levelRank(line.Level) < 2 {
			t.Fatalf("level filter let through %q", line.Level)
		}
	}
}
