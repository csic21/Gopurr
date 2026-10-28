package configyaml

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestMarshalPreservesPolicyOrderAndScalarSpelling(t *testing.T) {
	raw := []byte("dns:\n  nameserver-policy:\n    z.example: 1.1.1.1\n    a.example: 8.8.8.8\ncredentials:\n  short-id: '01234567'\n  number: 01234567\n")
	var values map[string]any
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatal(err)
	}
	values["mixed-port"] = 12345
	for i := 0; i < 3; i++ { // patch, initial daemon load, controller reload
		out, err := Marshal(raw, values)
		if err != nil {
			t.Fatal(err)
		}
		text := string(out)
		if strings.Index(text, "z.example:") > strings.Index(text, "a.example:") ||
			!strings.Contains(text, "short-id: '01234567'") || !strings.Contains(text, "number: 01234567") {
			t.Fatalf("round trip %d changed ordered or scalar YAML:\n%s", i, text)
		}
		raw = out
	}
}

func TestMarshalPreservesOverlayOrderAndExpandsAliases(t *testing.T) {
	raw := []byte("defaults: &defaults\n  external-controller: 0.0.0.0:9090\n  secret: unsafe\n<<: *defaults\nproxies: []\n")
	addition := []byte("dns:\n  nameserver-policy:\n    z.example: 1.1.1.1\n    a.example: 8.8.8.8\n")
	values := map[string]any{"defaults": map[string]any{"external-controller": "0.0.0.0:9090", "secret": "unsafe"},
		"proxies": []any{}, "dns": map[string]any{"nameserver-policy": map[string]any{"z.example": "1.1.1.1", "a.example": "8.8.8.8"}}}
	out, err := Marshal(raw, values, addition)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["external-controller"] != nil || decoded["secret"] != nil ||
		strings.Index(string(out), "z.example:") > strings.Index(string(out), "a.example:") {
		t.Fatalf("merge reintroduced stripped fields or reordered overlay:\n%s", out)
	}
}
