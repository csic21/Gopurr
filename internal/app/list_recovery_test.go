package app

import (
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/core"
)

func TestEmptyListRecoveryRestoresContentWithoutApplyingChanges(t *testing.T) {
	for _, page := range []string{"connections", "rules", "logs", "applications"} {
		t.Run(page, func(t *testing.T) {
			a := subscriptionTestApp(t)
			fixtureApplicationRules(a)
			a.appRulesDraft[0].Target = "REJECT" // Keep an unapplied edit across recovery.
			before := []core.Conn{{ID: "one", Host: "example.test", Process: "Browser"}}
			a.connRows, a.connFetched, a.connQuery = before, time.Now(), "missing"
			a.ruleQuery, a.ruleAdapter = "missing", "DIRECT"
			a.logPaused, a.logQuery, a.logLevelIdx = true, "missing", 2
			a.logFrozen = []core.LogLine{{Level: "info", Text: "diagnostic kept intact"}}
			a.appRulesQuery = "missing"
			view := func(c *ui.Context) {
				c.SetTheme(theme(false))
				ui.Column(c).Fill().Gap(12).Children(func() {
					switch page {
					case "connections":
						a.connsPage(c)
					case "rules":
						a.effectiveRules(c, []core.Rule{{Payload: "example.test", Type: "Domain", Adapter: "DIRECT"}})
					case "logs":
						a.logsPage(c)
					case "applications":
						a.applicationRulesPage(c)
					}
				})
			}
			tr := ui.NewTester(view, 616, 420)
			tr.SetPreferences(ui.Preferences{ReduceMotion: true})
			tr.Frame()
			if err := tr.Click("重置筛选"); err != nil {
				t.Fatal(err)
			}
			tr.Frame()
			if _, found := tr.Find("重置筛选"); found {
				t.Fatal("reset did not restore the list")
			}
			want := map[string]string{"connections": "example.test", "rules": "example.test", "logs": "diagnostic kept intact", "applications": "Work Browser · 工作与家庭共用的浏览器"}[page]
			if !tr.HasText(want) {
				t.Fatalf("recovered list is missing %q", want)
			}
			if !reflect.DeepEqual(a.connRows, before) || !a.logPaused || len(a.logFrozen) != 1 || a.appRulesDraft[0].Target != "REJECT" || a.ov.AppRules[0].Target == "REJECT" {
				t.Fatal("reset changed source data, resumed logging or applied an editor draft")
			}
		})
	}
}
