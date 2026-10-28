package patch

import (
	"strings"
	"testing"

	C "github.com/metacubex/mihomo/constant"
	RC "github.com/metacubex/mihomo/rules/common"
	"gopurr/internal/applications"
)

func TestApplicationRulesMatchHelpersWithoutCrossingBundles(t *testing.T) {
	path := "/Applications/Work, [Browser]+.app"
	route := AppRule{Application: applications.Application{Name: "Work Browser", Path: path, Executable: path + "/Contents/MacOS/Main"}, Target: "DIRECT", IncludeHelpers: true}
	for _, includeHelpers := range []bool{true, false} {
		route.IncludeHelpers = includeHelpers
		text, err := AppRuleText(route)
		if err != nil {
			t.Fatal(err)
		}
		kind, payload, target, params := RC.ParseRulePayload(text, true)
		rule, err := parseRuleOffline(kind, payload, target, params, nil)
		if err != nil {
			t.Fatalf("mihomo rejected generated rule %s: %v", text, err)
		}
		for _, tc := range []struct {
			path string
			want bool
		}{
			{route.Application.Executable, true},
			{path + "/Contents/Frameworks/Framework.framework/Versions/123/Helpers/Browser Helper.app/Contents/MacOS/Browser Helper", includeHelpers},
			{path + "/Contents/XPCServices/Worker.xpc/Contents/MacOS/Worker", includeHelpers},
			{path + "/Contents/Library/LoginItems/Updater.app/Contents/MacOS/Updater", includeHelpers},
			{path + ".backup/Contents/MacOS/Main", false},
			{path + "/Resources/Main", false},
			{"/Applications/Other.app/Contents/MacOS/Browser Helper", false},
			{"/usr/libexec/Browser Helper", false},
			{route.Application.Executable + " Other", includeHelpers},
		} {
			got, _ := rule.Match(&C.Metadata{ProcessPath: tc.path}, C.RuleMatchHelper{})
			if got != tc.want {
				t.Errorf("helpers=%v, %s matched=%v, want %v", includeHelpers, tc.path, got, tc.want)
			}
		}
	}
}

func TestApplicationRulesPriorityAndProcessLookup(t *testing.T) {
	ov := Default()
	route := AppRule{Application: applications.Application{Name: "curl", Path: "/usr/bin/curl", Executable: "/usr/bin/curl"}, Target: "DIRECT"}
	ov.AppRules = []AppRule{route}
	ov.ExtraRules = []string{"MATCH,REJECT"}
	ov.MergeYAML = "find-process-mode: off\nrules:\n  - MATCH,DIRECT\n"
	doc := appliedDoc(t, []byte("rules: []"), ov)
	rules := doc["rules"].([]any)
	if len(rules) != 3 || !strings.HasPrefix(rules[0].(string), "PROCESS-PATH-REGEX,") || rules[1] != "MATCH,REJECT" || rules[2] != "MATCH,DIRECT" || doc["find-process-mode"] != "always" {
		t.Fatalf("wrong precedence or lookup: %+v", doc)
	}
	ov.AppRules = nil
	doc = appliedDoc(t, []byte("rules: []"), ov)
	if doc["find-process-mode"] != "off" || len(doc["rules"].([]any)) != 2 {
		t.Fatal("removing app routes did not restore the underlying configuration")
	}
	for _, invalid := range []AppRule{
		{Application: route.Application, Target: "Deleted Group"},
		{Application: route.Application, Target: "DIRECT,REJECT"},
		{Application: route.Application, Target: "DIRECT", IncludeHelpers: true},
		{Application: applications.Application{Path: "relative", Executable: "relative"}, Target: "DIRECT"},
	} {
		ov.AppRules = []AppRule{invalid}
		if _, err := Apply([]byte("rules: []"), ov); err == nil {
			t.Fatalf("accepted invalid application route %+v", invalid)
		}
	}
	ov.AppRules = []AppRule{route, route}
	if _, err := Apply([]byte("rules: []"), ov); err == nil {
		t.Fatal("accepted duplicate application policies")
	}
}
