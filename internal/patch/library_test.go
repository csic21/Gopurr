package patch

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestReusableTemplatesApplyExplicitOrder(t *testing.T) {
	library := []OverrideTemplate{
		{ID: "first", Name: "First", YAML: "dns:\n  enable: true\n  nameserver: [1.1.1.1]\nprofile:\n  store-selected: true\n"},
		{ID: "second", Name: "Second", YAML: "dns:\n  nameserver: [9.9.9.9]\nrules: [MATCH,DIRECT]\n"},
	}
	doc := map[string]any{}
	rules, err := applyTemplateYAML(doc, library, []string{"first", "second"})
	if err != nil || !rules {
		t.Fatalf("layers rejected: %v", err)
	}
	dns := doc["dns"].(map[string]any)
	if dns["enable"] != true || dns["nameserver"].([]any)[0] != "9.9.9.9" {
		t.Fatalf("wrong merge: %+v", doc)
	}
	if doc["profile"].(map[string]any)["store-selected"] != true {
		t.Fatal("map fields lost")
	}
	if _, err := applyTemplateYAML(doc, library, []string{"second", "first"}); err != nil {
		t.Fatal(err)
	}
	if doc["dns"].(map[string]any)["nameserver"].([]any)[0] != "1.1.1.1" {
		t.Fatal("list order ignored")
	}
}

func TestReusableTemplateScopePrecedence(t *testing.T) {
	global := Default()
	global.Library = []OverrideTemplate{
		{ID: "global", Name: "Global", YAML: "mixed-port: 9999\ntcp-concurrent: true\ndns:\n  nameserver: [4.4.4.4]\n"},
		{ID: "profile", Name: "Profile", YAML: "dns:\n  nameserver: [8.8.8.8]\nrules:\n  - MATCH,DIRECT\n"},
	}
	global.TemplateIDs = []string{"global"}
	global.MergeYAML = "dns:\n  nameserver: [9.9.9.9]\n"
	local := ProfileOverrides{TemplateIDs: []string{"profile"}, MergeYAML: "dns:\n  nameserver: [1.1.1.1]\n"}
	raw, err := ApplyLayers([]byte("mixed-port: 7891\ndns:\n  enable: true\n  nameserver: [2.2.2.2]\nrules:\n  - MATCH,DIRECT\n"), global, local)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["mixed-port"] != global.MixedPort || doc["tcp-concurrent"] != true {
		t.Fatalf("application ownership or global template lost: %+v", doc)
	}
	dns := doc["dns"].(map[string]any)
	if dns["enable"] != true || dns["nameserver"].([]any)[0] != "1.1.1.1" {
		t.Fatalf("wrong scope precedence: %+v", dns)
	}
	local.TemplateIDs = []string{"missing"}
	if _, err := ApplyLayers(raw, global, local); err == nil {
		t.Fatal("missing profile template ignored")
	}
}

func TestReusableTemplateRejectsInvalidDefinitionsAndReferences(t *testing.T) {
	valid := OverrideTemplate{ID: "valid", Name: "Valid", YAML: "tcp-concurrent: true"}
	for _, library := range [][]OverrideTemplate{
		{{ID: "", Name: "Missing ID"}}, {valid, valid}, {{ID: "a/b", Name: "Path"}},
		{{ID: "a", Name: "\n"}}, {{ID: "a", Name: "A", YAML: "[not,a,map]"}},
		{{ID: "a", Name: "A", YAML: "external-controller: 0.0.0.0:9090"}},
		{{ID: "a", Name: "A", YAML: "dns: {}\n---\ndns: {}"}},
		{{ID: "a", Name: "A", YAML: "rules: false"}},
		{{ID: "a", Name: "A", YAML: strings.Repeat(" ", MaxTemplateYAML+1)}},
	} {
		if err := ValidateTemplateLibrary(library); err == nil {
			t.Fatalf("accepted %+v", library[:1])
		}
	}
	for _, ids := range [][]string{{"missing"}, {"valid", "valid"}, {""}} {
		if _, err := ResolveTemplateYAML([]OverrideTemplate{valid}, ids); err == nil {
			t.Fatalf("accepted refs: %v", ids)
		}
	}
}
